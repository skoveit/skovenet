package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

type writeNotifyingConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *writeNotifyingConn) Write(data []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(data)
}

// flushReadLoop proves every earlier line has been processed by the read loop:
// lines are handled in order, so receiving a later event is a barrier.
func flushReadLoop(t *testing.T, client *ControllerClient, encoder *json.Encoder) {
	t.Helper()
	if err := encoder.Encode(message{Event: "barrier", IsAsync: true}); err != nil {
		t.Fatal(err)
	}
	if event := receive(t, client.Events()); event.Type != "barrier" {
		t.Fatalf("event = %#v", event)
	}
}

func assertNoAsyncMessage(t *testing.T, client *ControllerClient) {
	t.Helper()
	select {
	case msg := <-client.AsyncMessages():
		t.Fatalf("retained result was also delivered to AsyncMessages: %#v", msg)
	default:
	}
}

func TestWaitAsyncAcceptsEarlyRetainedResult(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	client.RetainUnclaimedResults()
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	encoder := json.NewEncoder(remote)
	if err := encoder.Encode(message{Response: "early-result", CmdID: "cmd-early", IsAsync: true}); err != nil {
		t.Fatal(err)
	}
	flushReadLoop(t, client, encoder)
	assertNoAsyncMessage(t, client)
	if got, err := client.WaitAsync("cmd-early", time.Second); err != nil || got != "early-result" {
		t.Fatalf("WaitAsync = %q, %v", got, err)
	}
}

// The interactive controller prints AsyncMessages and also prints what
// WaitAsync returns, so without opting in an early result must reach exactly
// one of them.
func TestDefaultClientDeliversEarlyResultOnlyToAsyncMessages(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	if err := json.NewEncoder(remote).Encode(message{Response: "early-result", CmdID: "cmd-early", IsAsync: true}); err != nil {
		t.Fatal(err)
	}
	if msg := receive(t, client.AsyncMessages()); msg != (AsyncMessage{Text: "early-result", CmdID: "cmd-early"}) {
		t.Fatalf("async = %#v", msg)
	}
	if got, err := client.WaitAsync("cmd-early", 10*time.Millisecond); err == nil || err.Error() != "timeout" {
		t.Fatalf("early result delivered twice: WaitAsync = %q, %v", got, err)
	}
}

func TestWaitAsyncCorrelatesOutOfOrderResults(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	client.RetainUnclaimedResults()
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	encoder := json.NewEncoder(remote)
	for _, id := range []string{"second", "first"} {
		if err := encoder.Encode(message{Response: "output-" + id, CmdID: id, IsAsync: true}); err != nil {
			t.Fatal(err)
		}
	}
	flushReadLoop(t, client, encoder)
	for _, id := range []string{"first", "second"} {
		if got, err := client.WaitAsync(id, time.Second); err != nil || got != "output-"+id {
			t.Fatalf("WaitAsync(%q) = %q, %v", id, got, err)
		}
	}
}

func TestWaitAsyncBoundsRetainedResults(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	client.RetainUnclaimedResults()
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	encoder := json.NewEncoder(remote)
	for i := range 101 {
		id := fmt.Sprintf("cmd-%d", i)
		if err := encoder.Encode(message{Response: id, CmdID: id, IsAsync: true}); err != nil {
			t.Fatal(err)
		}
	}
	flushReadLoop(t, client, encoder)
	if _, err := client.WaitAsync("cmd-0", time.Millisecond); err == nil || err.Error() != "timeout" {
		t.Fatalf("oldest result was not evicted: %v", err)
	}
	for _, id := range []string{"cmd-1", "cmd-100"} {
		if got, err := client.WaitAsync(id, time.Second); err != nil || got != id {
			t.Fatalf("WaitAsync(%q) = %q, %v", id, got, err)
		}
	}
}

func TestWaitAsyncLegacyTimeoutPreservesLateOutput(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	if _, err := client.WaitAsync("cmd-late", time.Millisecond); err == nil || err.Error() != "timeout" {
		t.Fatalf("legacy timeout = %v, want exact timeout string", err)
	}
	if err := json.NewEncoder(remote).Encode(message{Response: "late-output", CmdID: "cmd-late", IsAsync: true}); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, client.AsyncMessages()); got != (AsyncMessage{Text: "late-output", CmdID: "cmd-late"}) {
		t.Fatalf("late output = %#v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := client.WaitAsyncContext(ctx, "missing"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context timeout = %v", err)
	}
}

func TestWaitAsyncCancellationRacingReplyDoesNotLoseOutput(t *testing.T) {
	t.Parallel()
	for _, retain := range []bool{false, true} {
		for i := range 30 {
			t.Run(fmt.Sprintf("retain=%t/race-%d", retain, i), func(t *testing.T) {
				remote, local := net.Pipe()
				client := NewControllerClientWithConn(local)
				if retain {
					client.RetainUnclaimedResults()
				}
				t.Cleanup(client.Close)
				t.Cleanup(func() { remote.Close() })
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				type result struct {
					text string
					err  error
				}
				done := make(chan result, 1)
				go func() { text, err := client.WaitAsyncContext(ctx, "cmd-race"); done <- result{text, err} }()
				if err := json.NewEncoder(remote).Encode(message{Response: "racing-output", CmdID: "cmd-race", IsAsync: true}); err != nil {
					t.Fatal(err)
				}
				cancel()
				got := receive(t, done)
				switch {
				case got.err == nil:
					if got.text != "racing-output" {
						t.Fatalf("result = %q", got.text)
					}
				case !errors.Is(got.err, context.Canceled):
					t.Fatal(got.err)
				case retain:
					if text, err := client.WaitAsync("cmd-race", time.Second); err != nil || text != "racing-output" {
						t.Fatalf("retained result = %q, %v", text, err)
					}
				default:
					if notification := receive(t, client.AsyncMessages()); notification.Text != "racing-output" {
						t.Fatalf("notification = %#v", notification)
					}
				}
			})
		}
	}
}

func TestWaitAsyncContextCancellation(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	client.RetainUnclaimedResults()
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	encoder := json.NewEncoder(remote)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.WaitAsyncContext(ctx, "cmd"); done <- err }()
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitAsyncContext error = %v", err)
	}
	// A cancelled waiter must not consume a later result for the same ID.
	if err := encoder.Encode(message{Response: "later-result", CmdID: "cmd", IsAsync: true}); err != nil {
		t.Fatal(err)
	}
	flushReadLoop(t, client, encoder)
	if got, err := client.WaitAsync("cmd", time.Second); err != nil || got != "later-result" {
		t.Fatalf("later result = %q, %v", got, err)
	}
}

func TestSendContextCancelsBlockedWrite(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	started := make(chan struct{})
	client := NewControllerClientWithConn(&writeNotifyingConn{Conn: local, started: started})
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.SendContext(ctx, "blocked"); done <- err }()
	receive(t, started)
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("SendContext error = %v", err)
	}
}

func TestSendContextCancelledReplyIsNotReturnedToNextRequest(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	decoder, encoder := json.NewDecoder(remote), json.NewEncoder(remote)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.SendContext(ctx, "slow"); done <- err }()
	var request message
	if err := decoder.Decode(&request); err != nil || request.Cmd != "slow" {
		t.Fatalf("request = %#v, %v", request, err)
	}
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("SendContext error = %v", err)
	}

	type result struct {
		text string
		err  error
	}
	next := make(chan result, 1)
	go func() { text, err := client.Send("next"); next <- result{text, err} }()
	if err := decoder.Decode(&request); err != nil || request.Cmd != "next" {
		t.Fatalf("connection unusable after cancellation: %#v, %v", request, err)
	}
	// The agent answers in order: the abandoned request's late reply arrives
	// while "next" is already waiting and must not be returned to it.
	for _, response := range []string{"slow-reply", "next-reply"} {
		if err := encoder.Encode(message{Response: response}); err != nil {
			t.Fatal(err)
		}
	}
	if got := receive(t, next); got.err != nil || got.text != "next-reply" {
		t.Fatalf("Send(next) = %q, %v", got.text, got.err)
	}
}

func TestSendDiscardsUnsolicitedReply(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	decoder, encoder := json.NewDecoder(remote), json.NewEncoder(remote)
	if err := encoder.Encode(message{Response: "unsolicited"}); err != nil {
		t.Fatal(err)
	}
	// Receiving a later event proves the read loop already handled the
	// unsolicited reply before any request was outstanding.
	if err := encoder.Encode(message{Event: "peer_connected", Data: "peer-1", IsAsync: true}); err != nil {
		t.Fatal(err)
	}
	receive(t, client.Events())
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() { text, err := client.Send("id"); done <- result{text, err} }()
	var request message
	if err := decoder.Decode(&request); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(message{Response: "id-reply"}); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, done); got.err != nil || got.text != "id-reply" {
		t.Fatalf("Send = %q, %v", got.text, got.err)
	}
}
