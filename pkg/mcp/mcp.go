// Package mcp implements a stdio-based MCP server that exposes SkoveNet
// mesh network operations as MCP tools. It communicates with the local
// SkoveNet agent over IPC and bridges responses back to the MCP client.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skoveit/skovenet/pkg/ipc"
)

// MCPServer wraps an IPC client and exposes SkoveNet operations as MCP tools.
type MCPServer struct {
	client *ipc.ControllerClient
}

// New creates a new MCPServer backed by the provided IPC client.
func New(client *ipc.ControllerClient) *MCPServer {
	return &MCPServer{client: client}
}

// Run registers all tools and starts the MCP stdio server.  It blocks until
// the context is cancelled or the underlying transport closes.
func (m *MCPServer) Run(ctx context.Context) error {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "skovenet",
		Version: "1.0.0",
	}, nil)

	m.registerTools(srv)

	return srv.Run(ctx, &mcp.StdioTransport{})
}

// ─── tool argument structs ────────────────────────────────────────────────────

type noArgs struct{}

type radarArgs struct {
	Timeout string `json:"timeout,omitempty" jsonschema:"Scan timeout, e.g. '3s'. Default is 3s."`
}

type peerArgs struct {
	PeerID string `json:"peer_id" jsonschema:"The target peer ID"`
}

type runCommandArgs struct {
	PeerID  string `json:"peer_id"  jsonschema:"The target peer ID"`
	Command string `json:"command"  jsonschema:"The shell command to execute"`
}

type listDirectoryArgs struct {
	PeerID string `json:"peer_id" jsonschema:"The target peer ID"`
	Path   string `json:"path"    jsonschema:"The directory path to list"`
}

type uploadArgs struct {
	PeerID     string `json:"peer_id"      jsonschema:"The target peer ID"`
	LocalPath  string `json:"local_path"   jsonschema:"Local file path on the operator machine"`
	RemotePath string `json:"remote_path"  jsonschema:"Destination path on the remote node"`
}

type downloadArgs struct {
	PeerID     string `json:"peer_id"      jsonschema:"The target peer ID"`
	RemotePath string `json:"remote_path"  jsonschema:"Source path on the remote node"`
	LocalPath  string `json:"local_path"   jsonschema:"Destination path on the operator machine"`
}

// ─── helper ──────────────────────────────────────────────────────────────────

// textResult wraps a plain string into a successful CallToolResult.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// sendAndWait sends a command over IPC and waits for the async response
// identified by the returned cmdID.  The response is what the agent sends
// back when the remote peer replies, so it arrives on the async channel.
func (m *MCPServer) sendAndWait(timeout time.Duration, cmd string, args ...string) (string, error) {
	// Send the command; the agent returns a cmdID for correlation.
	cmdID, err := m.client.Send(cmd, args...)
	if err != nil {
		return "", fmt.Errorf("ipc send error: %w", err)
	}
	if cmdID == "" {
		return "", fmt.Errorf("agent returned no command ID")
	}

	resp, err := m.client.WaitAsync(cmdID, timeout)
	if err != nil {
		return "", fmt.Errorf("waiting for response (cmdID=%s): %w", cmdID, err)
	}
	return resp, nil
}

// ─── tool registration ────────────────────────────────────────────────────────

func (m *MCPServer) registerTools(srv *mcp.Server) {
	// list_peers – synchronous IPC call
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "list_peers",
			Description: "List all peers currently connected to the local SkoveNet node.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
			resp, err := m.client.Send("peers")
			if err != nil {
				return nil, nil, fmt.Errorf("list_peers: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// radar_scan – synchronous IPC call (agent blocks for the scan duration)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "radar_scan",
			Description: "Discover all nodes reachable in the network via a radar scan.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args radarArgs) (*mcp.CallToolResult, any, error) {
			timeout := "3s"
			if args.Timeout != "" {
				timeout = args.Timeout
			}
			resp, err := m.client.Send("radar", timeout)
			if err != nil {
				return nil, nil, fmt.Errorf("radar_scan: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// run_command – async: agent forwards command to remote peer and returns a
	// cmdID; the peer's response arrives as an async push message.
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "run_command",
			Description: "Execute a shell command on a remote SkoveNet node and return its output.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args runCommandArgs) (*mcp.CallToolResult, any, error) {
			if args.PeerID == "" || args.Command == "" {
				return nil, nil, fmt.Errorf("run_command: peer_id and command are required")
			}
			resp, err := m.sendAndWait(30*time.Second, "send", args.PeerID, args.Command)
			if err != nil {
				return nil, nil, fmt.Errorf("run_command: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// list_directory – async
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "list_directory",
			Description: "List files and directories at the given path on a remote SkoveNet node.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args listDirectoryArgs) (*mcp.CallToolResult, any, error) {
			if args.PeerID == "" || args.Path == "" {
				return nil, nil, fmt.Errorf("list_directory: peer_id and path are required")
			}
			resp, err := m.sendAndWait(15*time.Second, "send", args.PeerID, "ls "+args.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("list_directory: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// get_system_info – async
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "get_system_info",
			Description: "Retrieve system information (OS, CPU, memory, etc.) from a remote SkoveNet node.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args peerArgs) (*mcp.CallToolResult, any, error) {
			if args.PeerID == "" {
				return nil, nil, fmt.Errorf("get_system_info: peer_id is required")
			}
			resp, err := m.sendAndWait(15*time.Second, "send", args.PeerID, "info")
			if err != nil {
				return nil, nil, fmt.Errorf("get_system_info: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// list_processes – async
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "list_processes",
			Description: "List running processes on a remote SkoveNet node.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args peerArgs) (*mcp.CallToolResult, any, error) {
			if args.PeerID == "" {
				return nil, nil, fmt.Errorf("list_processes: peer_id is required")
			}
			resp, err := m.sendAndWait(15*time.Second, "send", args.PeerID, "ps")
			if err != nil {
				return nil, nil, fmt.Errorf("list_processes: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// upload_file – async (file transfer can take a while)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "upload_file",
			Description: "Upload a local file to a remote SkoveNet node.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args uploadArgs) (*mcp.CallToolResult, any, error) {
			if args.PeerID == "" || args.LocalPath == "" || args.RemotePath == "" {
				return nil, nil, fmt.Errorf("upload_file: peer_id, local_path and remote_path are required")
			}
			resp, err := m.sendAndWait(120*time.Second, "send", args.PeerID, "upload", args.LocalPath, args.RemotePath)
			if err != nil {
				return nil, nil, fmt.Errorf("upload_file: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// download_file – async
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "download_file",
			Description: "Download a file from a remote SkoveNet node to the local machine.",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, args downloadArgs) (*mcp.CallToolResult, any, error) {
			if args.PeerID == "" || args.RemotePath == "" || args.LocalPath == "" {
				return nil, nil, fmt.Errorf("download_file: peer_id, remote_path and local_path are required")
			}
			resp, err := m.sendAndWait(120*time.Second, "send", args.PeerID, "download", args.RemotePath, args.LocalPath)
			if err != nil {
				return nil, nil, fmt.Errorf("download_file: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// ─── bonus: useful synchronous tools ─────────────────────────────────────

	// get_node_id – synchronous
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "get_node_id",
			Description: "Return the local SkoveNet node ID (public key fingerprint).",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
			resp, err := m.client.Send("id")
			if err != nil {
				return nil, nil, fmt.Errorf("get_node_id: %w", err)
			}
			return textResult(resp), nil, nil
		},
	)

	// get_topology – synchronous
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "get_topology",
			Description: "Return the full network topology as JSON (nodes and edges).",
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
			resp, err := m.client.Send("topology")
			if err != nil {
				return nil, nil, fmt.Errorf("get_topology: %w", err)
			}
			// Pretty-print JSON if possible
			var v any
			if json.Unmarshal([]byte(resp), &v) == nil {
				if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
					resp = string(pretty)
				}
			}
			return textResult(resp), nil, nil
		},
	)
}
