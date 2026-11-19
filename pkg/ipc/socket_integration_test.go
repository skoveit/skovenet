//go:build integration && !windows

package ipc

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestIsolatedUnixSocketLifecycle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := NewAgentServerWithListener(listener, func(cmd string, args []string) string { return "response:" + cmd })
	t.Cleanup(server.Stop)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	client := NewControllerClientWithConn(conn)
	t.Cleanup(client.Close)
	if got, err := client.Send("id"); err != nil || got != "response:id" {
		t.Fatalf("Send = %q, %v", got, err)
	}
	server.Stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket after Stop: %v", err)
	}
}
