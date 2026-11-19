package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	conn              net.Conn
	reader            *bufio.Reader
	syncWaiters       []chan string // FIFO; nil marks an abandoned request
	asyncCh           chan AsyncMessage
	eventCh           chan Event
	pendingResponses  map[string]chan string
	bufferedResponses map[string]string
	bufferedOrder     []string
	mu                sync.Mutex
	sendGate          chan struct{}
	done              chan struct{}
	closeOnce         sync.Once
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
		conn:              conn,
		reader:            bufio.NewReader(conn),
		asyncCh:           make(chan AsyncMessage, 100),
		eventCh:           make(chan Event, 100),
		pendingResponses:  make(map[string]chan string),
		bufferedResponses: make(map[string]string),
		sendGate:          make(chan struct{}, 1),
		done:              make(chan struct{}),
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
				if ok {
					select {
					case ch <- msg.Response:
					default:
					}
				} else {
					c.bufferResponse(msg.CmdID, msg.Response)
				}
				c.mu.Unlock()
				if ok {
					continue
				}
			}
			select {
			case c.asyncCh <- AsyncMessage{Text: msg.Response, CmdID: msg.CmdID}:
			case <-c.done:
				return
			}
		} else {
			// The agent answers each connection's commands in order, so the
			// oldest outstanding request owns this reply. Replies to abandoned
			// requests and unsolicited replies are discarded.
			c.mu.Lock()
			if len(c.syncWaiters) > 0 {
				reply := c.syncWaiters[0]
				c.syncWaiters = c.syncWaiters[1:]
				if reply != nil {
					reply <- msg.Response
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *ControllerClient) Send(cmd string, args ...string) (string, error) {
	return c.SendContext(context.Background(), cmd, args...)
}

// SendContext serializes requests because synchronous replies have no request
// ID; they are matched to requests in order. Cancellation while the request is
// being written closes the connection because a partial line would corrupt
// framing. Cancellation while awaiting the reply abandons it, keeping the
// connection usable: the late reply is discarded rather than returned to the
// next request.
func (c *ControllerClient) SendContext(ctx context.Context, cmd string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	select {
	case c.sendGate <- struct{}{}:
		defer func() { <-c.sendGate }()
	case <-ctx.Done():
		return "", ctx.Err()
	case <-c.done:
		return "", net.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	reply := make(chan string, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return "", net.ErrClosed
	default:
	}
	c.syncWaiters = append(c.syncWaiters, reply)
	c.mu.Unlock()

	data, _ := json.Marshal(message{Cmd: cmd, Args: args})
	line := append(data, '\n')
	// Cancellation interrupts a blocked write with a past deadline. Only an
	// incomplete line closes the connection; a complete request is abandoned
	// below so the connection stays usable.
	interrupted := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		defer close(interrupted)
		if c.conn.SetWriteDeadline(time.Unix(1, 0)) != nil {
			c.Close()
		}
	})
	n, err := c.conn.Write(line)
	if !stopCancel() {
		<-interrupted
		c.conn.SetWriteDeadline(time.Time{})
	}
	if n < len(line) {
		c.Close()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err == nil {
			err = io.ErrShortWrite
		}
		return "", err
	}

	select {
	case resp := <-reply:
		return resp, nil
	case <-ctx.Done():
		if resp, ok := c.abandonSync(reply); ok {
			return resp, nil
		}
		return "", ctx.Err()
	case <-c.done:
		if resp, ok := c.abandonSync(reply); ok {
			return resp, nil
		}
		return "", net.ErrClosed
	}
}

// abandonSync atomically turns a request into a tombstone so its eventual
// reply is discarded, unless the reply was already delivered.
func (c *ControllerClient) abandonSync(reply chan string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, waiter := range c.syncWaiters {
		if waiter == reply {
			c.syncWaiters[i] = nil
		}
	}
	select {
	case resp := <-reply:
		return resp, true
	default:
		return "", false
	}
}

func (c *ControllerClient) WaitAsync(cmdID string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := c.WaitAsyncContext(ctx, cmdID)
	if err == context.DeadlineExceeded {
		// The interactive controller recognizes this legacy error string.
		return "", fmt.Errorf("timeout")
	}
	return response, err
}

// WaitAsyncContext also accepts a result that arrived before the waiter.
func (c *ControllerClient) WaitAsyncContext(ctx context.Context, cmdID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ch := make(chan string, 1)

	c.mu.Lock()
	if response, ok := c.bufferedResponses[cmdID]; ok {
		delete(c.bufferedResponses, cmdID)
		for i, id := range c.bufferedOrder {
			if id == cmdID {
				c.bufferedOrder = append(c.bufferedOrder[:i], c.bufferedOrder[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return response, nil
	}
	if _, ok := c.pendingResponses[cmdID]; ok {
		c.mu.Unlock()
		return "", fmt.Errorf("already waiting for command %q", cmdID)
	}
	c.pendingResponses[cmdID] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.pendingResponses[cmdID] == ch {
			delete(c.pendingResponses, cmdID)
		}
		c.mu.Unlock()
	}()

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		if response, ok := c.finishWait(cmdID, ch); ok {
			return response, nil
		}
		return "", ctx.Err()
	case <-c.done:
		if response, ok := c.finishWait(cmdID, ch); ok {
			return response, nil
		}
		return "", net.ErrClosed
	}
}

// finishWait atomically removes a cancelled waiter and checks for a result
// already delivered to it. Later output is routed to AsyncMessages instead.
func (c *ControllerClient) finishWait(cmdID string, ch chan string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingResponses[cmdID] == ch {
		delete(c.pendingResponses, cmdID)
	}
	select {
	case response := <-ch:
		return response, true
	default:
		return "", false
	}
}

// bufferResponse requires mu. Bound unmatched results because interactive
// controllers may consume AsyncMessages without ever calling WaitAsync.
func (c *ControllerClient) bufferResponse(cmdID, response string) {
	if _, exists := c.bufferedResponses[cmdID]; exists {
		return
	}
	const maxBufferedResponses = 100
	if len(c.bufferedOrder) == maxBufferedResponses {
		delete(c.bufferedResponses, c.bufferedOrder[0])
		c.bufferedOrder = c.bufferedOrder[1:]
	}
	c.bufferedOrder = append(c.bufferedOrder, cmdID)
	c.bufferedResponses[cmdID] = response
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
