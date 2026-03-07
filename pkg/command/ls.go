package command

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

type LsCommand struct{}

func NewLsCommand() *LsCommand {
	return &LsCommand{}
}

func (c *LsCommand) Name() string {
	return "ls"
}

func (c *LsCommand) Description() string {
	return "Lists directory contents"
}

func (c *LsCommand) Execute(ctx context.Context, rawArgs string) (string, error) {
	path := strings.TrimSpace(rawArgs)
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "MODE\tSIZE\tNAME")

	for _, entry := range entries {
		info, err := entry.Info()
		size := "-"
		mode := entry.Type().String()
		if err == nil {
			if !info.IsDir() {
				size = fmt.Sprintf("%d", info.Size())
			}
			mode = info.Mode().String()
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", mode, size, name)
	}
	w.Flush()

	return b.String(), nil
}
