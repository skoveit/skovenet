package command

import (
	"context"
	"fmt"
	"os"
	"runtime"
)

type InfoCommand struct{}

func NewInfoCommand() *InfoCommand {
	return &InfoCommand{}
}

func (c *InfoCommand) Name() string {
	return "info"
}

func (c *InfoCommand) Description() string {
	return "Displays system information"
}

func (c *InfoCommand) Execute(ctx context.Context, _ string) (string, error) {
	hostname, _ := os.Hostname()
	wd, _ := os.Getwd()
	pid := os.Getpid()

	// Try to get username
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}

	info := fmt.Sprintf("OS/Arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	info += fmt.Sprintf("Hostname:  %s\n", hostname)
	info += fmt.Sprintf("Username:  %s\n", user)
	info += fmt.Sprintf("PID:       %d\n", pid)
	info += fmt.Sprintf("CWD:       %s\n", wd)

	return info, nil
}
