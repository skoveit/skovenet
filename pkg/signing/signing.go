package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// Hardcoded operator public key for signature verification.
// Agents use this to verify commands are from an authorized operator.
// The corresponding private key is kept secret by the operator.
var (
	// Base64-encoded Ed25519 public key (32 bytes)
	publicKeyB64 = "/dEyY8LHoQB01678XbaBPRoMNBaf974dzJeiLLecHXk="

	// Decoded public key (initialized once)
	publicKey ed25519.PublicKey
)

func init() {
	pubBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		panic("invalid public key: " + err.Error())
	}
	publicKey = ed25519.PublicKey(pubBytes)
}

// SignWithKey signs the given message with the provided private key.
// The privateKey should be base64-encoded (64 bytes).
// Returns the base64-encoded signature.
func SignWithKey(message []byte, privateKeyB64 string) (string, error) {
	privBytes, err := base64.StdEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return "", errors.New("invalid private key encoding")
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return "", errors.New("invalid private key size")
	}
	privKey := ed25519.PrivateKey(privBytes)
	sig := ed25519.Sign(privKey, message)
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify checks if the signature is valid for the given message.
func Verify(message []byte, signatureB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return false
	}
	return ed25519.Verify(publicKey, message, sig)
}

// GetPublicKeyBase64 returns the public key as a base64 string.
// Useful for debugging and verification.
func GetPublicKeyBase64() string {
	return publicKeyB64
}
