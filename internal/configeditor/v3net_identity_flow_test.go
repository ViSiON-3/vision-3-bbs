package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// openIdentity navigates from the top menu to the V3Net Node Identity screen.
func openIdentity(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, m, "6", "1")
	if m.mode != modeV3NetIdentity || m.identitySubState != identityMain {
		t.Fatalf("mode/sub = %v/%d, want identity main", m.mode, m.identitySubState)
	}
	return m
}

// makeKey creates a key file at path and returns the keystore.
func makeKey(t *testing.T, path string) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(path)
	if err != nil {
		t.Fatalf("keystore.Load: %v", err)
	}
	return ks
}

// TestIdentity_NoKeyFile pins the messages shown when no key exists yet, and
// that Q and Escape return to the V3Net menu.
func TestIdentity_NoKeyFile(t *testing.T) {
	m, _ := newDiskModel(t)
	m = openIdentity(t, m)
	wantScreen(t, m, "No V3Net identity configured.")
	for _, k := range []string{"S", "E"} {
		m = press(t, m, k)
		if m.identitySubState != identityMain || !strings.HasPrefix(m.message, "No V3Net key file found") {
			t.Errorf("%s: sub=%d msg=%q", k, m.identitySubState, m.message)
		}
	}
	m = press(t, m, "Q")
	if m.mode != modeCategoryMenu {
		t.Errorf("Q: mode = %v", m.mode)
	}
	m = openIdentity(t, press(t, m, "esc"))
	m = press(t, m, "esc")
	if m.mode != modeCategoryMenu {
		t.Errorf("esc: mode = %v", m.mode)
	}
}

// TestIdentity_CorruptKeyFile pins that an unreadable key file is reported
// instead of being treated as missing.
func TestIdentity_CorruptKeyFile(t *testing.T) {
	m, _ := newDiskModel(t)
	if err := os.WriteFile(m.configs.V3Net.KeystorePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m = openIdentity(t, m)
	wantScreen(t, m, "Error:")
	for _, k := range []string{"S", "E"} {
		m = press(t, m, k)
		if !strings.Contains(m.message, "key file corrupt") {
			t.Errorf("%s: msg = %q", k, m.message)
		}
	}
}

// TestIdentity_ShowPhrase pins that S reveals the key's 24-word phrase and
// any key hides it again.
func TestIdentity_ShowPhrase(t *testing.T) {
	m, _ := newDiskModel(t)
	ks := makeKey(t, m.configs.V3Net.KeystorePath)
	want, _ := ks.Mnemonic()
	m = openIdentity(t, m)
	wantScreen(t, m, ks.NodeID(), ks.PubKeyBase64())

	m = press(t, m, "S")
	if m.identitySubState != identityShowPhrase || m.identityPhrase != want {
		t.Fatalf("sub=%d phrase match=%v", m.identitySubState, m.identityPhrase == want)
	}
	words := strings.Fields(want)
	wantScreen(t, m, "Recovery Seed Phrase", words[0], words[23])
	m = press(t, m, "x")
	if m.identitySubState != identityMain || m.identityPhrase != "" {
		t.Errorf("sub=%d phrase kept=%v", m.identitySubState, m.identityPhrase != "")
	}
}

// TestIdentity_ExportPhrase pins the export prompt: paths with ".." and
// existing files are refused, Escape cancels, and a good path gets a 0600
// recovery file naming the node.
func TestIdentity_ExportPhrase(t *testing.T) {
	m, _ := newDiskModel(t)
	ks := makeKey(t, m.configs.V3Net.KeystorePath)
	m = openIdentity(t, m)

	m = press(t, m, "E")
	if m.identitySubState != identityExportPrompt || m.textInput.Value() != "v3net-recovery.txt" {
		t.Fatalf("sub=%d default=%q", m.identitySubState, m.textInput.Value())
	}
	wantScreen(t, m, "Export Recovery Phrase")
	m = press(t, m, "esc")
	if m.identitySubState != identityMain {
		t.Fatalf("esc: sub = %d", m.identitySubState)
	}

	m = press(t, replaceText(t, press(t, m, "E"), "../escape.txt"), "enter")
	if m.message != "Path must not contain '..'" {
		t.Errorf("dotdot msg = %q", m.message)
	}

	out := filepath.Join(t.TempDir(), "recovery.txt")
	m = press(t, replaceText(t, press(t, m, "E"), out), "enter")
	if !strings.HasPrefix(m.message, "Saved to "+out) {
		t.Fatalf("export msg = %q", m.message)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	body, _ := os.ReadFile(out)
	phrase, _ := ks.Mnemonic()
	if !strings.Contains(string(body), "Node ID: "+ks.NodeID()) || !strings.Contains(string(body), strings.Fields(phrase)[23]) {
		t.Errorf("recovery file missing node ID or words:\n%s", body)
	}

	m = press(t, replaceText(t, press(t, m, "E"), out), "enter")
	if !strings.Contains(m.message, "already exists") {
		t.Errorf("overwrite msg = %q", m.message)
	}

	// Key file removed after the prompt opened.
	m = press(t, m, "E")
	if err := os.Remove(m.configs.V3Net.KeystorePath); err != nil {
		t.Fatal(err)
	}
	m = press(t, replaceText(t, m, filepath.Join(t.TempDir(), "x.txt")), "enter")
	if !strings.HasPrefix(m.message, "No V3Net key file found") {
		t.Errorf("missing key msg = %q", m.message)
	}
}

// TestIdentity_RecoverFromPhrase pins recovery: an invalid phrase is refused,
// No and Escape abandon it, and Yes replaces the key file with the recovered
// identity.
func TestIdentity_RecoverFromPhrase(t *testing.T) {
	m, _ := newDiskModel(t)
	makeKey(t, m.configs.V3Net.KeystorePath)
	other := makeKey(t, filepath.Join(t.TempDir(), "other.key"))
	phrase, _ := other.Mnemonic()
	m = openIdentity(t, m)

	m = press(t, m, "R")
	if m.identitySubState != identityRecoverInput {
		t.Fatalf("sub = %d", m.identitySubState)
	}
	wantScreen(t, m, "Recover Identity")
	m = press(t, typeText(t, m, "not a real phrase"), "enter")
	if m.identitySubState != identityMain || !strings.HasPrefix(m.message, "Invalid:") {
		t.Fatalf("sub=%d msg=%q", m.identitySubState, m.message)
	}
	m = press(t, m, "R", "esc")
	if m.identitySubState != identityMain {
		t.Fatalf("esc: sub = %d", m.identitySubState)
	}

	for _, cancel := range []string{"N", "esc"} {
		m = press(t, typeText(t, press(t, m, "R"), phrase), "enter")
		if m.identitySubState != identityRecoverConfirm || m.identityRecoverNodeID != other.NodeID() {
			t.Fatalf("sub=%d node=%q", m.identitySubState, m.identityRecoverNodeID)
		}
		wantScreen(t, m, "Node ID will become: "+other.NodeID())
		m = press(t, m, "z", cancel)
		if m.identitySubState != identityMain || m.identityRecoverInput != "" {
			t.Fatalf("%s: sub=%d", cancel, m.identitySubState)
		}
	}
	if got := makeKey(t, m.configs.V3Net.KeystorePath); got.NodeID() == other.NodeID() {
		t.Fatal("cancelled recovery replaced the key")
	}

	m = press(t, typeText(t, press(t, m, "R"), phrase), "enter", "y")
	if !strings.Contains(m.message, "Identity recovered. Node ID: "+other.NodeID()) {
		t.Fatalf("recover msg = %q", m.message)
	}
	if got := makeKey(t, m.configs.V3Net.KeystorePath); got.NodeID() != other.NodeID() {
		t.Errorf("key file node = %s, want %s", got.NodeID(), other.NodeID())
	}
}
