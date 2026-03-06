package command

import (
	"context"
	"strings"
)

type Executor struct {
	commands map[string]Command
	fallback Command
}

func NewExecutor() *Executor {
	return &Executor{
		commands: make(map[string]Command),
		fallback: NewShellCommand(),
	}
}

func (e *Executor) Register(cmd Command) {
	e.commands[cmd.Name()] = cmd
}

func (e *Executor) Execute(ctx context.Context, payload string) (string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", nil
	}

	parts := strings.SplitN(payload, " ", 2)
	cmdName := parts[0]
	rawArgs := ""
	if len(parts) > 1 {
		rawArgs = strings.TrimSpace(parts[1])
	}

	if cmd, ok := e.commands[cmdName]; ok {
		return cmd.Execute(ctx, rawArgs)
	}

	// Fallback to shell execution for backwards compatibility
	return e.fallback.Execute(ctx, payload)
}
