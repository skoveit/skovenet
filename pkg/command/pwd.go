package command

import (
	"context"
	"os"
)

type PwdCommand struct{}

func NewPwdCommand() *PwdCommand {
	return &PwdCommand{}
}

func (c *PwdCommand) Name() string {
	return "pwd"
}

func (c *PwdCommand) Description() string {
	return "Prints the current working directory"
}

func (c *PwdCommand) Execute(ctx context.Context, _ string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return dir, nil
}
