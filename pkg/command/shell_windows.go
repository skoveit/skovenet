//go:build windows

package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// ExecTimeout is the maximum time a command can run before being killed
	ExecTimeout = 30 * time.Second
	// MaxOutputSize is the maximum bytes captured from stdout+stderr (1 MB)
	MaxOutputSize = 1 << 20
)

type ShellCommand struct{}

func NewShellCommand() *ShellCommand {
	return &ShellCommand{}
}

func (s *ShellCommand) Name() string {
	return "shell"
}

func (s *ShellCommand) Description() string {
	return "Executes raw shell commands"
}

func (s *ShellCommand) Execute(ctx context.Context, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil
	}

	// Create context with timeout to prevent commands running forever
	execCtx, cancel := context.WithTimeout(ctx, ExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "cmd.exe", "/C", command)

	// Use limited writers to cap output size
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{buf: &stdout, n: MaxOutputSize}
	cmd.Stderr = &limitWriter{buf: &stderr, n: MaxOutputSize}

	err := cmd.Run()

	// Combine stdout and stderr
	output := stdout.String()
	errOutput := stderr.String()

	if errOutput != "" {
		if output != "" {
			output += "\n"
		}
		output += errOutput
	}

	output = strings.TrimSpace(output)

	// If the context deadline was exceeded, report it
	if execCtx.Err() == context.DeadlineExceeded {
		output = fmt.Sprintf("[killed: exceeded %s timeout]\n%s", ExecTimeout, output)
		return output, execCtx.Err()
	}

	// Indicate if output was truncated
	if stdout.Len() >= MaxOutputSize || stderr.Len() >= MaxOutputSize {
		output += "\n[truncated: output exceeded 1MB limit]"
	}

	// If there was an error but no output, include the error message
	if err != nil && output == "" {
		output = err.Error()
	}

	return output, err
}

// limitWriter wraps a buffer and stops writing after n bytes
type limitWriter struct {
	buf *bytes.Buffer
	n   int64
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	remaining := lw.n - int64(lw.buf.Len())
	if remaining <= 0 {
		return len(p), nil // silently discard
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	return lw.buf.Write(p)
}
