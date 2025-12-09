//go:build windows

package ipc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

const SocketPath = `\\.\pipe\mojo.8524.3120.123456789`

// CreateListener creates a Windows Named Pipe listener
func CreateListener() (net.Listener, error) {
	config := &winio.PipeConfig{
		SecurityDescriptor: "", // Default security (current user)
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(SocketPath, config)
}

// ConnectToAgent connects to the agent via Named Pipe
func ConnectToAgent() (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(SocketPath, &timeout)
}

// CleanupSocket is a no-op on Windows (pipes are auto-cleaned)
func CleanupSocket() {
	// Named pipes are automatically cleaned up when the listener closes
}

// SocketExists checks if the agent pipe exists
func SocketExists() bool {
	conn, err := winio.DialPipe(SocketPath, nil)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
