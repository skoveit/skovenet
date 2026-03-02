package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

// ---------------------------------------------------------------------------
// Sign + Verify roundtrip
// ---------------------------------------------------------------------------

func TestSignAndVerify_WithCorrectKey(t *testing.T) {
	// The hardcoded public key in signing.go corresponds to a specific
	// private key. We can't sign with the real operator key here, so we
	// test the SignWithKey + Verify contract with a fresh keypair, then
	// test Verify separately against the hardcoded public key.

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	msg := []byte("test message")
	privB64 := base64.StdEncoding.EncodeToString(priv)

	sig, err := SignWithKey(msg, privB64)
	if err != nil {
		t.Fatalf("SignWithKey: %v", err)
	}

	// Manually verify with the generated public key (not the hardcoded one)
	sigBytes, _ := base64.StdEncoding.DecodeString(sig)
	if !ed25519.Verify(pub, msg, sigBytes) {
		t.Error("signature should verify with the correct public key")
	}
}

// ---------------------------------------------------------------------------
// SignWithKey — error cases
// ---------------------------------------------------------------------------

func TestSignWithKey_InvalidEncoding(t *testing.T) {
	_, err := SignWithKey([]byte("msg"), "not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestSignWithKey_WrongKeySize(t *testing.T) {
	// Valid base64 but wrong size
	tooShort := base64.StdEncoding.EncodeToString([]byte("shortkey"))
	_, err := SignWithKey([]byte("msg"), tooShort)
	if err == nil {
		t.Error("expected error for wrong key size")
	}
}

// ---------------------------------------------------------------------------
// Verify — edge cases
// ---------------------------------------------------------------------------

func TestVerify_EmptySignature(t *testing.T) {
	if Verify([]byte("msg"), "") {
		t.Error("empty signature should not verify")
	}
}

func TestVerify_InvalidBase64(t *testing.T) {
	if Verify([]byte("msg"), "not!valid!base64!") {
		t.Error("invalid base64 should not verify")
	}
}

func TestVerify_WrongMessage(t *testing.T) {
	// Sign with a fresh key, then verify with the hardcoded public key.
	// This should fail because the keys don't match.
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	privB64 := base64.StdEncoding.EncodeToString(priv)

	sig, err := SignWithKey([]byte("msg"), privB64)
	if err != nil {
		t.Fatalf("SignWithKey: %v", err)
	}

	// Verify against hardcoded public key — should fail
	if Verify([]byte("msg"), sig) {
		t.Error("signature from wrong key should not verify against hardcoded key")
	}
}

// ---------------------------------------------------------------------------
// GetPublicKeyBase64
// ---------------------------------------------------------------------------

func TestGetPublicKeyBase64_NotEmpty(t *testing.T) {
	pub := GetPublicKeyBase64()
	if pub == "" {
		t.Error("GetPublicKeyBase64 should not be empty")
	}

	// Should be valid base64
	decoded, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		t.Errorf("public key is not valid base64: %v", err)
	}

	// Ed25519 public key is 32 bytes
	if len(decoded) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(decoded), ed25519.PublicKeySize)
	}
}
