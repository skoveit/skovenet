package command

import "context"

// Command represents a registered action the agent can perform.
type Command interface {
	// Name returns the command's name (e.g., "download", "shell").
	Name() string
	// Description returns a short description of the command.
	Description() string
	// Execute runs the command with the provided context and raw arguments string.
	// It is up to the command to parse the rawArgs appropriately.
	Execute(ctx context.Context, rawArgs string) (string, error)
}
