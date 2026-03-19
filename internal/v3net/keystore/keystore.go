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
	"path/filepath"
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
// is generated and saved with mode 0600. The created return value is true when
// a new keypair was generated.
func Load(path string) (*Keystore, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return generate(path)
	}
	if err != nil {
		return nil, false, fmt.Errorf("keystore: read %s: %w", path, err)
	}

	var sk storedKey
	if err := json.Unmarshal(data, &sk); err != nil {
		return nil, false, fmt.Errorf("keystore: parse %s: %w", path, err)
	}

	privKey, err := base64.StdEncoding.DecodeString(sk.PrivKeyB64)
	if err != nil {
		return nil, false, fmt.Errorf("keystore: decode private key: %w", err)
	}
	pubKey, err := base64.StdEncoding.DecodeString(sk.PubKeyB64)
	if err != nil {
		return nil, false, fmt.Errorf("keystore: decode public key: %w", err)
	}

	if len(privKey) != ed25519.PrivateKeySize {
		return nil, false, fmt.Errorf("keystore: invalid private key size: %d", len(privKey))
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return nil, false, fmt.Errorf("keystore: invalid public key size: %d", len(pubKey))
	}

	return &Keystore{privKey: privKey, pubKey: pubKey}, false, nil
}

func generate(path string) (*Keystore, bool, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, false, fmt.Errorf("keystore: generate keypair: %w", err)
	}
	ks := &Keystore{privKey: privKey, pubKey: pubKey}
	if err := ks.save(path); err != nil {
		return nil, false, err
	}
	return ks, true, nil
}

// save writes the keypair atomically: write to a temp file in the same
// directory, then rename over the target path. This prevents corruption
// if the process crashes mid-write and ensures 0600 permissions even
// when overwriting an existing file.
func (ks *Keystore) save(path string) error {
	sk := storedKey{
		PrivKeyB64: base64.StdEncoding.EncodeToString(ks.privKey),
		PubKeyB64:  base64.StdEncoding.EncodeToString(ks.pubKey),
	}
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return fmt.Errorf("keystore: marshal: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".v3net-key-*.tmp")
	if err != nil {
		return fmt.Errorf("keystore: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("keystore: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("keystore: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("keystore: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("keystore: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("keystore: rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
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

// SignRaw produces a raw ed25519 signature over the given payload bytes.
func (ks *Keystore) SignRaw(payload []byte) ([]byte, error) {
	return ed25519.Sign(ks.privKey, payload), nil
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

// Mnemonic returns the 24-word BIP39 recovery phrase for this keypair.
// Computed on-the-fly from the private key seed — never stored on disk.
// Never log the return value.
func (ks *Keystore) Mnemonic() (string, error) {
	return encodeMnemonic(ks.privKey.Seed())
}

// FromMnemonic reconstructs a Keystore from a 24-word BIP39 phrase.
// Input is case-insensitive and tolerant of extra whitespace.
// Does not write to disk.
func FromMnemonic(mnemonic string) (*Keystore, error) {
	seed, err := decodeMnemonic(mnemonic)
	if err != nil {
		return nil, err
	}
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	return &Keystore{privKey: privKey, pubKey: pubKey}, nil
}

// RecoverToFile reconstructs a keypair from a mnemonic and saves it to path
// with mode 0600. Overwrites any existing file. The caller is responsible for
// overwrite confirmation and path validation.
func RecoverToFile(mnemonic, path string) (*Keystore, error) {
	ks, err := FromMnemonic(mnemonic)
	if err != nil {
		return nil, err
	}
	if err := ks.save(path); err != nil {
		return nil, err
	}
	return ks, nil
}
