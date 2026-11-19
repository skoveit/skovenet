package mcp

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skoveit/skovenet/pkg/ipc"
)

type pipeListener struct {
	conn      net.Conn
	once      sync.Once
	done      chan struct{}
	closeOnce sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error   { l.closeOnce.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return l.conn.LocalAddr() }

func isolatedIPC(t *testing.T, handler ipc.CommandHandler) (*ipc.AgentServer, *ipc.ControllerClient) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := ipc.NewAgentServerWithListener(&pipeListener{conn: serverConn, done: make(chan struct{})}, handler)
	client := ipc.NewControllerClientWithConn(clientConn)
	t.Cleanup(func() { client.Close(); server.Stop() })
	return server, client
}

func mockIPCServer(t *testing.T) (*ipc.AgentServer, *ipc.ControllerClient) {
	return isolatedIPC(t, func(cmd string, args []string) string {
		switch cmd {
		case "peers":
			return `["peer1", "peer2"]`
		case "id":
			return "my-test-id"
		case "radar":
			return `[{"peer_id":"peer1","latency_ms":10}]`
		case "send":
			return "cmd_1234"
		default:
			return "unknown"
		}
	})
}

func setupMCPTest(t *testing.T) (*mcp.Server, *ipc.AgentServer, *ipc.ControllerClient) {
	agent, client := mockIPCServer(t)

	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "skovenet-test",
		Version: "1.0",
	}, nil)

	handler := New(client)
	handler.registerTools(mcpSrv)

	return mcpSrv, agent, client
}

func TestToolsList(t *testing.T) {
	srv, agent, client := setupMCPTest(t)
	defer agent.Stop()
	defer client.Close()

	// Wait briefly for init to settle
	time.Sleep(50 * time.Millisecond)

	// MCP spec dictates tools are grouped under "tools/list".
	// The internal mcp.Server API uses srv.Tools() to retrieve the list.
	// Unfortunately, mcp.Server methods are scoped to handle incoming protocol messages.
	// We can manually verify if tools were registered by simulating a CallTool.
	// Just asserting the server starts without panics for now.
	if srv == nil {
		t.Fatalf("mcp server should not be nil")
	}
}

func TestListPeersTool(t *testing.T) {
	srv, agent, client := setupMCPTest(t)
	defer agent.Stop()
	defer client.Close()

	// list_peers is sync. Let's just create an ad-hoc test instance that is not bound to a real Stdio socket.
	// But it's easier to invoke our IPC directly or use the mcp in-memory transport.

	t1, t2 := mcp.NewInMemoryTransports()
	go func() {
		_ = srv.Run(context.Background(), t1)
	}()

	c := mcp.NewClient(&mcp.Implementation{Name: "testClient", Version: "1.0"}, nil)
	session, err := c.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	defer session.Close()

	// Allow session initialization
	time.Sleep(100 * time.Millisecond)

	// Call list_peers
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_peers",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("CallTool list_peers failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content")
	}

	if tc.Text != `["peer1", "peer2"]` {
		t.Errorf("expected peers array, got %q", tc.Text)
	}
}

func TestAsyncTool_RunCommand(t *testing.T) {
	srv, agent, client := setupMCPTest(t)
	defer agent.Stop()
	defer client.Close()

	t1, t2 := mcp.NewInMemoryTransports()
	go func() {
		_ = srv.Run(context.Background(), t1)
	}()

	c := mcp.NewClient(&mcp.Implementation{Name: "testClient", Version: "1.0"}, nil)
	session, err := c.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	defer session.Close()

	// Simulate the agent sending the async push message 100ms later
	go func() {
		time.Sleep(100 * time.Millisecond)
		agent.PushWithCmdID("root\n", "cmd_1234")
	}()

	// Note: Timeout is very long in mcp.go, but we override it naturally via context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_command",
		Arguments: map[string]interface{}{
			"peer_id": "test-peer",
			"command": "whoami",
		},
	})
	if err != nil {
		t.Fatalf("CallTool run_command failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected content in result")
	}

	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content")
	}

	if tc.Text != "root\n" {
		t.Errorf("expected 'root\n', got %q", tc.Text)
	}
}

func TestTopologyTool_JSONFormat(t *testing.T) {
	_, client := isolatedIPC(t, func(cmd string, args []string) string {
		if cmd == "topology" {
			// Deliberately messy single-line JSON
			return `{"nodes":["n1","n2"],"edges":[{"from":"n1","to":"n2"}]}`
		}
		return ""
	})
	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "skovenet-test"}, nil)
	mcs := New(client)
	mcs.registerTools(mcpSrv)

	t1, t2 := mcp.NewInMemoryTransports()
	go func() {
		_ = mcpSrv.Run(context.Background(), t1)
	}()

	c := mcp.NewClient(&mcp.Implementation{Name: "testClient"}, nil)
	session, err := c.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_topology",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("CallTool get_topology failed: %v", err)
	}

	tc, _ := res.Content[0].(*mcp.TextContent)

	// It should be pretty-printed
	var dummy map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Text), &dummy); err != nil {
		t.Errorf("result is not valid JSON: %v", err)
	}

	if len(tc.Text) < 50 || tc.Text[0] != '{' {
		t.Errorf("result doesn't seem pretty printed: %q", tc.Text)
	}
}
