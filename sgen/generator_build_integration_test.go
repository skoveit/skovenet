//go:build generator_integration

// Generated-agent build smoke test. Run through scripts/verify-generator.sh,
// which regenerates the embedded source archive first.
//
// What this proves for each selected target: the embedded toolchain extracts
// into a test directory, Build compiles the embedded agent source for that
// GOOS/GOARCH, the output has the expected executable format and CPU
// architecture (debug/elf, debug/macho, debug/pe), and it contains the
// injected public key. Only the native target is executed, and only with -h,
// which exits during flag parsing before any node or network setup. Format
// checks of cross-compiled binaries are not runtime proof.
//
// SGEN_SMOKE_TARGETS selects targets: "native" (default), "all", or a
// comma-separated list such as "linux/amd64,windows/amd64".
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func smokeTargets(t *testing.T) []string {
	t.Helper()
	native := runtime.GOOS + "/" + runtime.GOARCH
	spec := strings.TrimSpace(os.Getenv("SGEN_SMOKE_TARGETS"))
	switch spec {
	case "", "native":
		return []string{native}
	case "all":
		var all []string
		for _, p := range supportedPlatforms {
			all = append(all, p.OS+"/"+p.Arch)
		}
		return all
	}
	var targets []string
	for _, target := range strings.Split(spec, ",") {
		if target = strings.TrimSpace(target); target != "" {
			targets = append(targets, target)
		}
	}
	return targets
}

// TestEmbeddedSourceMatchesTree guards against smoke-testing a stale
// source archive: go.mod, go.sum, agent/ and pkg/ must match the checkout.
func TestEmbeddedSourceMatchesTree(t *testing.T) {
	gr, err := gzip.NewReader(bytes.NewReader(sourceArchive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	archived := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if hdr.Typeflag != tar.TypeReg || strings.HasPrefix(name, "vendor/") {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		archived[name] = data
	}

	onDisk := map[string]bool{"go.mod": true, "go.sum": true}
	for _, dir := range []string{"agent", "pkg"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel("..", path)
			if err != nil {
				return err
			}
			onDisk[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for name := range onDisk {
		want, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := archived[name]; !ok || !bytes.Equal(got, want) {
			t.Errorf("embedded source is stale: %s differs from the checkout (run scripts/verify-generator.sh)", name)
		}
	}
	for name := range archived {
		if !onDisk[name] {
			t.Errorf("embedded source is stale: %s is not in the checkout", name)
		}
	}
}

func TestGeneratedAgentBuild(t *testing.T) {
	work := t.TempDir()
	tcDir := filepath.Join(work, "toolchain")
	tmp := filepath.Join(work, "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	// Keep the compiler cache and Build's workspace under the test directory.
	t.Setenv("GOCACHE", filepath.Join(work, "gocache"))
	t.Setenv("TMPDIR", tmp)
	t.Setenv("GOENV", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "off")

	seed := bytes.Repeat([]byte{0x5a}, ed25519.SeedSize)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	native := runtime.GOOS + "/" + runtime.GOARCH

	for _, target := range smokeTargets(t) {
		t.Run(strings.ReplaceAll(target, "/", "_"), func(t *testing.T) {
			goos, goarch, ok := strings.Cut(target, "/")
			if !ok {
				t.Fatalf("target %q is not os/arch", target)
			}
			out := filepath.Join(work, "out", goos+"-"+goarch, "agent")
			if goos == "windows" {
				out += ".exe"
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			err := Build(&BuildConfig{OS: goos, Arch: goarch, PublicKey: pubB64, OutputPath: out, toolchainDir: tcDir})
			if err != nil {
				t.Fatalf("Build(%s): %v", target, err)
			}
			t.Logf("built %s in %s", target, time.Since(start).Round(time.Millisecond))

			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(pubB64)) {
				t.Errorf("generated binary does not contain the injected public key")
			}
			checkBinaryFormat(t, out, goos, goarch)

			if target != native {
				t.Logf("%s: format checked only; not executed on %s", target, native)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, out, "-h")
			cmd.Dir = t.TempDir()
			cmd.Env = []string{"HOME=" + cmd.Dir, "TMPDIR=" + cmd.Dir}
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("generated agent -h did not exit: %v", ctx.Err())
			}
			if err != nil {
				t.Fatalf("generated agent -h failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "-debug") {
				t.Errorf("generated agent -h did not print its flags:\n%s", output)
			}
		})
	}
}

func checkBinaryFormat(t *testing.T, path, goos, goarch string) {
	t.Helper()
	switch goos {
	case "linux":
		f, err := elf.Open(path)
		if err != nil {
			t.Fatalf("not an ELF binary: %v", err)
		}
		defer f.Close()
		want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64, "386": elf.EM_386, "arm": elf.EM_ARM}[goarch]
		if f.Machine != want || f.Type != elf.ET_EXEC {
			t.Errorf("ELF machine/type = %v/%v, want %v/%v", f.Machine, f.Type, want, elf.ET_EXEC)
		}
	case "darwin":
		f, err := macho.Open(path)
		if err != nil {
			t.Fatalf("not a Mach-O binary: %v", err)
		}
		defer f.Close()
		want := map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64}[goarch]
		if f.Cpu != want || f.Type != macho.TypeExec {
			t.Errorf("Mach-O cpu/type = %v/%v, want %v/%v", f.Cpu, f.Type, want, macho.TypeExec)
		}
	case "windows":
		f, err := pe.Open(path)
		if err != nil {
			t.Fatalf("not a PE binary: %v", err)
		}
		defer f.Close()
		want := map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64}[goarch]
		if f.Machine != want || f.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 {
			t.Errorf("PE machine = %#x (characteristics %#x), want %#x executable", f.Machine, f.Characteristics, want)
		}
	default:
		t.Fatal(errors.New("no format check for " + goos))
	}
}
