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

	// Use a simple split but handle potential quotes if we want to be fully robust.
	// However, the payload from the protocol should ideally be already cleaned up.
	// Let's do a basic check for quotes at the start/end of the first word.

	payload = strings.TrimSpace(payload)
	var cmdName string
	var rawArgs string

	if len(payload) > 0 && (payload[0] == '"' || payload[0] == '\'') {
		// If the entire payload starts with a quote, find the matching end quote
		quote := payload[0]
		end := strings.IndexRune(payload[1:], rune(quote))
		if end != -1 {
			cmdName = payload[1 : end+1]
			rawArgs = strings.TrimSpace(payload[end+2:])
		} else {
			// Malformed, but let's try our best
			parts := strings.SplitN(payload, " ", 2)
			cmdName = parts[0]
			if len(parts) > 1 {
				rawArgs = parts[1]
			}
		}
	} else {
		parts := strings.SplitN(payload, " ", 2)
		cmdName = parts[0]
		if len(parts) > 1 {
			rawArgs = parts[1]
		}
	}

	if cmd, ok := e.commands[cmdName]; ok {
		return cmd.Execute(ctx, rawArgs)
	}

	// Fallback to shell execution for backwards compatibility
	return e.fallback.Execute(ctx, payload)
}
