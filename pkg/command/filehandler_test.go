package command

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/libp2p/go-libp2p/core/network"
)

type fileTestStream struct {
	network.Stream
	reader  io.Reader
	written bytes.Buffer
	closed  bool
}

func (s *fileTestStream) Read(p []byte) (int, error)  { return s.reader.Read(p) }
func (s *fileTestStream) Write(p []byte) (int, error) { return s.written.Write(p) }
func (s *fileTestStream) Close() error {
	s.closed = true
	return nil
}

func fileStreamInput(t *testing.T, header FileHeader, payload []byte) []byte {
	t.Helper()
	var wire bytes.Buffer
	if err := json.NewEncoder(&wire).Encode(header); err != nil {
		t.Fatal(err)
	}
	wire.Write(payload)
	return wire.Bytes()
}

func TestFileHandlerSavePreservesPayload(t *testing.T) {
	for name, payload := range map[string][]byte{
		"empty":           {},
		"text":            []byte("hello, mesh"),
		"binary":          {0, 0xff, '\n', '\r', '{', '}', 0x80},
		"leading newline": []byte("\n\nkeep both newlines\n"),
		"large":           bytes.Repeat([]byte{0, 0xff, '\n', 'x'}, 32*1024),
	} {
		t.Run(name, func(t *testing.T) {
			for _, fragmented := range []bool{false, true} {
				label := "coalesced"
				if fragmented {
					label = "fragmented"
				}
				t.Run(label, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "nested", "received.bin")
					wire := fileStreamInput(t, FileHeader{Cmd: "save", Path: path}, payload)
					var input io.Reader = bytes.NewReader(wire)
					if fragmented {
						input = iotest.OneByteReader(input)
					}
					stream := &fileTestStream{reader: input}
					(&FileHandler{}).HandleFileStream(stream)
					got, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, payload) {
						t.Errorf("saved %d bytes, want byte-for-byte payload of %d bytes", len(got), len(payload))
					}
					if !stream.closed {
						t.Error("file stream was not closed")
					}
				})
			}
		})
	}
}

func TestFileHandlerFetchPreservesPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.bin")
	payload := bytes.Repeat([]byte{0, '\n', 0xff, 'x'}, 16*1024)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	stream := &fileTestStream{reader: bytes.NewReader(fileStreamInput(t, FileHeader{Cmd: "fetch", Path: path}, nil))}
	(&FileHandler{}).HandleFileStream(stream)
	want := append([]byte("OK\n"), payload...)
	if !bytes.Equal(stream.written.Bytes(), want) {
		t.Errorf("fetch returned %d bytes, want OK framing plus %d payload bytes", stream.written.Len(), len(payload))
	}
	if !stream.closed {
		t.Error("file stream was not closed")
	}
}

func TestFileHandlerFetchMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	stream := &fileTestStream{reader: bytes.NewReader(fileStreamInput(t, FileHeader{Cmd: "fetch", Path: path}, nil))}
	(&FileHandler{}).HandleFileStream(stream)
	if !strings.HasPrefix(stream.written.String(), "ERROR:") || !strings.HasSuffix(stream.written.String(), "\n") {
		t.Errorf("missing file did not produce an error line: %q", stream.written.String())
	}
	if !stream.closed {
		t.Error("file stream was not closed")
	}
}

func TestFileHandlerRejectsInvalidHeaders(t *testing.T) {
	for _, name := range []string{"malformed JSON", "missing delimiter", "unknown command"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "must-not-exist")
			var wire []byte
			switch name {
			case "malformed JSON":
				wire = []byte("{bad header}\nignored payload")
			case "missing delimiter":
				var err error
				wire, err = json.Marshal(FileHeader{Cmd: "save", Path: path})
				if err != nil {
					t.Fatal(err)
				}
			case "unknown command":
				wire = fileStreamInput(t, FileHeader{Cmd: "unknown", Path: path}, []byte("ignored payload"))
			}
			stream := &fileTestStream{reader: bytes.NewReader(wire)}
			(&FileHandler{}).HandleFileStream(stream)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("invalid header created a file: %v", err)
			}
			if !stream.closed {
				t.Error("invalid file stream was not closed")
			}
		})
	}
}
