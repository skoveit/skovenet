package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// supportedPlatforms lists all OS/arch combinations the agent can be built for.
var supportedPlatforms = []struct {
	OS   string
	Arch string
}{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"linux", "386"},
	{"linux", "arm"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"windows", "amd64"},
}

// BuildConfig holds everything needed to generate an agent binary.
type BuildConfig struct {
	OS         string // Target GOOS
	Arch       string // Target GOARCH
	PublicKey  string // Base64-encoded Ed25519 public key
	OutputPath string // Where to write the output binary
}

// ValidateConfig checks that the build configuration is valid.
func ValidateConfig(cfg *BuildConfig) error {
	valid := false
	for _, p := range supportedPlatforms {
		if p.OS == cfg.OS && p.Arch == cfg.Arch {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unsupported platform: %s/%s (run 'sgen list' to see options)", cfg.OS, cfg.Arch)
	}

	decoded, err := base64.StdEncoding.DecodeString(cfg.PublicKey)
	if err != nil {
		return fmt.Errorf("invalid public key (must be base64): %w", err)
	}
	if len(decoded) != 32 {
		return fmt.Errorf("invalid public key size: got %d bytes, want 32 (Ed25519)", len(decoded))
	}

	return nil
}

// Build generates an agent binary for the given configuration.
func Build(cfg *BuildConfig) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	// Step 1: Ensure Go toolchain is extracted.
	fmt.Println("[*] Checking toolchain...")
	if err := ensureToolchain(); err != nil {
		return err
	}

	gobin, err := goBin()
	if err != nil {
		return err
	}
	root, err := goRoot()
	if err != nil {
		return err
	}

	// Step 2: Extract source to temp workspace.
	fmt.Println("[*] Preparing workspace...")
	tmpDir, err := os.MkdirTemp("", "sgen-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarGz(sourceArchive, tmpDir); err != nil {
		return fmt.Errorf("source extraction failed: %w", err)
	}

	// Resolve output path to absolute (go build runs from tmpDir).
	outPath, err := filepath.Abs(cfg.OutputPath)
	if err != nil {
		return fmt.Errorf("invalid output path: %w", err)
	}

	// Step 3: Build the agent with animation.
	ldflags := fmt.Sprintf("-s -w -X 'github.com/skoveit/skovenet/pkg/signing.publicKeyB64=%s'", cfg.PublicKey)

	cmd := exec.Command(gobin, "build",
		"-ldflags", ldflags,
		"-mod=vendor",
		"-trimpath",
		"-o", outPath,
		"./agent",
	)
	cmd.Dir = tmpDir
	cmd.Env = buildEnv(root, cfg.OS, cfg.Arch, tmpDir)

	// Start the build in background and animate while waiting.
	start := time.Now()
	var buildErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buildErr = cmd.Run()
	}()

	// Animate while building.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	animateCompile(cfg.OS, cfg.Arch, done)

	elapsed := time.Since(start)

	if buildErr != nil {
		return fmt.Errorf("compilation failed: %w", buildErr)
	}

	// Print result.
	info, _ := os.Stat(outPath)
	sizeMB := float64(info.Size()) / (1024 * 1024)

	keyShort := cfg.PublicKey
	if len(keyShort) > 28 {
		keyShort = keyShort[:12] + "..." + keyShort[len(keyShort)-8:]
	}

	fmt.Println()
	fmt.Printf("[✓] Agent ready  %s/%s  %.1f MB  %.1fs\n", cfg.OS, cfg.Arch, sizeMB, elapsed.Seconds())
	fmt.Printf("    Output: %s\n", cfg.OutputPath)
	fmt.Printf("    Key:    %s\n", keyShort)
	fmt.Println()

	return nil
}

// animateCompile shows an ASCII animation while the agent is compiling.
func animateCompile(goos, goarch string, done <-chan struct{}) {
	// Tree/forest-themed frames for "skovenet" (skov = forest in Danish)
	frames := []string{
		"  🌲        compiling %s/%s",
		"  🌲🌿      compiling %s/%s",
		"  🌲🌿🌱    compiling %s/%s",
		"  🌿🌱🌲    compiling %s/%s",
		"  🌱🌲🌿    compiling %s/%s",
		"  🌲🌲🌿    compiling %s/%s",
		"  🌲🌿🌲    compiling %s/%s",
		"  🌿🌲🌲    compiling %s/%s",
	}

	i := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			// Clear the animation line.
			fmt.Print("\r\033[K")
			return
		case <-ticker.C:
			frame := fmt.Sprintf(frames[i%len(frames)], goos, goarch)
			fmt.Printf("\r\033[K%s", frame)
			i++
		}
	}
}

// buildEnv constructs the environment variables for go build.
func buildEnv(goroot, goos, goarch, workdir string) []string {
	env := os.Environ()

	filtered := make([]string, 0, len(env))
	skip := map[string]bool{
		"GOROOT": true, "GOOS": true, "GOARCH": true,
		"CGO_ENABLED": true, "GOPATH": true, "GOFLAGS": true,
	}
	for _, e := range env {
		key := e
		for i, c := range e {
			if c == '=' {
				key = e[:i]
				break
			}
		}
		if !skip[key] {
			filtered = append(filtered, e)
		}
	}

	return append(filtered,
		"GOROOT="+goroot,
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
		"GOPATH="+filepath.Join(workdir, ".gopath"),
		"GOFLAGS=",
	)
}
