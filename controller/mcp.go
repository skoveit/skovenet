package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/skoveit/skovenet/pkg/ipc"
)

// ============================================================================
// MCP JSON-RPC 2.0 types (subset of the Model Context Protocol spec)
// ============================================================================

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // string | number | null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool definition as per MCP spec
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// tools/call params
type mcpCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// tools/call result content item
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// initialize params (we only read protocolVersion)
type mcpInitParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

// ============================================================================
// Tool input schemas (JSON Schema, inline)
// ============================================================================

var schemaNoArgs = json.RawMessage(`{"type":"object","properties":{}}`)

var schemaUsePeer = json.RawMessage(`{
  "type": "object",
  "properties": {
    "peer_id": {
      "type": "string",
      "description": "The peer ID to select as the active target"
    }
  },
  "required": ["peer_id"]
}`)

var schemaSendCommand = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "Shell command to execute on the peer (e.g. \"ls /tmp\")"
    },
    "peer_id": {
      "type": "string",
      "description": "Optional peer ID override. Uses active peer if omitted."
    },
    "timeout_sec": {
      "type": "integer",
      "description": "Seconds to wait for a response before returning (default: 15)",
      "default": 15
    }
  },
  "required": ["command"]
}`)

// ============================================================================
// MCP server
// ============================================================================

// RunMCPServer runs an MCP stdio server until stdin is closed or an error occurs.
// It uses the provided ControllerClient to proxy tool calls to the running agent.
func RunMCPServer(c *ipc.ControllerClient) {
	// Active peer for this MCP session (independent of interactive CLI)
	var activePeer string

	srv := &mcpServer{
		client:  c,
		getPeer: func() string { return activePeer },
		setPeer: func(p string) { activePeer = p },
		in:      bufio.NewReader(os.Stdin),
		out:     os.Stdout,
	}
	srv.serve()
}

type mcpServer struct {
	client  *ipc.ControllerClient
	getPeer func() string
	setPeer func(string)
	in      *bufio.Reader
	out     io.Writer
}

func (s *mcpServer) writeResponse(resp mcpResponse) {
	data, _ := json.Marshal(resp)
	s.out.Write(append(data, '\n'))
}

func (s *mcpServer) errResponse(id any, code int, msg string) mcpResponse {
	return mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpError{Code: code, Message: msg},
	}
}

func (s *mcpServer) serve() {
	for {
		line, err := s.in.ReadBytes('\n')
		if err != nil {
			return // stdin closed / EOF
		}
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}

		var req mcpRequest
		if json.Unmarshal(line, &req) != nil {
			s.writeResponse(s.errResponse(nil, -32700, "parse error"))
			continue
		}

		if req.JSONRPC != "2.0" {
			s.writeResponse(s.errResponse(req.ID, -32600, "invalid request"))
			continue
		}

		// Notifications (no id) – just ignore
		if req.ID == nil && !strings.HasPrefix(req.Method, "initialize") {
			continue
		}

		switch req.Method {
		case "initialize":
			s.handleInitialize(req)
		case "notifications/initialized":
			// ACK from client, nothing to do
		case "tools/list":
			s.handleToolsList(req)
		case "tools/call":
			s.handleToolsCall(req)
		default:
			s.writeResponse(s.errResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method)))
		}
	}
}

// ============================================================================
// Method handlers
// ============================================================================

func (s *mcpServer) handleInitialize(req mcpRequest) {
	s.writeResponse(mcpResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "skovenet-controller",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		},
	})
}

func (s *mcpServer) handleToolsList(req mcpRequest) {
	tools := []mcpTool{
		{
			Name:        "list_peers",
			Description: "Returns the list of peer IDs currently connected to the agent",
			InputSchema: schemaNoArgs,
		},
		{
			Name:        "use_peer",
			Description: "Sets the active peer for subsequent send_command calls. Returns an error if the peer does not exist.",
			InputSchema: schemaUsePeer,
		},
		{
			Name:        "send_command",
			Description: "Executes a command on a peer and returns its output. Uses the active peer unless peer_id is supplied.",
			InputSchema: schemaSendCommand,
		},
		{
			Name:        "radar_scan",
			Description: "Scans the network for reachable peers and returns latency results sorted by latency",
			InputSchema: schemaNoArgs,
		},
		{
			Name:        "get_node_id",
			Description: "Returns the local agent node ID",
			InputSchema: schemaNoArgs,
		},
	}

	s.writeResponse(mcpResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	})
}

func (s *mcpServer) handleToolsCall(req mcpRequest) {
	var p mcpCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.writeResponse(s.errResponse(req.ID, -32602, "invalid params"))
		return
	}

	var text string
	var toolErr error

	switch p.Name {
	case "list_peers":
		text, toolErr = s.toolListPeers()
	case "use_peer":
		text, toolErr = s.toolUsePeer(p.Arguments)
	case "send_command":
		text, toolErr = s.toolSendCommand(p.Arguments)
	case "radar_scan":
		text, toolErr = s.toolRadarScan()
	case "get_node_id":
		text, toolErr = s.toolGetNodeID()
	default:
		s.writeResponse(s.errResponse(req.ID, -32602, fmt.Sprintf("unknown tool: %s", p.Name)))
		return
	}

	if toolErr != nil {
		s.writeResponse(mcpResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []mcpContent{{Type: "text", Text: toolErr.Error()}},
				"isError": true,
			},
		})
		return
	}

	s.writeResponse(mcpResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []mcpContent{{Type: "text", Text: text}},
		},
	})
}

// ============================================================================
// Tool implementations
// ============================================================================

func (s *mcpServer) toolListPeers() (string, error) {
	resp, err := s.client.Send("peerlist")
	if err != nil {
		return "", fmt.Errorf("IPC error: %w", err)
	}

	var peers []string
	if json.Unmarshal([]byte(resp), &peers) != nil || len(peers) == 0 {
		return "No peers connected", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d peer(s) connected:\n", len(peers))
	for i, p := range peers {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, p)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func (s *mcpServer) toolUsePeer(raw json.RawMessage) (string, error) {
	var args struct {
		PeerID string `json:"peer_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.PeerID == "" {
		return "", fmt.Errorf("peer_id is required")
	}

	// Validate peer exists
	resp, err := s.client.Send("peerlist")
	if err != nil {
		return "", fmt.Errorf("IPC error: %w", err)
	}
	var peers []string
	json.Unmarshal([]byte(resp), &peers)

	found := false
	for _, p := range peers {
		if p == args.PeerID {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("peer %q not found (connected peers: %s)", args.PeerID, strings.Join(peers, ", "))
	}

	s.setPeer(args.PeerID)
	return fmt.Sprintf("Active peer set to: %s", args.PeerID), nil
}

func (s *mcpServer) toolSendCommand(raw json.RawMessage) (string, error) {
	var args struct {
		Command    string `json:"command"`
		PeerID     string `json:"peer_id"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	peer := args.PeerID
	if peer == "" {
		peer = s.getPeer()
	}
	if peer == "" {
		return "", fmt.Errorf("no active peer — use use_peer first or supply peer_id")
	}

	timeout := time.Duration(args.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	cmdID, err := s.client.Send("send", peer, args.Command)
	if err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}
	if cmdID == "" {
		return "(no output)", nil
	}

	result, err := s.client.WaitAsync(cmdID, timeout)
	if err != nil {
		if err.Error() == "timeout" {
			return fmt.Sprintf("[cmd:%s] Command sent but timed out waiting for output", cmdID), nil
		}
		return "", fmt.Errorf("error waiting for response: %w", err)
	}
	if result == "" {
		return "(command executed, no output)", nil
	}
	return result, nil
}

func (s *mcpServer) toolRadarScan() (string, error) {
	resp, err := s.client.Send("radar", "3s")
	if err != nil {
		return "", fmt.Errorf("radar failed: %w", err)
	}

	var results []RadarResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		return "", fmt.Errorf("failed to parse radar results: %w", err)
	}

	if len(results) == 0 {
		return "No nodes detected by radar scan", nil
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "Radar scan — %d node(s) detected:\n", len(results))
	fmt.Fprintf(&sb, "%-3s %-40s %8s  %s\n", "#", "Peer ID", "Latency", "Signal")
	fmt.Fprintf(&sb, "%s\n", strings.Repeat("-", 70))
	for i, r := range results {
		signal := radarSignal(r.Latency)
		fmt.Fprintf(&sb, "%-3d %-40s %6dms  %s\n", i+1, r.PeerID, r.Latency, signal)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func (s *mcpServer) toolGetNodeID() (string, error) {
	resp, err := s.client.Send("id")
	if err != nil {
		return "", fmt.Errorf("IPC error: %w", err)
	}
	return resp, nil
}

// radarSignal returns a human-readable signal quality string for a given latency.
func radarSignal(latencyMs int64) string {
	switch {
	case latencyMs < 50:
		return "EXCELLENT"
	case latencyMs < 100:
		return "GOOD"
	case latencyMs < 200:
		return "FAIR"
	case latencyMs < 500:
		return "WEAK"
	default:
		return "POOR"
	}
}
