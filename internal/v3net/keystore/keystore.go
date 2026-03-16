// Package keystore manages ed25519 keypair generation, persistence, and request signing.
package keystore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

// storedKey is the on-disk JSON format for the keypair.
type storedKey struct {
	PrivKeyB64 string `json:"privkey_b64"`
	PubKeyB64  string `json:"pubkey_b64"`
}

// Keystore wraps an ed25519 keypair and provides signing operations.
type Keystore struct {
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
}

// Load reads a keypair from path. If the file does not exist, a new keypair
// is generated and saved with mode 0600.
func Load(path string) (*Keystore, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return generate(path)
	}
	if err != nil {
		return nil, fmt.Errorf("keystore: read %s: %w", path, err)
	}

	var sk storedKey
	if err := json.Unmarshal(data, &sk); err != nil {
		return nil, fmt.Errorf("keystore: parse %s: %w", path, err)
	}

	privKey, err := base64.StdEncoding.DecodeString(sk.PrivKeyB64)
	if err != nil {
		return nil, fmt.Errorf("keystore: decode private key: %w", err)
	}
	pubKey, err := base64.StdEncoding.DecodeString(sk.PubKeyB64)
	if err != nil {
		return nil, fmt.Errorf("keystore: decode public key: %w", err)
	}

	if len(privKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("keystore: invalid private key size: %d", len(privKey))
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("keystore: invalid public key size: %d", len(pubKey))
	}

	return &Keystore{privKey: privKey, pubKey: pubKey}, nil
}

func generate(path string) (*Keystore, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keystore: generate keypair: %w", err)
	}

	sk := storedKey{
		PrivKeyB64: base64.StdEncoding.EncodeToString(privKey),
		PubKeyB64:  base64.StdEncoding.EncodeToString(pubKey),
	}
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("keystore: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, fmt.Errorf("keystore: write %s: %w", path, err)
	}

	return &Keystore{privKey: privKey, pubKey: pubKey}, nil
}

// NodeID returns the 16-char hex node identifier derived from the public key.
// It is the first 8 bytes (16 hex chars) of SHA-256(raw public key).
func (ks *Keystore) NodeID() string {
	h := sha256.Sum256(ks.pubKey)
	return fmt.Sprintf("%x", h[:8])
}

// PubKeyBase64 returns the base64 standard encoding of the raw public key.
func (ks *Keystore) PubKeyBase64() string {
	return base64.StdEncoding.EncodeToString(ks.pubKey)
}

// PubKey returns the raw ed25519 public key.
func (ks *Keystore) PubKey() ed25519.PublicKey {
	return ks.pubKey
}

// Sign produces a base64url-encoded ed25519 signature over the canonical string:
//
//	{method}\n{path}\n{dateUTC}\n{bodySHA256}
func (ks *Keystore) Sign(method, path, dateUTC, bodySHA256 string) (string, error) {
	canonical := method + "\n" + path + "\n" + dateUTC + "\n" + bodySHA256
	sig := ed25519.Sign(ks.privKey, []byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify checks a base64url-encoded signature against the given canonical components
// using the provided public key.
func Verify(pubKey ed25519.PublicKey, method, path, dateUTC, bodySHA256, sigB64 string) bool {
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	canonical := method + "\n" + path + "\n" + dateUTC + "\n" + bodySHA256
	return ed25519.Verify(pubKey, []byte(canonical), sig)
}
