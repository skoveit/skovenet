package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func containmentArchive(t *testing.T, format, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if format == "zip" {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractionDoesNotFollowExistingOutsideSymlinks(t *testing.T) {
	for _, format := range []struct {
		name    string
		extract func([]byte, string) error
	}{
		{"tar.gz", extractTarGz},
		{"zip", extractZip},
	} {
		for _, shape := range []string{"parent directory", "destination file"} {
			t.Run(format.name+"/"+shape, func(t *testing.T) {
				root := t.TempDir()
				dest := filepath.Join(root, "destination")
				outside := filepath.Join(root, "outside")
				for _, dir := range []string{dest, outside} {
					if err := os.Mkdir(dir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				guard := filepath.Join(outside, "guard")
				if err := os.WriteFile(guard, []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
				name, target := "link/guard", outside
				if shape == "destination file" {
					name, target = "link", guard
				}
				if err := os.Symlink(target, filepath.Join(dest, "link")); err != nil {
					t.Skipf("symbolic links are unavailable: %v", err)
				}
				err := format.extract(containmentArchive(t, format.name, name, []byte("modified")), dest)
				if err == nil {
					t.Error("extraction accepted a destination outside its root")
				}
				got, err := os.ReadFile(guard)
				if err != nil || string(got) != "original" {
					t.Errorf("outside file was changed: %q, %v", got, err)
				}
			})
		}
	}
}

func tarWithLink(t *testing.T, linkTarget string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: linkTarget}); err != nil {
		t.Fatal(err)
	}
	data := []byte("modified")
	if err := tw.WriteHeader(&tar.Header{Name: "link/guard", Mode: 0o600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTarExtractionSkipsOutsideSymlinks(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		name := "relative"
		if absolute {
			name = "absolute"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "destination")
			outside := filepath.Join(root, "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			guard := filepath.Join(outside, "guard")
			if err := os.WriteFile(guard, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Probe link support independently of the extraction behavior.
			probe := filepath.Join(root, "probe")
			if err := os.Symlink(outside, probe); err != nil {
				t.Skipf("symbolic links are unavailable: %v", err)
			}
			target := "../outside"
			if absolute {
				target = outside
			}
			if err := extractTarGz(tarWithLink(t, target), dest); err != nil {
				t.Fatalf("extract: %v", err)
			}
			got, err := os.ReadFile(guard)
			if err != nil || string(got) != "original" {
				t.Errorf("archive symlink escaped its root: %q, %v", got, err)
			}
			info, err := os.Lstat(filepath.Join(dest, "link"))
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				t.Errorf("outside symlink was retained: %v, %v", info, err)
			}
		})
	}
}

func TestTarExtractionPreservesContainedSymlink(t *testing.T) {
	dest := t.TempDir()
	if err := os.Mkdir(filepath.Join(dest, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dest, "probe")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	if err := extractTarGz(tarWithLink(t, "target"), dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "target", "guard"))
	if err != nil || string(got) != "modified" {
		t.Errorf("contained symlink did not resolve within the destination: %q, %v", got, err)
	}
	link, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil || link != "target" {
		t.Errorf("contained symlink was not preserved: %q, %v", link, err)
	}
}

func TestExtractionAllowsNamesBeginningWithTwoDots(t *testing.T) {
	for _, format := range []struct {
		name    string
		extract func([]byte, string) error
	}{
		{"tar.gz", extractTarGz},
		{"zip", extractZip},
	} {
		t.Run(format.name, func(t *testing.T) {
			dest := t.TempDir()
			if err := format.extract(containmentArchive(t, format.name, "..notes", []byte("allowed")), dest); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dest, "..notes"))
			if err != nil || string(got) != "allowed" {
				t.Errorf("safe filename was rejected: %q, %v", got, err)
			}
		})
	}
}

type tarEntry struct {
	name, link string
	data       []byte
}

func tarEntries(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o600, Size: int64(len(e.data))}
		if e.link != "" {
			hdr = &tar.Header{Name: e.name, Typeflag: tar.TypeSymlink, Linkname: e.link}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Each link below looks contained when checked lexically on its own, but
// "a -> ." makes "a/.." resolve to the destination's parent.
func TestTarExtractionSkipsComposedEscapingSymlinks(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "destination")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "probe")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	archive := tarEntries(t,
		tarEntry{name: "a", link: "."},
		tarEntry{name: "c", link: "a/../outside"},
		tarEntry{name: "a/b", link: "../outside"},
		tarEntry{name: "d", link: "sub/../../outside"},
	)
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if link, err := os.Readlink(filepath.Join(dest, "a")); err != nil || link != "." {
		t.Errorf("downward symlink was not preserved: %q, %v", link, err)
	}
	for _, name := range []string{"c", "b", "d"} {
		if _, err := os.Lstat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Errorf("symlink %q with a parent reference was retained: %v", name, err)
		}
		if _, err := os.ReadFile(filepath.Join(dest, name, "secret")); err == nil {
			t.Errorf("%q resolves outside the destination", name)
		}
	}
}

func TestExtractionSkipsNonLocalNamesAndContinues(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "destination")
	archive := tarEntries(t,
		tarEntry{name: "../escaped", data: []byte("no")},
		tarEntry{name: "nested/../../escaped", data: []byte("no")},
		tarEntry{name: "/absolute", data: []byte("no")},
		tarEntry{name: "./go/bin/tool", data: []byte("yes")},
	)
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escaped")); !os.IsNotExist(err) {
		t.Errorf("traversal entry was written outside the destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "absolute")); !os.IsNotExist(err) {
		t.Errorf("absolute entry was extracted: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "go", "bin", "tool"))
	if err != nil || string(got) != "yes" {
		t.Errorf("local entry after skipped entries was not extracted: %q, %v", got, err)
	}
}

func TestDownwardSymlinkTarget(t *testing.T) {
	for _, tc := range []struct {
		target string
		ok     bool
	}{
		{"target", true},
		{"dir/target", true},
		{"./target", true},
		{".", true},
		{"..notes", true},
		{"", false},
		{"..", false},
		{"../target", false},
		{"dir/../target", false},
		{"/etc/passwd", false},
	} {
		if _, ok := downwardSymlinkTarget(tc.target); ok != tc.ok {
			t.Errorf("downwardSymlinkTarget(%q) = %v, want %v", tc.target, ok, tc.ok)
		}
	}
}
