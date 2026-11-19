package main

import (
	"archive/tar"
	"archive/zip"
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

// goRootIn returns the GOROOT of a toolchain extracted into tcDir.
func goRootIn(tcDir string) string {
	return filepath.Join(tcDir, "go")
}

// goBinIn returns the go binary of a toolchain extracted into tcDir.
func goBinIn(tcDir string) string {
	bin := "go"
	if runtime.GOOS == "windows" {
		bin = "go.exe"
	}
	return filepath.Join(goRootIn(tcDir), "bin", bin)
}

// ensureToolchainIn extracts the embedded Go toolchain into tcDir if it is
// not already present. The CLI uses ~/.skovenet/toolchain (toolchainDir).
func ensureToolchainIn(tcDir string) error {
	bin := goBinIn(tcDir)
	if _, err := os.Stat(bin); err == nil {
		return nil // already extracted
	}

	fmt.Println("[*] Extracting Go toolchain (first run, this may take a moment)...")

	// Determine which toolchain archive was embedded.
	var archive []byte
	var isZip bool
	var err error
	archive, err = toolchainFS.ReadFile("assets/toolchain.zip")
	if err == nil {
		isZip = true
	} else {
		archive, err = toolchainFS.ReadFile("assets/toolchain.tar.gz")
		if err != nil {
			return fmt.Errorf("embedded toolchain archive not found (.zip or .tar.gz)")
		}
	}

	if isZip {
		if err := extractZip(archive, tcDir); err != nil {
			return fmt.Errorf("toolchain zip extraction failed: %w", err)
		}
	} else {
		if err := extractTarGz(archive, tcDir); err != nil {
			return fmt.Errorf("toolchain tar.gz extraction failed: %w", err)
		}
	}

	// Verify the binary exists after extraction.
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("toolchain extracted but go binary not found at %s", bin)
	}

	fmt.Println("[✓] Toolchain ready")
	return nil
}

// extractTarGz extracts a gzip-compressed tar archive into destDir.
// All filesystem operations go through an os.Root, so entries cannot write
// outside destDir even through pre-existing or archive-created symlinks.
// Entries with non-local names and symlinks that could escape are skipped.
func extractTarGz(archive []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()

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

		cleanName := filepath.Clean(filepath.FromSlash(hdr.Name))
		if !filepath.IsLocal(cleanName) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(cleanName, os.FileMode(hdr.Mode).Perm()|0o755); err != nil {
				return err
			}

		case tar.TypeReg:
			if err := root.MkdirAll(filepath.Dir(cleanName), 0o755); err != nil {
				return err
			}
			f, err := root.OpenFile(cleanName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode).Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()

		case tar.TypeSymlink:
			linkName, ok := downwardSymlinkTarget(hdr.Linkname)
			if !ok {
				continue
			}
			if err := root.MkdirAll(filepath.Dir(cleanName), 0o755); err != nil {
				return err
			}
			root.Remove(cleanName) // remove the link itself if it exists
			if err := root.Symlink(linkName, cleanName); err != nil {
				return err
			}
		}
	}
	return nil
}

// downwardSymlinkTarget reports whether an archive symlink target can only
// resolve beneath the link's own directory. Lexically checking targets that
// contain ".." is not enough: links such as "a -> ." and "c -> a/../x" each
// look contained but together resolve outside the destination. Relative
// targets without ".." elements stay contained no matter how other links in
// the archive are arranged. The embedded archives contain no symlinks today.
func downwardSymlinkTarget(linkname string) (string, bool) {
	target := filepath.FromSlash(linkname)
	if !filepath.IsLocal(target) {
		return "", false
	}
	for _, elem := range strings.Split(filepath.ToSlash(target), "/") {
		if elem == ".." {
			return "", false
		}
	}
	return target, true
}

// extractZip extracts a zip archive into destDir.
// All filesystem operations go through an os.Root, so entries cannot write
// outside destDir even through pre-existing symlinks. Entries with non-local
// names are skipped.
func extractZip(archive []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer root.Close()

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("invalid zip archive: %w", err)
	}

	for _, f := range zr.File {
		cleanName := filepath.Clean(filepath.FromSlash(f.Name))
		if !filepath.IsLocal(cleanName) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := root.MkdirAll(cleanName, f.Mode().Perm()|0o755); err != nil {
				return err
			}
			continue
		}

		if err := root.MkdirAll(filepath.Dir(cleanName), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		dst, err := root.OpenFile(cleanName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
		if err != nil {
			rc.Close()
			return err
		}

		_, copyErr := io.Copy(dst, rc)
		dst.Close()
		rc.Close()

		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
