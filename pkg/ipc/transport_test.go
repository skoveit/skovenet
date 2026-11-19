package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
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

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return l.conn.LocalAddr() }

func testAgent(t *testing.T, handler CommandHandler) (*AgentServer, *ControllerClient) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := NewAgentServerWithListener(&pipeListener{conn: serverConn, done: make(chan struct{})}, handler)
	client := NewControllerClientWithConn(clientConn)
	t.Cleanup(func() { client.Close(); server.Stop() })
	return server, client
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transport operation")
		var zero T
		return zero
	}
}

func TestAgentClientRoundTrip(t *testing.T) {
	t.Parallel()
	_, client := testAgent(t, func(cmd string, args []string) string {
		return cmd + ":" + strings.Join(args, "|")
	})
	response, err := client.Send("send", "peer", "a path with spaces", "first\nsecond")
	if err != nil {
		t.Fatal(err)
	}
	if want := "send:peer|a path with spaces|first\nsecond"; response != want {
		t.Fatalf("response = %q, want %q", response, want)
	}
}

func TestAgentClientConcurrentCommands(t *testing.T) {
	t.Parallel()
	_, client := testAgent(t, func(cmd string, args []string) string { return cmd })
	errors := make(chan error, 16)
	for i := range 16 {
		go func() {
			want := fmt.Sprintf("command-%d", i)
			got, err := client.Send(want)
			if err == nil && got != want {
				err = fmt.Errorf("response = %q, want %q", got, want)
			}
			errors <- err
		}()
	}
	for range 16 {
		if err := receive(t, errors); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentStopClosesLiveClients(t *testing.T) {
	t.Parallel()
	server, client := testAgent(t, func(cmd string, args []string) string { return "ready" })
	if _, err := client.Send("ready"); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { server.Stop(); server.Stop(); close(stopped) }()
	receive(t, stopped)
	if _, err := client.Send("after-stop"); err == nil {
		t.Fatal("Send succeeded after Stop")
	}
}

func TestClientDisconnectUnblocksPendingOperations(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"send", "wait"} {
		t.Run(operation, func(t *testing.T) {
			remote, local := net.Pipe()
			client := NewControllerClientWithConn(local)
			t.Cleanup(client.Close)
			done := make(chan error, 1)
			if operation == "send" {
				go func() { _, err := client.Send("pending"); done <- err }()
				var request message
				if err := json.NewDecoder(remote).Decode(&request); err != nil {
					t.Fatal(err)
				}
			} else {
				go func() { _, err := client.WaitAsync("pending", time.Minute); done <- err }()
			}
			remote.Close()
			if err := receive(t, done); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("operation error = %v, want closed connection", err)
			}
		})
	}
}

func TestClientReceivesPushesBeforeCommandResponse(t *testing.T) {
	t.Parallel()
	remote, local := net.Pipe()
	client := NewControllerClientWithConn(local)
	t.Cleanup(client.Close)
	t.Cleanup(func() { remote.Close() })
	remoteDone := make(chan error, 1)
	go func() {
		var request message
		if err := json.NewDecoder(remote).Decode(&request); err != nil {
			remoteDone <- err
			return
		}
		for _, msg := range []message{
			{Event: "peer_connected", Data: "peer-1", IsAsync: true},
			{Response: "early-output", CmdID: "cmd-1", IsAsync: true},
			{Response: "cmd-1"},
		} {
			if err := json.NewEncoder(remote).Encode(msg); err != nil {
				remoteDone <- err
				return
			}
		}
		remoteDone <- nil
	}()
	done := make(chan error, 1)
	go func() {
		response, err := client.Send("send", "peer-1", "info")
		if err == nil && response != "cmd-1" {
			err = fmt.Errorf("response = %q", response)
		}
		done <- err
	}()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, remoteDone); err != nil {
		t.Fatal(err)
	}
	if event := receive(t, client.Events()); event != (Event{Type: "peer_connected", Data: "peer-1"}) {
		t.Fatalf("event = %#v", event)
	}
	if msg := receive(t, client.AsyncMessages()); msg != (AsyncMessage{Text: "early-output", CmdID: "cmd-1"}) {
		t.Fatalf("async = %#v", msg)
	}
}

func TestAgentPushesReachController(t *testing.T) {
	t.Parallel()
	server, client := testAgent(t, func(cmd string, args []string) string { return "ready" })
	if _, err := client.Send("ready"); err != nil {
		t.Fatal(err)
	}
	server.Push("notice")
	server.PushWithCmdID("output", "cmd-2")
	server.PushEvent("peer_disconnected", "peer-2")
	if msg := receive(t, client.AsyncMessages()); msg != (AsyncMessage{Text: "notice"}) {
		t.Fatalf("notice = %#v", msg)
	}
	if msg := receive(t, client.AsyncMessages()); msg != (AsyncMessage{Text: "output", CmdID: "cmd-2"}) {
		t.Fatalf("output = %#v", msg)
	}
	if event := receive(t, client.Events()); event != (Event{Type: "peer_disconnected", Data: "peer-2"}) {
		t.Fatalf("event = %#v", event)
	}
}

func TestAgentStopInterruptsBlockedPush(t *testing.T) {
	t.Parallel()
	serverConn, remote := net.Pipe()
	server := NewAgentServerWithListener(&pipeListener{conn: serverConn, done: make(chan struct{})}, func(cmd string, args []string) string { return "ready" })
	t.Cleanup(server.Stop)
	t.Cleanup(func() { remote.Close() })
	if err := json.NewEncoder(remote).Encode(message{Cmd: "ready"}); err != nil {
		t.Fatal(err)
	}
	var response message
	if err := json.NewDecoder(remote).Decode(&response); err != nil {
		t.Fatal(err)
	}
	pushed := make(chan struct{})
	go func() { server.Push(strings.Repeat("x", 65536)); close(pushed) }()
	stopped := make(chan struct{})
	go func() { server.Stop(); close(stopped) }()
	receive(t, stopped)
	receive(t, pushed)
}

func TestAgentQuitReturnsFinalResponse(t *testing.T) {
	t.Parallel()
	_, client := testAgent(t, func(cmd string, args []string) string { return "goodbye" })
	if got, err := client.Send("quit"); err != nil || got != "goodbye" {
		t.Fatalf("quit response = %q, %v", got, err)
	}
	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatal("events channel remained open")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not observe disconnect after quit")
	}
}
