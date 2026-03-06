package command

import (
	"context"
	"strings"
	"testing"
)

type mockCommand struct {
	called   bool
	lastArgs string
	name     string
	resp     string
	err      error
}

func (m *mockCommand) Name() string {
	return m.name
}

func (m *mockCommand) Description() string {
	return "Mock command"
}

func (m *mockCommand) Execute(ctx context.Context, rawArgs string) (string, error) {
	m.called = true
	m.lastArgs = rawArgs
	return m.resp, m.err
}

func TestExecutor_Dispatch(t *testing.T) {
	e := NewExecutor()

	mockDownload := &mockCommand{name: "download", resp: "downloaded"}
	mockUpload := &mockCommand{name: "upload", resp: "uploaded"}

	e.Register(mockDownload)
	e.Register(mockUpload)

	// Test download command dispatch
	output, err := e.Execute(context.Background(), "download file.txt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockDownload.called || mockDownload.lastArgs != "file.txt" {
		t.Errorf("download command not called correctly")
	}
	if output != "downloaded" {
		t.Errorf("wrong output: %s", output)
	}

	// Test upload command dispatch with multiple args
	output, err = e.Execute(context.Background(), "upload src.txt dst.txt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockUpload.called || mockUpload.lastArgs != "src.txt dst.txt" {
		t.Errorf("upload command not called correctly")
	}

	// Test fallback (unregistered command)
	// reset mock states
	mockDownload.called = false
	mockUpload.called = false

	output, err = e.Execute(context.Background(), "echo test_fallback")
	if err != nil {
		t.Errorf("unexpected error from fallback: %v", err)
	}
	if mockDownload.called || mockUpload.called {
		t.Errorf("registered commands should not have been called")
	}
	if !strings.Contains(output, "test_fallback") {
		t.Errorf("expected fallback output to contain 'test_fallback', got %q", output)
	}
}

func TestExecutor_NoArgs(t *testing.T) {
	e := NewExecutor()
	mockCmd := &mockCommand{name: "status", resp: "ok"}
	e.Register(mockCmd)

	output, err := e.Execute(context.Background(), "status")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockCmd.called || mockCmd.lastArgs != "" {
		t.Errorf("status command not called correctly, args: %q", mockCmd.lastArgs)
	}
	if output != "ok" {
		t.Errorf("wrong output: %s", output)
	}
}
