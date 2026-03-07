package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/skoveit/skovenet/pkg/logger"
)

// Internal message format
type message struct {
	Cmd      string   `json:"cmd"`
	Args     []string `json:"args,omitempty"`
	Response string   `json:"response,omitempty"`
	CmdID    string   `json:"cmd_id,omitempty"` // Originating command ID for response correlation
	IsAsync  bool     `json:"async,omitempty"`
	Event    string   `json:"event,omitempty"` // peer_connected, peer_disconnected
	Data     string   `json:"data,omitempty"`  // event data
}

// ============================================================================
// AGENT SERVER
// ============================================================================

type CommandHandler func(cmd string, args []string) string

type AgentServer struct {
	listener    net.Listener
	handler     CommandHandler
	connections map[net.Conn]bool
	connMu      sync.RWMutex
	done        chan struct{}
	wg          sync.WaitGroup
}

func NewAgentServer(handler CommandHandler) (*AgentServer, error) {
	listener, err := CreateListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %w", err)
	}

	s := &AgentServer{
		listener:    listener,
		handler:     handler,
		connections: make(map[net.Conn]bool),
		done:        make(chan struct{}),
	}

	s.wg.Add(1)
	go s.acceptLoop()

	logger.Debug("IPC server started on %s", SocketPath)
	return s, nil
}

func (s *AgentServer) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *AgentServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	s.connMu.Lock()
	s.connections[conn] = true
	s.connMu.Unlock()
	logger.Debug("Controller connected")

	defer func() {
		s.connMu.Lock()
		delete(s.connections, conn)
		s.connMu.Unlock()
		logger.Debug("Controller disconnected")
	}()

	reader := bufio.NewReader(conn)
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var msg message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		response := s.handler(msg.Cmd, msg.Args)
		resp := message{Response: response}
		respData, _ := json.Marshal(resp)
		conn.Write(append(respData, '\n'))

		if msg.Cmd == "quit" {
			return
		}
	}
}

// Push sends async text to all controllers
func (s *AgentServer) Push(text string) {
	s.broadcast(message{Response: text, IsAsync: true})
}

// PushWithCmdID sends async text to all controllers with a command ID for correlation
func (s *AgentServer) PushWithCmdID(text, cmdID string) {
	s.broadcast(message{Response: text, CmdID: cmdID, IsAsync: true})
}

// PushEvent sends an event notification to all controllers
func (s *AgentServer) PushEvent(event, data string) {
	s.broadcast(message{Event: event, Data: data, IsAsync: true})
}

func (s *AgentServer) broadcast(msg message) {
	s.connMu.RLock()
	defer s.connMu.RUnlock()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')

	for conn := range s.connections {
		conn.Write(data)
	}
}

func (s *AgentServer) Stop() {
	close(s.done)
	s.listener.Close()
	CleanupSocket()
	s.wg.Wait()
}

// ============================================================================
// CONTROLLER CLIENT
// ============================================================================

type Event struct {
	Type string // peer_connected, peer_disconnected
	Data string // peer ID
}

// AsyncMessage carries a response with optional command ID for correlation
type AsyncMessage struct {
	Text  string // response text
	CmdID string // originating command ID (may be empty)
}

type ControllerClient struct {
	conn             net.Conn
	reader           *bufio.Reader
	responseCh       chan string
	asyncCh          chan AsyncMessage
	eventCh          chan Event
	pendingResponses map[string]chan string
	mu               sync.Mutex
}

func NewControllerClient() (*ControllerClient, error) {
	if !SocketExists() {
		return nil, fmt.Errorf("no agent running")
	}

	conn, err := ConnectToAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	c := &ControllerClient{
		conn:             conn,
		reader:           bufio.NewReader(conn),
		responseCh:       make(chan string, 1),
		asyncCh:          make(chan AsyncMessage, 100),
		eventCh:          make(chan Event, 100),
		pendingResponses: make(map[string]chan string),
	}

	go c.readLoop()
	return c, nil
}

func (c *ControllerClient) readLoop() {
	for {
		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			close(c.asyncCh)
			close(c.eventCh)
			return
		}

		var msg message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		if msg.Event != "" {
			c.eventCh <- Event{Type: msg.Event, Data: msg.Data}
		} else if msg.IsAsync {
			if msg.CmdID != "" {
				c.mu.Lock()
				ch, ok := c.pendingResponses[msg.CmdID]
				c.mu.Unlock()
				if ok {
					ch <- msg.Response
					continue
				}
			}
			c.asyncCh <- AsyncMessage{Text: msg.Response, CmdID: msg.CmdID}
		} else {
			c.responseCh <- msg.Response
		}
	}
}

func (c *ControllerClient) Send(cmd string, args ...string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	msg := message{Cmd: cmd, Args: args}
	data, _ := json.Marshal(msg)
	if _, err := c.conn.Write(append(data, '\n')); err != nil {
		return "", err
	}

	resp := <-c.responseCh
	return resp, nil
}

func (c *ControllerClient) WaitAsync(cmdID string, timeout time.Duration) (string, error) {
	ch := make(chan string, 1)

	c.mu.Lock()
	c.pendingResponses[cmdID] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendingResponses, cmdID)
		c.mu.Unlock()
	}()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout")
	}
}

func (c *ControllerClient) AsyncMessages() <-chan AsyncMessage { return c.asyncCh }
func (c *ControllerClient) Events() <-chan Event               { return c.eventCh }
func (c *ControllerClient) Close()                             { c.conn.Close() }

// ============================================================================
// HELPERS
// ============================================================================

func ParseInput(input string) (cmd string, args []string) {
	if input == "" {
		return "", nil
	}

	var parts []string
	var current strings.Builder
	inQuotes := false
	quoteChar := rune(0)

	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inQuotes {
			if r == quoteChar {
				inQuotes = false
			} else {
				current.WriteRune(r)
			}
		} else {
			if r == '"' || r == '\'' {
				inQuotes = true
				quoteChar = r
			} else if r == ' ' || r == '\t' {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(r)
			}
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}
