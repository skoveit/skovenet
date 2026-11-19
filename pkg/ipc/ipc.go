package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
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
	connections map[*agentConnection]bool
	connMu      sync.RWMutex
	done        chan struct{}
	wg          sync.WaitGroup
	stopOnce    sync.Once
}

type agentConnection struct {
	net.Conn
	writeMu sync.Mutex
}

func (c *agentConnection) write(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.Write(data)
	return err
}

func NewAgentServer(handler CommandHandler) (*AgentServer, error) {
	listener, err := CreateListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %w", err)
	}

	return NewAgentServerWithListener(listener, handler), nil
}

// NewAgentServerWithListener serves IPC on listener and takes ownership of it.
// It allows callers to use an isolated endpoint instead of the default socket.
func NewAgentServerWithListener(listener net.Listener, handler CommandHandler) *AgentServer {
	s := &AgentServer{
		listener:    listener,
		handler:     handler,
		connections: make(map[*agentConnection]bool),
		done:        make(chan struct{}),
	}

	s.wg.Add(1)
	go s.acceptLoop()

	logger.Debug("IPC server started on %s", listener.Addr())
	return s
}

func (s *AgentServer) acceptLoop() {
	defer s.wg.Done()
	var backoff time.Duration
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			// A closed listener never accepts again; retrying would spin.
			if errors.Is(err, net.ErrClosed) {
				logger.Debug("IPC listener closed: %v", err)
				return
			}
			// Back off on other errors, such as descriptor exhaustion, so a
			// persistent failure does not busy-loop.
			backoff = min(max(2*backoff, 5*time.Millisecond), time.Second)
			logger.Debug("IPC accept failed: %v; retrying in %v", err, backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
				continue
			case <-s.done:
				timer.Stop()
				return
			}
		}
		backoff = 0
		client := &agentConnection{Conn: conn}
		s.connMu.Lock()
		select {
		case <-s.done:
			s.connMu.Unlock()
			conn.Close()
			return
		default:
		}
		s.connections[client] = true
		s.wg.Add(1)
		s.connMu.Unlock()
		go s.handleConn(client)
	}
}

func (s *AgentServer) handleConn(conn *agentConnection) {
	defer s.wg.Done()
	defer conn.Close()

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
		if err := conn.write(append(respData, '\n')); err != nil {
			return
		}

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
	connections := make([]*agentConnection, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.connMu.RUnlock()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')

	for _, conn := range connections {
		if err := conn.write(data); err != nil {
			conn.Close()
		}
	}
}

func (s *AgentServer) Stop() {
	s.stopOnce.Do(func() {
		close(s.done)
		s.listener.Close()
		s.connMu.RLock()
		for conn := range s.connections {
			conn.Close()
		}
		s.connMu.RUnlock()
		s.wg.Wait()
	})
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
	sendMu           sync.Mutex
	done             chan struct{}
	closeOnce        sync.Once
}

func NewControllerClient() (*ControllerClient, error) {
	if !SocketExists() {
		return nil, fmt.Errorf("no agent running")
	}

	conn, err := ConnectToAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return NewControllerClientWithConn(conn), nil
}

// NewControllerClientWithConn starts an IPC client and takes ownership of conn.
func NewControllerClientWithConn(conn net.Conn) *ControllerClient {
	c := &ControllerClient{
		conn:             conn,
		reader:           bufio.NewReader(conn),
		responseCh:       make(chan string, 1),
		asyncCh:          make(chan AsyncMessage, 100),
		eventCh:          make(chan Event, 100),
		pendingResponses: make(map[string]chan string),
		done:             make(chan struct{}),
	}

	go c.readLoop()
	return c
}

func (c *ControllerClient) readLoop() {
	defer close(c.asyncCh)
	defer close(c.eventCh)
	defer c.Close()
	for {
		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var msg message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		if msg.Event != "" {
			select {
			case c.eventCh <- Event{Type: msg.Event, Data: msg.Data}:
			case <-c.done:
				return
			}
		} else if msg.IsAsync {
			if msg.CmdID != "" {
				c.mu.Lock()
				ch, ok := c.pendingResponses[msg.CmdID]
				c.mu.Unlock()
				if ok {
					select {
					case ch <- msg.Response:
					default:
					}
					continue
				}
			}
			select {
			case c.asyncCh <- AsyncMessage{Text: msg.Response, CmdID: msg.CmdID}:
			case <-c.done:
				return
			}
		} else {
			select {
			case c.responseCh <- msg.Response:
			case <-c.done:
				return
			}
		}
	}
}

func (c *ControllerClient) Send(cmd string, args ...string) (string, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	select {
	case <-c.done:
		return "", net.ErrClosed
	default:
	}

	msg := message{Cmd: cmd, Args: args}
	data, _ := json.Marshal(msg)
	if _, err := c.conn.Write(append(data, '\n')); err != nil {
		return "", err
	}

	select {
	case resp := <-c.responseCh:
		return resp, nil
	case <-c.done:
		select {
		case resp := <-c.responseCh:
			return resp, nil
		default:
			return "", net.ErrClosed
		}
	}
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
	case <-c.done:
		select {
		case resp := <-ch:
			return resp, nil
		default:
			return "", net.ErrClosed
		}
	}
}

func (c *ControllerClient) AsyncMessages() <-chan AsyncMessage { return c.asyncCh }
func (c *ControllerClient) Events() <-chan Event               { return c.eventCh }
func (c *ControllerClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

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
