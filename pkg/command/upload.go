package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

type UploadCommand struct {
	fileHandler *FileHandler
}

func NewUploadCommand(fh *FileHandler) *UploadCommand {
	return &UploadCommand{fileHandler: fh}
}

func (c *UploadCommand) Name() string {
	return "upload"
}

func (c *UploadCommand) Description() string {
	return "Uploads a file from the local machine to the remote agent"
}

func (c *UploadCommand) Execute(ctx context.Context, rawArgs string) (string, error) {
	// Args should be: <local_path> <remote_path>
	parts := strings.Fields(rawArgs)
	if len(parts) < 2 {
		return "", fmt.Errorf("usage: upload <local_path> <remote_path>")
	}
	localPath := parts[0]
	remotePath := parts[1]

	// Extract the operator's peer ID from context
	operatorID, ok := ctx.Value("source_peer").(string)
	if !ok || operatorID == "" {
		return "", fmt.Errorf("could not determine operator peer ID")
	}

	// This is running on the TARGET agent.
	// We need to tell the operator to SEND (fetch) the file from `localPath` on their machine,
	// and we will save data as it streams in to `remotePath`.

	// Open stream to operator, telling them to FETCH (read and send us) `localPath`
	stream, err := c.fileHandler.OpenFileStream(ctx, operatorID, FileHeader{
		Cmd:  "fetch",
		Path: localPath, // We tell them to read this file
	})
	if err != nil {
		return "", fmt.Errorf("failed to open stream to operator: %w", err)
	}
	// The open file stream sends the header then immediately returns. We can't wait synchronously
	// easily here if the fetch command handles writing locally.
	// Actually, the `/mesh-c2/file/1.0` logic is symmetric!
	// If we send `fetch` to their node, their `HandleFileStream` will immediately
	// open the local file and `io.Copy(s, file)`.
	// Thus, we just need to read from `stream` and write to `remotePath` HERE.

	// Wait for the remote to send 'OK\n' indicating file was opened properly
	// We use bufio to not over-read the file bytes
	reader := bufio.NewReader(stream)
	responseMsg, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read stream response: %w", err)
	}

	responseMsg = strings.TrimSpace(responseMsg)
	if strings.HasPrefix(responseMsg, "ERROR") {
		return "", fmt.Errorf("operator agent failed to open file: %s", responseMsg)
	}

	file, err := os.Create(remotePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file on target: %w", err)
	}
	defer file.Close()

	// the rest of the stream is the file data
	copied, err := io.Copy(file, reader)
	if err != nil {
		return "", fmt.Errorf("failed during file transfer: %w", err)
	}

	return fmt.Sprintf("✅ Upload complete: %d bytes transferred to %s", copied, remotePath), nil
}
