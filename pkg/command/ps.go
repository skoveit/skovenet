package command

import (
	"context"
	"os/exec"
	"runtime"
)

type PsCommand struct{}

func NewPsCommand() *PsCommand {
	return &PsCommand{}
}

func (c *PsCommand) Name() string {
	return "ps"
}

func (c *PsCommand) Description() string {
	return "Lists running processes"
}

func (c *PsCommand) Execute(ctx context.Context, _ string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "tasklist")
	} else {
		cmd = exec.CommandContext(ctx, "ps", "aux")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}
