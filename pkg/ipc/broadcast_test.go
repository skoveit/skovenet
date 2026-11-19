package ipc

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// queueListener accepts the connections queued on conns until closed.
type queueListener struct {
	conns     chan net.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func (l *queueListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *queueListener) Close() error   { l.closeOnce.Do(func() { close(l.done) }); return nil }
func (l *queueListener) Addr() net.Addr { return queueAddr{} }

type queueAddr struct{}

func (queueAddr) Network() string { return "queue" }
func (queueAddr) String() string  { return "queue" }

func TestBroadcastDisconnectsStalledController(t *testing.T) {
	t.Parallel()
	listener := &queueListener{conns: make(chan net.Conn, 2), done: make(chan struct{})}
	server := NewAgentServerWithListener(listener, func(cmd string, _ []string) string { return cmd })
	// Set before any connection is queued; the channel handoff orders it
	// before every read by connection handlers and broadcasts.
	server.writeTimeout = 50 * time.Millisecond
	t.Cleanup(server.Stop)

	// The stalled controller never reads; with net.Pipe every write to it
	// blocks until a reader appears.
	stalledRemote, stalledLocal := net.Pipe()
	t.Cleanup(func() { stalledRemote.Close() })
	listener.conns <- stalledLocal
	healthyServer, healthyClient := net.Pipe()
	listener.conns <- healthyServer
	client := NewControllerClientWithConn(healthyClient)
	t.Cleanup(client.Close)
	// Connections are registered in accept order, so a served request on the
	// healthy connection proves the stalled one is registered too.
	if got, err := client.Send("ready"); err != nil || got != "ready" {
		t.Fatalf("Send = %q, %v", got, err)
	}

	pushed := make(chan struct{})
	go func() { server.PushEvent("peer_connected", "peer-1"); close(pushed) }()
	receive(t, pushed)
	if event := receive(t, client.Events()); event != (Event{Type: "peer_connected", Data: "peer-1"}) {
		t.Fatalf("event = %#v", event)
	}

	readErr := make(chan error, 1)
	go func() { _, err := stalledRemote.Read(make([]byte, 1)); readErr <- err }()
	if err := receive(t, readErr); err != io.EOF {
		t.Fatalf("stalled controller read = %v, want EOF after disconnect", err)
	}

	// The healthy controller keeps receiving later broadcasts.
	server.PushWithCmdID("result", "cmd-1")
	if msg := receive(t, client.AsyncMessages()); msg != (AsyncMessage{Text: "result", CmdID: "cmd-1"}) {
		t.Fatalf("async = %#v", msg)
	}
}
