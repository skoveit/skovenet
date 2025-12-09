//go:build !windows

package ipc

import (
	"net"
	"os"
)

const SocketPath = "/tmp/nostalgia-agent.sock"

// CreateListener creates a Unix domain socket listener
func CreateListener() (net.Listener, error) {
	os.Remove(SocketPath)
	return net.Listen("unix", SocketPath)
}

// ConnectToAgent connects to the agent via Unix socket
func ConnectToAgent() (net.Conn, error) {
	return net.Dial("unix", SocketPath)
}

// CleanupSocket removes the socket file
func CleanupSocket() {
	os.Remove(SocketPath)
}

// SocketExists checks if the agent socket exists
func SocketExists() bool {
	_, err := os.Stat(SocketPath)
	return err == nil
}
