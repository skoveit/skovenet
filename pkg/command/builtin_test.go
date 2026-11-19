package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestWorkingDirectoryCommands(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	dir := filepath.Join(root, "directory with spaces")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := NewCdCommand().Execute(context.Background(), "  "+dir+"  ")
	if err != nil {
		t.Fatalf("cd: %v", err)
	}
	reported, ok := strings.CutPrefix(out, "Changed directory to: ")
	if !ok {
		t.Fatalf("cd output = %q", out)
	}
	assertSameDirectory(t, reported, dir)
	got, err := NewPwdCommand().Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("pwd: %v", err)
	}
	assertSameDirectory(t, got, dir)
	beforeFailedChange := got

	if _, err := NewCdCommand().Execute(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Error("cd to a missing directory succeeded")
	}
	got, err = NewPwdCommand().Execute(context.Background(), "")
	if err != nil || got != beforeFailedChange {
		t.Errorf("failed cd changed working directory: pwd = %q, %v", got, err)
	}
}

func assertSameDirectory(t *testing.T, got, want string) {
	t.Helper()
	actual, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat reported directory %q: %v", got, err)
	}
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat expected directory %q: %v", want, err)
	}
	// Windows can report an 8.3 alias while filepath.EvalSymlinks reports the
	// long name; Unix paths can also have multiple names through symlinks.
	if !actual.IsDir() || !os.SameFile(actual, expected) {
		t.Errorf("reported path %q does not identify expected directory %q", got, want)
	}
}

func TestLsCommandListsFilesAndDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "listing with spaces")
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := NewLsCommand().Execute(context.Background(), "  "+dir+"  ")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("ls returned %d lines, want header and two entries: %q", len(lines), out)
	}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "MODE SIZE NAME" {
		t.Errorf("unexpected ls header: %q", lines[0])
	}
	file := strings.Fields(lines[1])
	if len(file) != 3 || file[1] != "5" || file[2] != "hello.txt" {
		t.Errorf("file entry does not report its size and name: %q", lines[1])
	}
	subdir := strings.Fields(lines[2])
	if len(subdir) != 3 || subdir[1] != "-" || subdir[2] != "subdir/" || !strings.HasPrefix(subdir[0], "d") {
		t.Errorf("directory entry does not report a directory: %q", lines[2])
	}

	t.Chdir(dir)
	defaultOutput, err := NewLsCommand().Execute(context.Background(), "")
	if err != nil || defaultOutput != out {
		t.Errorf("ls with no path = %q, %v; want %q", defaultOutput, err, out)
	}
	if _, err := NewLsCommand().Execute(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Error("ls of a missing directory succeeded")
	}
	if _, err := NewLsCommand().Execute(context.Background(), filepath.Join(dir, "hello.txt")); err == nil {
		t.Error("ls of a regular file succeeded")
	}
}

func TestInfoCommandReportsCurrentProcess(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("USER", "skovenet-test-user")
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	out, err := NewInfoCommand().Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	for label, value := range map[string]string{
		"OS/Arch":  runtime.GOOS + "/" + runtime.GOARCH,
		"Hostname": hostname,
		"Username": "skovenet-test-user",
		"PID":      strconv.Itoa(os.Getpid()),
		"CWD":      wd,
	} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			key, got, ok := strings.Cut(line, ":")
			if ok && key == label {
				found = true
				if strings.TrimSpace(got) != value {
					t.Errorf("%s = %q, want %q", label, strings.TrimSpace(got), value)
				}
			}
		}
		if !found {
			t.Errorf("info output is missing %s: %q", label, out)
		}
	}
}

func TestPsCommandListsCurrentProcess(t *testing.T) {
	out, err := NewPsCommand().Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	pid := strconv.Itoa(os.Getpid())
	for _, field := range strings.Fields(out) {
		if field == pid {
			return
		}
	}
	t.Errorf("process list did not contain current PID %s: %q", pid, out)
}

func TestPsCommandHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPsCommand().Execute(ctx, ""); err == nil {
		t.Error("ps with a canceled context succeeded")
	}
}

func TestTransferCommandsRequireArgumentsAndSourcePeer(t *testing.T) {
	for _, cmd := range []Command{NewUploadCommand(nil), NewDownloadCommand(nil)} {
		t.Run(cmd.Name(), func(t *testing.T) {
			for _, args := range []string{"", "one-path"} {
				if _, err := cmd.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "usage:") {
					t.Errorf("args %q: expected usage error, got %v", args, err)
				}
			}
			for _, ctx := range []context.Context{
				context.Background(),
				context.WithValue(context.Background(), "source_peer", ""),
				context.WithValue(context.Background(), "source_peer", 123),
			} {
				if _, err := cmd.Execute(ctx, "source destination"); err == nil || !strings.Contains(err.Error(), "operator peer ID") {
					t.Errorf("expected missing operator peer error, got %v", err)
				}
			}
		})
	}
}
