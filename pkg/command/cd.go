package command

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type CdCommand struct{}

func NewCdCommand() *CdCommand {
	return &CdCommand{}
}

func (c *CdCommand) Name() string {
	return "cd"
}

func (c *CdCommand) Description() string {
	return "Changes the current working directory"
}

func (c *CdCommand) Execute(ctx context.Context, rawArgs string) (string, error) {
	path := strings.TrimSpace(rawArgs)
	if path == "" {
		// Default to home directory or current directory?
		// Usually cd with no args goes to $HOME on Unix.
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = home
	}

	err := os.Chdir(path)
	if err != nil {
		return "", err
	}

	newDir, _ := os.Getwd()
	return fmt.Sprintf("Changed directory to: %s", newDir), nil
}
