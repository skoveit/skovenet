package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKeyPairProducesMatchingFreshKeys(t *testing.T) {
	first, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	public, err := base64.StdEncoding.DecodeString(first.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		t.Fatalf("invalid public key: size=%d, error=%v", len(public), err)
	}
	private, err := base64.StdEncoding.DecodeString(first.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		t.Fatalf("invalid private key: size=%d, error=%v", len(private), err)
	}
	message := []byte("skovenet generator verification")
	signature := ed25519.Sign(ed25519.PrivateKey(private), message)
	if !ed25519.Verify(ed25519.PublicKey(public), message, signature) {
		t.Fatal("generated public key does not verify its private key's signature")
	}
	second, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey == second.PublicKey || first.PrivateKey == second.PrivateKey {
		t.Error("independent key generation reused a key")
	}
}

func TestValidateConfigAcceptsSupportedPlatforms(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	for _, platform := range []struct{ OS, Arch string }{
		{"linux", "amd64"}, {"linux", "arm64"}, {"linux", "386"}, {"linux", "arm"},
		{"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"},
	} {
		t.Run(platform.OS+"_"+platform.Arch, func(t *testing.T) {
			cfg := &BuildConfig{OS: platform.OS, Arch: platform.Arch, PublicKey: key}
			if err := ValidateConfig(cfg); err != nil {
				t.Errorf("supported platform rejected: %v", err)
			}
		})
	}
}

func TestValidateConfigRejectsInvalidPlatformAndKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	for _, tt := range []struct {
		name string
		cfg  BuildConfig
		want string
	}{
		{"missing platform", BuildConfig{PublicKey: key}, "unsupported platform"},
		{"unknown OS", BuildConfig{OS: "unknown", Arch: "amd64", PublicKey: key}, "unsupported platform"},
		{"unknown architecture", BuildConfig{OS: "linux", Arch: "unknown", PublicKey: key}, "unsupported platform"},
		{"missing key", BuildConfig{OS: "linux", Arch: "amd64"}, "invalid public key size"},
		{"malformed base64", BuildConfig{OS: "linux", Arch: "amd64", PublicKey: "not!base64"}, "invalid public key"},
		{"short key", BuildConfig{OS: "linux", Arch: "amd64", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 31))}, "invalid public key size"},
		{"private key as public key", BuildConfig{OS: "linux", Arch: "amd64", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 64))}, "invalid public key size"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateConfig(&tt.cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ValidateConfig error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildEnvOverridesHostBuildSettings(t *testing.T) {
	for _, key := range []string{"GOROOT", "GOOS", "GOARCH", "CGO_ENABLED", "GOPATH", "GOFLAGS"} {
		t.Setenv(key, "host-setting-must-not-leak")
	}
	t.Setenv("SKOVENET_VERIFY_MARKER", "preserved")
	root := filepath.Join(t.TempDir(), "compiler")
	workdir := t.TempDir()
	env := buildEnv(root, "windows", "amd64", workdir)
	want := map[string]string{
		"GOROOT":                 root,
		"GOOS":                   "windows",
		"GOARCH":                 "amd64",
		"CGO_ENABLED":            "0",
		"GOPATH":                 filepath.Join(workdir, ".gopath"),
		"GOFLAGS":                "",
		"SKOVENET_VERIFY_MARKER": "preserved",
	}
	counts := make(map[string]int)
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("invalid environment entry %q", entry)
		}
		if expected, exists := want[key]; exists {
			counts[key]++
			if value != expected {
				t.Errorf("%s = %q, want %q", key, value, expected)
			}
		}
	}
	for key := range want {
		if counts[key] != 1 {
			t.Errorf("%s appeared %d times, want once", key, counts[key])
		}
	}
}
