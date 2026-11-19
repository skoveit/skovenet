package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

type mcpFixture struct {
	ctx     context.Context
	session *sdk.ClientSession
	bridge  *MCPServer
	agent   *ipc.AgentServer
}

// Both transports carry real newline-delimited JSON over isolated net.Pipe
// connections. No default agent socket, real shell, or scheduling sleep is used.
func newMCPFixture(t *testing.T, handler func(*ipc.AgentServer, string, []string) string, middleware ...sdk.Middleware) *mcpFixture {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	var agent *ipc.AgentServer
	agent = ipc.NewAgentServerWithListener(&pipeListener{conn: serverConn, done: make(chan struct{})}, func(cmd string, args []string) string { return handler(agent, cmd, args) })
	client := ipc.NewControllerClientWithConn(clientConn)
	t.Cleanup(func() { client.Close(); agent.Stop() })
	bridge := New(client)
	server := bridge.server()
	server.AddReceivingMiddleware(middleware...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	mcpClient := sdk.NewClient(&sdk.Implementation{Name: "contract-test", Version: "1.0"}, nil)
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		serverSession.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); session.Close(); serverSession.Close() })
	return &mcpFixture{ctx: ctx, session: session, bridge: bridge, agent: agent}
}

func callToolText(t *testing.T, f *mcpFixture, name string, args map[string]any) string {
	t.Helper()
	result, err := f.session.CallTool(f.ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool %s returned an error: %#v", name, result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("CallTool %s returned %d content blocks", name, len(result.Content))
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("CallTool %s returned %T, want text", name, result.Content[0])
	}
	return text.Text
}

func TestMCPInitializeAndToolsListContract(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t, func(*ipc.AgentServer, string, []string) string {
		t.Error("tools/list unexpectedly called IPC")
		return ""
	})
	info := f.session.InitializeResult()
	if info.ProtocolVersion == "" || info.ServerInfo.Name != "skovenet" || info.ServerInfo.Version != "1.0.0" || info.Capabilities.Tools == nil {
		t.Fatalf("initialize = %#v", info)
	}
	result, err := f.session.ListTools(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{
		"list_peers": {}, "radar_scan": {}, "run_command": {"command", "peer_id"},
		"list_directory": {"path", "peer_id"}, "get_system_info": {"peer_id"}, "list_processes": {"peer_id"},
		"upload_file": {"local_path", "peer_id", "remote_path"}, "download_file": {"local_path", "peer_id", "remote_path"},
		"get_node_id": {}, "get_topology": {},
	}
	if len(result.Tools) != len(expected) || result.NextCursor != "" {
		t.Fatalf("tools/list returned %d tools and cursor %q", len(result.Tools), result.NextCursor)
	}
	seen := map[string]bool{}
	for _, tool := range result.Tools {
		required, ok := expected[tool.Name]
		if !ok || seen[tool.Name] {
			t.Errorf("unexpected/duplicate tool %q", tool.Name)
			continue
		}
		seen[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Type       string
			Required   []string
			Properties map[string]json.RawMessage
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		slices.Sort(schema.Required)
		if schema.Type != "object" || !slices.Equal(schema.Required, required) {
			t.Errorf("tool %s schema = %s", tool.Name, raw)
		}
		for _, key := range required {
			var property struct{ Type string }
			if err := json.Unmarshal(schema.Properties[key], &property); err != nil || property.Type != "string" {
				t.Errorf("tool %s property %s is not a string", tool.Name, key)
			}
		}
	}
}

func TestMCPSynchronousToolContracts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		args           map[string]any
		cmd            string
		ipcArgs        []string
		response, want string
	}{
		{"list_peers", map[string]any{}, "peers", nil, "peer-1\npeer-2", "peer-1\npeer-2"},
		{"get_node_id", map[string]any{}, "id", nil, "local-peer", "local-peer"},
		{"radar_scan", map[string]any{}, "radar", []string{"3s"}, "radar-output", "radar-output"},
		{"radar_scan", map[string]any{"timeout": "500ms"}, "radar", []string{"500ms"}, "radar-output", "radar-output"},
		{"get_topology", map[string]any{}, "topology", nil, `{"nodes":["a","b"],"edges":[]}`, "{\n  \"edges\": [],\n  \"nodes\": [\n    \"a\",\n    \"b\"\n  ]\n}"},
		{"get_topology", map[string]any{}, "topology", nil, "topology unavailable", "topology unavailable"},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%s-%d", tt.name, i), func(t *testing.T) {
			t.Parallel()
			f := newMCPFixture(t, func(_ *ipc.AgentServer, cmd string, args []string) string {
				if cmd != tt.cmd || !reflect.DeepEqual(args, tt.ipcArgs) {
					t.Errorf("IPC = %s %q, want %s %q", cmd, args, tt.cmd, tt.ipcArgs)
				}
				return tt.response
			})
			if got := callToolText(t, f, tt.name, tt.args); got != tt.want {
				t.Fatalf("result = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMCPAsyncToolContractsWithEarlyReplies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"run_command", map[string]any{"peer_id": "peer-1", "command": "echo hello world"}, []string{"peer-1", "echo hello world"}},
		{"list_directory", map[string]any{"peer_id": "peer-1", "path": "/a path"}, []string{"peer-1", "ls /a path"}},
		{"get_system_info", map[string]any{"peer_id": "peer-1"}, []string{"peer-1", "info"}},
		{"list_processes", map[string]any{"peer_id": "peer-1"}, []string{"peer-1", "ps"}},
		{"upload_file", map[string]any{"peer_id": "peer-1", "local_path": "/local path", "remote_path": "/remote path"}, []string{"peer-1", "upload", "/local path", "/remote path"}},
		{"download_file", map[string]any{"peer_id": "peer-1", "local_path": "/local path", "remote_path": "/remote path"}, []string{"peer-1", "download", "/remote path", "/local path"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newMCPFixture(t, func(agent *ipc.AgentServer, cmd string, args []string) string {
				if cmd != "send" || !reflect.DeepEqual(args, tt.want) {
					t.Errorf("IPC = %s %q, want send %q", cmd, args, tt.want)
				}
				agent.PushWithCmdID("early-result\n", "cmd-early")
				return "cmd-early"
			})
			if got := callToolText(t, f, tt.name, tt.args); got != "early-result\n" {
				t.Fatalf("result = %q", got)
			}
		})
	}
}

func TestMCPRepeatedEarlyRepliesDoNotFillNotifications(t *testing.T) {
	t.Parallel()
	var next atomic.Int64
	f := newMCPFixture(t, func(agent *ipc.AgentServer, cmd string, args []string) string {
		id := fmt.Sprintf("cmd-%d", next.Add(1))
		agent.PushEvent("peer_connected", "peer-1")
		agent.PushWithCmdID("output:"+id, id)
		return id
	})
	for i := 1; i <= 150; i++ {
		if got := callToolText(t, f, "run_command", map[string]any{"peer_id": "peer-1", "command": "info"}); got != fmt.Sprintf("output:cmd-%d", i) {
			t.Fatalf("result = %q", got)
		}
	}
}

func TestMCPInvalidCallsDoNotReachAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args map[string]any
	}{
		{"unknown_tool", map[string]any{}},
		{"run_command", map[string]any{"peer_id": "peer-1"}},
		{"run_command", map[string]any{"peer_id": "peer-1", "command": 42}},
		{"run_command", map[string]any{"peer_id": "", "command": "info"}},
		{"list_directory", map[string]any{"peer_id": "peer-1", "path": ""}},
		{"get_system_info", map[string]any{"peer_id": ""}},
		{"list_processes", map[string]any{"peer_id": ""}},
		{"upload_file", map[string]any{"peer_id": "peer-1", "local_path": "", "remote_path": "/remote"}},
		{"download_file", map[string]any{"peer_id": "peer-1", "remote_path": "/remote"}},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%s-%d", tt.name, i), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			f := newMCPFixture(t, func(*ipc.AgentServer, string, []string) string { calls.Add(1); return "unexpected" })
			result, err := f.session.CallTool(f.ctx, &sdk.CallToolParams{Name: tt.name, Arguments: tt.args})
			if err == nil && !result.IsError {
				t.Fatal("invalid tool call succeeded")
			}
			if calls.Load() != 0 {
				t.Fatal("invalid tool call reached IPC agent")
			}
		})
	}
}

func TestMCPToolCancellationStopsServerWait(t *testing.T) {
	t.Parallel()
	seen := make(chan struct{}, 1)
	finished := make(chan struct{}, 1)
	middleware := func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if method == "tools/call" {
				defer func() { finished <- struct{}{} }()
			}
			return next(ctx, method, req)
		}
	}
	f := newMCPFixture(t, func(*ipc.AgentServer, string, []string) string { seen <- struct{}{}; return "cmd-pending" }, middleware)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.session.CallTool(ctx, &sdk.CallToolParams{Name: "run_command", Arguments: map[string]any{"peer_id": "peer-1", "command": "info"}})
		done <- err
	}()
	select {
	case <-seen:
	case <-f.ctx.Done():
		t.Fatal("agent did not receive command")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallTool error = %v", err)
		}
	case <-f.ctx.Done():
		t.Fatal("cancelled CallTool did not return")
	}
	select {
	case <-finished:
	case <-f.ctx.Done():
		t.Fatal("server kept waiting after tool cancellation")
	}
}

func TestMCPEmptyCommandIDReturnsError(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t, func(*ipc.AgentServer, string, []string) string { return "" })
	result, err := f.session.CallTool(f.ctx, &sdk.CallToolParams{Name: "run_command", Arguments: map[string]any{"peer_id": "peer-1", "command": "info"}})
	if err != nil {
		if !strings.Contains(err.Error(), "no command ID") {
			t.Fatal(err)
		}
		return
	}
	if !result.IsError {
		t.Fatal("empty command ID succeeded")
	}
	if len(result.Content) != 1 {
		t.Fatalf("error content = %#v", result.Content)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || !strings.Contains(text.Text, "no command ID") {
		t.Fatalf("unexpected error content %#v", result.Content)
	}
}

func TestMCPAgentRejectionReturnsError(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t, func(*ipc.AgentServer, string, []string) string {
		return "error: not signed in. Use 'sign <private_key>' first"
	})
	result, err := f.session.CallTool(f.ctx, &sdk.CallToolParams{Name: "run_command", Arguments: map[string]any{"peer_id": "peer-1", "command": "info"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("agent rejection = %#v", result)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || !strings.Contains(text.Text, "not signed in") {
		t.Fatalf("rejection content = %#v", result.Content)
	}
}

func TestMCPCancelledSynchronousToolKeepsBridgeUsable(t *testing.T) {
	t.Parallel()
	radarStarted := make(chan struct{}, 1)
	releaseRadar := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseRadar) }) }
	finished := make(chan struct{}, 1)
	middleware := func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if method == "tools/call" {
				defer func() {
					select {
					case finished <- struct{}{}:
					default:
					}
				}()
			}
			return next(ctx, method, req)
		}
	}
	f := newMCPFixture(t, func(_ *ipc.AgentServer, cmd string, _ []string) string {
		if cmd == "radar" {
			radarStarted <- struct{}{}
			<-releaseRadar
			return "stale-radar-output"
		}
		return "peer-1"
	}, middleware)
	t.Cleanup(release) // registered after the fixture so it runs before agent.Stop
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.session.CallTool(ctx, &sdk.CallToolParams{Name: "radar_scan", Arguments: map[string]any{}})
		done <- err
	}()
	select {
	case <-radarStarted:
	case <-f.ctx.Done():
		t.Fatal("agent did not receive radar command")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallTool error = %v", err)
		}
	case <-f.ctx.Done():
		t.Fatal("cancelled CallTool did not return")
	}
	select {
	case <-finished:
	case <-f.ctx.Done():
		t.Fatal("server kept waiting after tool cancellation")
	}
	// The agent now delivers the abandoned radar reply; the next tool must get
	// its own reply over the same, still-open IPC connection.
	release()
	if got := callToolText(t, f, "list_peers", map[string]any{}); got != "peer-1" {
		t.Fatalf("list_peers after cancellation = %q", got)
	}
}
