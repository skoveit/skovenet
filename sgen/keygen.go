package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// KeyPair holds a generated Ed25519 key pair in base64 encoding.
type KeyPair struct {
	PublicKey  string // 44-char base64 (32 bytes)
	PrivateKey string // 88-char base64 (64 bytes)
}

// GenerateKeyPair creates a new Ed25519 key pair.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("key generation failed: %w", err)
	}
	return &KeyPair{
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
	}, nil
}

// PrintKeyPair displays a keypair in a formatted box.
func PrintKeyPair(kp *KeyPair) {
	fmt.Println("[*] Ed25519 Key Pair Generated")
	fmt.Printf("[*] Public Key  (embed in agent):   %s\n", kp.PublicKey)
	fmt.Printf("[*] Private Key (operator secret):  %s\n", kp.PrivateKey)
}
