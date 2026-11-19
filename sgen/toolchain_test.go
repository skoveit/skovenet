package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type archiveTestEntry struct {
	name string
	data []byte
	mode int64
}

func tarTestArchive(t *testing.T, entries []archiveTestEntry) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Size: int64(len(entry.data)), Mode: entry.mode}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func zipTestArchive(t *testing.T, entries []archiveTestEntry) []byte {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(os.FileMode(entry.mode))
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestArchiveExtractionPreservesFiles(t *testing.T) {
	entries := []archiveTestEntry{
		{"go/bin/compiler", []byte("executable fixture\n"), 0o751},
		{"go/data/binary.bin", []byte{0, 0xff, '\n', '\r', 0x80}, 0o600},
		{"empty", nil, 0o600},
	}
	for _, format := range []struct {
		name    string
		archive func(*testing.T, []archiveTestEntry) []byte
		extract func([]byte, string) error
	}{
		{"tar.gz", tarTestArchive, extractTarGz},
		{"zip", zipTestArchive, extractZip},
	} {
		t.Run(format.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "new", "destination")
			if err := format.extract(format.archive(t, entries), dest); err != nil {
				t.Fatalf("extract: %v", err)
			}
			for _, entry := range entries {
				path := filepath.Join(dest, filepath.FromSlash(entry.name))
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, entry.data) {
					t.Errorf("%s contents = %v, error=%v; want %v", entry.name, got, err, entry.data)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != os.FileMode(entry.mode) {
					t.Errorf("%s mode = %o, want %o", entry.name, info.Mode().Perm(), entry.mode)
				}
			}
		})
	}
}

func TestArchiveExtractionSkipsParentTraversal(t *testing.T) {
	entries := []archiveTestEntry{
		{"../outside", []byte("must not escape"), 0o600},
		{"nested/../../outside", []byte("must not escape"), 0o600},
		{"inside", []byte("allowed"), 0o600},
	}
	for _, format := range []struct {
		name    string
		archive func(*testing.T, []archiveTestEntry) []byte
		extract func([]byte, string) error
	}{
		{"tar.gz", tarTestArchive, extractTarGz},
		{"zip", zipTestArchive, extractZip},
	} {
		t.Run(format.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "destination")
			if err := format.extract(format.archive(t, entries), dest); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "outside")); !os.IsNotExist(err) {
				t.Errorf("archive escaped its destination: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(dest, "inside"))
			if err != nil || string(got) != "allowed" {
				t.Errorf("safe entry was not extracted: %q, %v", got, err)
			}
		})
	}
}

func TestArchiveExtractionRejectsInvalidArchives(t *testing.T) {
	for _, tt := range []struct {
		name    string
		extract func([]byte, string) error
	}{
		{"tar.gz", extractTarGz},
		{"zip", extractZip},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.extract([]byte("not an archive"), t.TempDir()); err == nil {
				t.Error("invalid archive was accepted")
			}
		})
	}
}
