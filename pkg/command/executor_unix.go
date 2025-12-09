//go:build !windows

package command

import (
	"bytes"
	"os/exec"
	"strings"
)

type Executor struct{}

func NewExecutor() *Executor {
	return &Executor{}
}

func (e *Executor) Execute(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil
	}

	// Run through shell for proper variable expansion, pipes, etc.
	cmd := exec.Command("/bin/sh", "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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

	// If there was an error but no output, include the error message
	if err != nil && output == "" {
		output = err.Error()
	}

	return output, err
}
