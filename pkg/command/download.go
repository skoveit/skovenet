package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

type DownloadCommand struct {
	fileHandler *FileHandler
}

func NewDownloadCommand(fh *FileHandler) *DownloadCommand {
	return &DownloadCommand{fileHandler: fh}
}

func (c *DownloadCommand) Name() string {
	return "download"
}

func (c *DownloadCommand) Description() string {
	return "Downloads a file from the remote agent to the local machine"
}

func (c *DownloadCommand) Execute(ctx context.Context, rawArgs string) (string, error) {
	// Args should be: <remote_path> <local_path>
	parts := strings.Fields(rawArgs)
	if len(parts) < 2 {
		return "", fmt.Errorf("usage: download <remote_path> <local_path>")
	}
	remotePath := parts[0]
	localPath := parts[1]

	// Extract the operator's peer ID from context (who requested this)
	operatorID, ok := ctx.Value("source_peer").(string)
	if !ok || operatorID == "" {
		return "", fmt.Errorf("could not determine operator peer ID")
	}

	// This is running on the TARGET agent.
	// We need to fetch the file from `remotePath` locally on this machine,
	// and stream it to the operator's agent so they can save it to `localPath`.

	file, err := os.Open(remotePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file on target: %w", err)
	}
	defer file.Close()

	// Open stream to operator, telling them to SAVE the data to `localPath`
	stream, err := c.fileHandler.OpenFileStream(ctx, operatorID, FileHeader{
		Cmd:  "save",
		Path: localPath,
	})
	if err != nil {
		return "", fmt.Errorf("failed to open stream to operator: %w", err)
	}
	defer stream.Close()

	// Stream the file contents over the network directly.
	copied, err := io.Copy(stream, file)
	if err != nil {
		return "", fmt.Errorf("failed during file transfer: %w", err)
	}

	return fmt.Sprintf("Download complete: %d bytes transferred to %s", copied, localPath), nil
}
