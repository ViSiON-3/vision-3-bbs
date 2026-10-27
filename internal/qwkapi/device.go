package qwkapi

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Device pairing. A phone generates a key pair, registers the public half
// with a pair token from a password login, and from then on logs in by
// signing a single-use server challenge. The password is typed once and never
// stored on the device.

const (
	// maxDevicesPerUser caps how many devices one account can pair.
	maxDevicesPerUser = 10
	// maxDeviceNameLen bounds the display name a client gives its device.
	maxDeviceNameLen = 64
	// challengeTTL is how long a client has to sign a challenge.
	challengeTTL = 60 * time.Second
	// maxChallenges bounds the challenge store. Challenges are issued before
	// any proof of identity, so the store must not grow without limit.
	maxChallenges = 10000
)

// deviceLoginPrefix starts every message a device signs. It keeps a device
// key's signatures for this protocol from being valid for anything else.
const deviceLoginPrefix = "ViSiON/3 QWK API device login v1"

// deviceLoginMessage is the exact byte string a device signs to log in. It
// names this board, so a signature obtained by one BBS cannot be replayed to
// another that the same key is paired with, and the device, so a challenge
// issued for one device cannot be used by another.
func deviceLoginMessage(boardID, deviceID, challenge string) []byte {
	return []byte(deviceLoginPrefix + "\n" + boardID + "\n" + deviceID + "\n" + challenge)
}

// parseDeviceKey decodes a base64 PKIX (SubjectPublicKeyInfo) public key and
// accepts only the two types phones can hold: P-256 ECDSA, which the iOS
// Secure Enclave and Android Keystore support in hardware, and Ed25519. It
// returns the key and its canonical base64 encoding for storage.
func parseDeviceKey(b64 string) (crypto.PublicKey, string, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, "", errors.New("public key is not valid base64")
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, "", errors.New("public key is not a PKIX SubjectPublicKeyInfo")
	}
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return nil, "", errors.New("ECDSA keys must use P-256")
		}
	case ed25519.PublicKey:
	default:
		return nil, "", errors.New("key type must be P-256 ECDSA or Ed25519")
	}
	return pub, base64.StdEncoding.EncodeToString(der), nil
}

// verifyDeviceSignature checks sig over msg. ECDSA signatures are over the
// SHA-256 of msg and may be ASN.1 DER (iOS SecKey, Android Signature) or raw
// 64-byte r||s (WebCrypto); Ed25519 signatures are over msg itself.
func verifyDeviceSignature(pub crypto.PublicKey, msg, sig []byte) bool {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		digest := sha256.Sum256(msg)
		// A DER signature can, rarely, also be 64 bytes long, so a 64-byte
		// signature that fails as r||s is still tried as DER.
		if len(sig) == 64 {
			r := new(big.Int).SetBytes(sig[:32])
			s := new(big.Int).SetBytes(sig[32:])
			if ecdsa.Verify(k, digest[:], r, s) {
				return true
			}
		}
		return ecdsa.VerifyASN1(k, digest[:], sig)
	case ed25519.PublicKey:
		return ed25519.Verify(k, msg, sig)
	}
	return false
}

// cleanDeviceName trims a client-supplied device name, drops control
// characters (it is shown in logs and listings) and bounds its length.
func cleanDeviceName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if r := []rune(name); len(r) > maxDeviceNameLen {
		name = string(r[:maxDeviceNameLen])
	}
	if name == "" {
		name = "Unnamed device"
	}
	return name
}

// randomID returns n random bytes as hex.
func randomID(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// challengeStore holds issued login challenges until they are used or expire.
// Each is bound to the device it was requested for and works once.
type challengeStore struct {
	mu sync.Mutex
	m  map[string]challengeEntry
}

type challengeEntry struct {
	deviceID  string
	expiresAt time.Time
}

var errChallengeStoreFull = errors.New("too many outstanding challenges")

func newChallengeStore() *challengeStore {
	return &challengeStore{m: make(map[string]challengeEntry)}
}

// issue creates a challenge for deviceID. It is issued whether or not the
// device exists, so the endpoint does not reveal which device IDs are paired.
func (cs *challengeStore) issue(deviceID string) (string, time.Time, error) {
	ch, err := randomID(32)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(challengeTTL)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.m) >= maxChallenges {
		cs.sweepLocked()
		if len(cs.m) >= maxChallenges {
			return "", time.Time{}, errChallengeStoreFull
		}
	}
	cs.m[ch] = challengeEntry{deviceID: deviceID, expiresAt: exp}
	return ch, exp, nil
}

// consume uses up a challenge, reporting whether it was live and issued for
// deviceID. It is deleted either way, so a failed attempt cannot retry it.
func (cs *challengeStore) consume(ch, deviceID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	e, ok := cs.m[ch]
	if !ok {
		return false
	}
	delete(cs.m, ch)
	return e.deviceID == deviceID && time.Now().Before(e.expiresAt)
}

func (cs *challengeStore) sweep() {
	cs.mu.Lock()
	cs.sweepLocked()
	cs.mu.Unlock()
}

func (cs *challengeStore) sweepLocked() {
	now := time.Now()
	for k, e := range cs.m {
		if !now.Before(e.expiresAt) {
			delete(cs.m, k)
		}
	}
}
