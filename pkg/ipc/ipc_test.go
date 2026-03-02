package ipc

import (
	"testing"
)

// ---------------------------------------------------------------------------
// ParseInput
// ---------------------------------------------------------------------------

func TestParseInput_SimpleCommand(t *testing.T) {
	cmd, args := ParseInput("peers")
	if cmd != "peers" {
		t.Errorf("cmd = %q, want %q", cmd, "peers")
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestParseInput_CommandWithArgs(t *testing.T) {
	cmd, args := ParseInput("send peer123 whoami")
	if cmd != "send" {
		t.Errorf("cmd = %q, want %q", cmd, "send")
	}
	if len(args) != 2 || args[0] != "peer123" || args[1] != "whoami" {
		t.Errorf("args = %v, want [peer123 whoami]", args)
	}
}

func TestParseInput_EmptyString(t *testing.T) {
	cmd, args := ParseInput("")
	if cmd != "" {
		t.Errorf("cmd = %q, want empty", cmd)
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestParseInput_WhitespaceOnly(t *testing.T) {
	cmd, args := ParseInput("   ")
	if cmd != "" {
		t.Errorf("cmd = %q, want empty", cmd)
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestParseInput_ExtraSpaces(t *testing.T) {
	cmd, args := ParseInput("  sign    key123   ")
	if cmd != "sign" {
		t.Errorf("cmd = %q, want %q", cmd, "sign")
	}
	if len(args) != 1 || args[0] != "key123" {
		t.Errorf("args = %v, want [key123]", args)
	}
}
