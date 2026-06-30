package user

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// makeKey returns a valid OpenSSH ed25519 authorized_keys line and its SHA256
// fingerprint. comment is appended when non-empty.
func makeKey(t *testing.T, comment string) (line, fingerprint string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh pub: %v", err)
	}
	line = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return line, ssh.FingerprintSHA256(sshPub)
}

func TestNormalizeAuthorizedKey(t *testing.T) {
	line, fp := makeKey(t, "sysop@laptop")
	norm, info, err := NormalizeAuthorizedKey(line)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.Type != "ssh-ed25519" || info.Comment != "sysop@laptop" || info.Fingerprint != fp {
		t.Fatalf("info wrong: %+v (want fp %s)", info, fp)
	}
	if !strings.HasPrefix(norm, "ssh-ed25519 ") || !strings.HasSuffix(norm, " sysop@laptop") {
		t.Fatalf("normalized wrong: %q", norm)
	}
	if _, _, err := NormalizeAuthorizedKey("not a key"); err == nil {
		t.Fatal("expected error for malformed key")
	}
	if _, _, err := NormalizeAuthorizedKey("   "); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestAddPublicKeyDedup(t *testing.T) {
	line, _ := makeKey(t, "a@host")
	u := &User{}
	if _, err := u.AddPublicKey(line); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(u.PublicKeys))
	}
	// Same key bytes, different comment → duplicate.
	sameKeyDiffComment := strings.SplitN(line, " ", 3)
	dup := sameKeyDiffComment[0] + " " + sameKeyDiffComment[1] + " other@host"
	if _, err := u.AddPublicKey(dup); err == nil {
		t.Fatal("expected duplicate rejection")
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("dup must not be appended, got %d", len(u.PublicKeys))
	}
	if _, err := u.AddPublicKey("garbage"); err == nil {
		t.Fatal("expected malformed rejection")
	}
}

func TestRemovePublicKey(t *testing.T) {
	l1, fp1 := makeKey(t, "one")
	l2, fp2 := makeKey(t, "two")
	u := &User{PublicKeys: []string{l1, l2}}

	// Remove by full fingerprint.
	if info, err := u.RemovePublicKey(fp1); err != nil || info.Fingerprint != fp1 {
		t.Fatalf("remove by fp: %v %+v", err, info)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 left, got %d", len(u.PublicKeys))
	}
	// Remove remaining by index 1.
	if info, err := u.RemovePublicKey("1"); err != nil || info.Fingerprint != fp2 {
		t.Fatalf("remove by index: %v %+v", err, info)
	}
	if len(u.PublicKeys) != 0 {
		t.Fatalf("expected 0 left, got %d", len(u.PublicKeys))
	}
	// No match.
	if _, err := u.RemovePublicKey("SHA256:nope"); err == nil {
		t.Fatal("expected no-match error")
	}
}

func TestRemovePublicKeyVisibleIndex(t *testing.T) {
	// User has [corrupt, goodA, goodB]. ListPublicKeys sees index 1=goodA, 2=goodB.
	// RemovePublicKey("1") must remove goodA (first visible), not the corrupt entry.
	lA, fpA := makeKey(t, "keyA")
	lB, fpB := makeKey(t, "keyB")
	u := &User{PublicKeys: []string{"corrupt-garbage", lA, lB}}

	// Index 1 → goodA
	info, err := u.RemovePublicKey("1")
	if err != nil {
		t.Fatalf("remove index 1: %v", err)
	}
	if info.Fingerprint != fpA {
		t.Fatalf("expected fingerprint of goodA (%s), got %s", fpA, info.Fingerprint)
	}
	// Remaining: [corrupt, goodB]
	if len(u.PublicKeys) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(u.PublicKeys))
	}

	// Index 1 again → goodB (only visible key now)
	info, err = u.RemovePublicKey("1")
	if err != nil {
		t.Fatalf("remove index 1 (second): %v", err)
	}
	if info.Fingerprint != fpB {
		t.Fatalf("expected fingerprint of goodB (%s), got %s", fpB, info.Fingerprint)
	}
	// Remaining: [corrupt]
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(u.PublicKeys))
	}

	// Index 1 now → out of range (0 visible keys)
	if _, err := u.RemovePublicKey("1"); err == nil {
		t.Fatal("expected out-of-range error for index 1 when no visible keys")
	}
}

func TestListPublicKeysReportsUnparseable(t *testing.T) {
	good, _ := makeKey(t, "ok")
	u := &User{PublicKeys: []string{good, "corrupt-entry"}}
	keys, bad := u.ListPublicKeys()
	if len(keys) != 1 || bad != 1 {
		t.Fatalf("want 1 good / 1 bad, got %d / %d", len(keys), bad)
	}
}
