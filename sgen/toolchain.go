package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const skovenetDirName = ".skovenet"

// skovenetHome returns the path to ~/.skovenet/
func skovenetHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, skovenetDirName), nil
}

// toolchainDir returns the path to ~/.skovenet/toolchain/
func toolchainDir() (string, error) {
	home, err := skovenetHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "toolchain"), nil
}

// goRoot returns the path to ~/.skovenet/toolchain/go/
func goRoot() (string, error) {
	tcDir, err := toolchainDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(tcDir, "go"), nil
}

// goBin returns the path to the go binary inside the extracted toolchain.
func goBin() (string, error) {
	root, err := goRoot()
	if err != nil {
		return "", err
	}
	bin := "go"
	if runtime.GOOS == "windows" {
		bin = "go.exe"
	}
	return filepath.Join(root, "bin", bin), nil
}

// ensureToolchain extracts the embedded Go toolchain if not already present.
func ensureToolchain() error {
	bin, err := goBin()
	if err != nil {
		return err
	}
	if _, err := os.Stat(bin); err == nil {
		return nil // already extracted
	}

	tcDir, err := toolchainDir()
	if err != nil {
		return err
	}

	fmt.Println("[*] Extracting Go toolchain (first run, this may take a moment)...")
	if err := extractTarGz(toolchainArchive, tcDir); err != nil {
		return fmt.Errorf("toolchain extraction failed: %w", err)
	}

	// Verify the binary exists after extraction.
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("toolchain extracted but go binary not found at %s", bin)
	}

	fmt.Println("[✓] Toolchain ready")
	return nil
}

// extractTarGz extracts a gzip-compressed tar archive into destDir.
// Sanitizes paths to prevent directory traversal.
func extractTarGz(archive []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("invalid gzip archive: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		// Sanitize: prevent directory traversal.
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") {
			continue
		}
		target := filepath.Join(destDir, cleanName)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)|0o755); err != nil {
				return err
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()

		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target) // remove if exists
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}
