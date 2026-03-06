//go:build !windows

package command

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Basic execution
// ---------------------------------------------------------------------------

func TestExecute_SimpleCommand(t *testing.T) {
	e := NewExecutor()
	output, err := e.Execute(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.TrimSpace(output) != "hello" {
		t.Errorf("output = %q, want %q", output, "hello")
	}
}

func TestExecute_EmptyCommand(t *testing.T) {
	e := NewExecutor()
	output, err := e.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if output != "" {
		t.Errorf("empty command should return empty output, got %q", output)
	}
}

func TestExecute_WhitespaceCommand(t *testing.T) {
	e := NewExecutor()
	output, err := e.Execute(context.Background(), "   ")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if output != "" {
		t.Errorf("whitespace command should return empty output, got %q", output)
	}
}

func TestExecute_CapturesStderr(t *testing.T) {
	e := NewExecutor()
	output, _ := e.Execute(context.Background(), "echo error >&2")
	if !strings.Contains(output, "error") {
		t.Errorf("should capture stderr, got %q", output)
	}
}

func TestExecute_FailedCommand(t *testing.T) {
	e := NewExecutor()
	output, err := e.Execute(context.Background(), "false")
	if err == nil {
		t.Error("'false' command should return error")
	}
	_ = output // output may be empty for 'false'
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

func TestExecute_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	e := NewExecutor()
	start := time.Now()
	output, err := e.Execute(context.Background(), "sleep 120") // will be killed by 30s timeout
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error for timed-out command")
	}

	// Should finish well before the 120s sleep
	if elapsed > 35*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}

	if !strings.Contains(output, "timeout") {
		t.Errorf("output should mention timeout, got %q", output)
	}
}

// ---------------------------------------------------------------------------
// Output size limit
// ---------------------------------------------------------------------------

func TestExecute_OutputLimit(t *testing.T) {
	e := NewExecutor()
	// Generate 2MB of output (well above the 1MB limit)
	output, _ := e.Execute(context.Background(), "dd if=/dev/zero bs=1024 count=2048 2>/dev/null | tr '\\0' 'A'")

	if len(output) > MaxOutputSize+200 { // +200 for the truncation message
		t.Errorf("output size = %d, should be capped near %d", len(output), MaxOutputSize)
	}
}

// ---------------------------------------------------------------------------
// Pipes and shell features
// ---------------------------------------------------------------------------

func TestExecute_Pipes(t *testing.T) {
	e := NewExecutor()
	output, err := e.Execute(context.Background(), "echo 'hello world' | wc -w")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.TrimSpace(output) != "2" {
		t.Errorf("pipe output = %q, want %q", strings.TrimSpace(output), "2")
	}
}

func TestExecute_VariableExpansion(t *testing.T) {
	e := NewExecutor()
	output, err := e.Execute(context.Background(), "echo $HOME")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if output == "" || output == "$HOME" {
		t.Errorf("variable expansion failed, got %q", output)
	}
}
