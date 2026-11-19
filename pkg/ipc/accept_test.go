package ipc

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedListener returns the scripted errors in order, then the given
// connection once, then blocks until closed.
type scriptedListener struct {
	errs      []error
	conn      net.Conn
	calls     atomic.Int64
	called    chan struct{}
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

func newScriptedListener(conn net.Conn, errs ...error) *scriptedListener {
	return &scriptedListener{errs: errs, conn: conn, called: make(chan struct{}, 100), done: make(chan struct{})}
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	select {
	case l.called <- struct{}{}:
	default:
	}
	l.mu.Lock()
	if len(l.errs) > 0 {
		err := l.errs[0]
		if len(l.errs) > 1 || l.conn != nil {
			l.errs = l.errs[1:]
		}
		l.mu.Unlock()
		return nil, err
	}
	conn := l.conn
	l.conn = nil
	l.mu.Unlock()
	if conn != nil {
		return conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *scriptedListener) Close() error   { l.closeOnce.Do(func() { close(l.done) }); return nil }
func (l *scriptedListener) Addr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

type temporaryError struct{}

func (temporaryError) Error() string   { return "accept: too many open files" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

func TestAcceptLoopStopsWhenListenerClosedExternally(t *testing.T) {
	t.Parallel()
	// The last scripted error repeats forever, like a listener closed by
	// something other than Stop.
	listener := newScriptedListener(nil, &net.OpError{Op: "accept", Net: "unix", Err: net.ErrClosed})
	server := NewAgentServerWithListener(listener, func(string, []string) string { return "" })
	receive(t, listener.called)
	select {
	case <-listener.called:
		t.Fatalf("accept loop retried a closed listener (%d calls)", listener.calls.Load())
	case <-time.After(100 * time.Millisecond):
	}
	stopped := make(chan struct{})
	go func() { server.Stop(); close(stopped) }()
	receive(t, stopped)
}

func TestAcceptLoopBacksOffAndRecoversFromTransientErrors(t *testing.T) {
	t.Parallel()
	serverConn, clientConn := net.Pipe()
	listener := newScriptedListener(serverConn, temporaryError{}, temporaryError{}, temporaryError{})
	server := NewAgentServerWithListener(listener, func(cmd string, _ []string) string { return "response:" + cmd })
	t.Cleanup(server.Stop)
	client := NewControllerClientWithConn(clientConn)
	t.Cleanup(client.Close)
	if got, err := client.Send("id"); err != nil || got != "response:id" {
		t.Fatalf("Send after transient accept errors = %q, %v", got, err)
	}
	if calls := listener.calls.Load(); calls < 4 {
		t.Fatalf("Accept calls = %d, want at least 4", calls)
	}
}

func TestAcceptLoopPersistentErrorDoesNotSpinAndStopsPromptly(t *testing.T) {
	t.Parallel()
	// The single scripted error repeats forever.
	listener := newScriptedListener(nil, temporaryError{})
	server := NewAgentServerWithListener(listener, func(string, []string) string { return "" })
	receive(t, listener.called)
	time.Sleep(50 * time.Millisecond)
	// Backoff starts at 5ms and doubles to a 1s cap, so even a badly delayed
	// sleep allows only a few attempts, while a busy loop makes thousands.
	if calls := listener.calls.Load(); calls > 25 {
		t.Fatalf("accept loop spun: %d calls in 50ms", calls)
	}
	start := time.Now()
	server.Stop()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %v while backing off", elapsed)
	}
}
