package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveKeyPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator key")
	if err := os.WriteFile(path, []byte("  file-fixture-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKOVENET_KEY", "environment-fixture-key")
	for _, tt := range []struct {
		name, flag, file, want string
	}{
		{"flag before file and environment", "flag-fixture-key", path, "flag-fixture-key"},
		{"flag does not open ignored file", "flag-fixture-key", path + ".missing", "flag-fixture-key"},
		{"file before environment", "", path, "file-fixture-key"},
		{"environment fallback", "", "", "environment-fixture-key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveKey(tt.flag, tt.file); got != tt.want {
				t.Errorf("resolveKey = %q, want %q", got, tt.want)
			}
		})
	}
	t.Setenv("SKOVENET_KEY", "")
	if got := resolveKey("", ""); got != "" {
		t.Errorf("no configured key returned %q", got)
	}
}

func TestResolveMissingKeyFileExits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-key")
	cmd := exec.Command(os.Args[0], "-test.run=^TestResolveMissingKeyFileHelper$")
	cmd.Env = append(os.Environ(), "SKOVENET_VERIFY_MISSING_KEY_FILE="+path)
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("missing key file exit = %v, output %q", err, output)
	}
	if !strings.Contains(string(output), "Failed to read key file") || !strings.Contains(string(output), path) {
		t.Errorf("missing key file diagnostic = %q", output)
	}
}

func TestResolveMissingKeyFileHelper(t *testing.T) {
	if path := os.Getenv("SKOVENET_VERIFY_MISSING_KEY_FILE"); path != "" {
		resolveKey("", path)
		t.Fatal("missing key file unexpectedly returned")
	}
}

func setControllerTestState(t *testing.T, selected string, peers []string) {
	t.Helper()
	mu.Lock()
	oldSelected, oldPeers, oldCount := selectedPeer, peerList, peerCount
	selectedPeer, peerList, peerCount = selected, peers, len(peers)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		selectedPeer, peerList, peerCount = oldSelected, oldPeers, oldCount
		mu.Unlock()
	})
}

func TestCompleteCommandsAndPeerIDs(t *testing.T) {
	for _, tt := range []struct {
		name, selected, input string
		want                  []string
	}{
		{"global command", "", "pe", []string{"peers"}},
		{"peer commands", "peer-a", "p", []string{"pwd", "ps"}},
		{"peer prefix", "", "use peer-a", []string{"use peer-alpha"}},
		{"space after use", "", "use ", []string{"use peer-alpha", "use peer-beta"}},
		{"tab after use", "", "use\t", []string{"use peer-alpha", "use peer-beta"}},
		{"leading whitespace", "", "  use peer-b", []string{"use peer-beta"}},
		{"unknown prefix", "", "missing-command", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setControllerTestState(t, tt.selected, []string{"peer-alpha", "peer-beta"})
			if got := complete(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("complete(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPromptReflectsSelectionAndPeerCount(t *testing.T) {
	setControllerTestState(t, "", []string{"peer-a", "peer-b"})
	if got := getPrompt(); got != "[2 peers]> " {
		t.Errorf("global prompt = %q", got)
	}
	mu.Lock()
	selectedPeer = "1234567890abcdefghijklmnop"
	mu.Unlock()
	if got := getPrompt(); got != "[1234567890abcdef]> " {
		t.Errorf("selected peer prompt = %q", got)
	}
}
