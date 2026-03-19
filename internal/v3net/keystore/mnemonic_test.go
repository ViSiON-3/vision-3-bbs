package keystore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMnemonic_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	phrase, err := ks.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic failed: %v", err)
	}
	words := strings.Split(phrase, " ")
	if len(words) != 24 {
		t.Fatalf("expected 24 words, got %d", len(words))
	}
	recovered, err := FromMnemonic(phrase)
	if err != nil {
		t.Fatalf("FromMnemonic failed: %v", err)
	}
	if ks.NodeID() != recovered.NodeID() {
		t.Errorf("node IDs differ: %q vs %q", ks.NodeID(), recovered.NodeID())
	}
	if ks.PubKeyBase64() != recovered.PubKeyBase64() {
		t.Error("public keys differ after mnemonic round-trip")
	}
}

func TestMnemonic_ChecksumValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	phrase, err := ks.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic failed: %v", err)
	}
	words := strings.Split(phrase, " ")
	if words[0] == "abandon" {
		words[0] = "ability"
	} else {
		words[0] = "abandon"
	}
	tampered := strings.Join(words, " ")
	_, err = FromMnemonic(tampered)
	if err == nil {
		t.Error("expected checksum error for tampered mnemonic")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected checksum error, got: %v", err)
	}
}

func TestMnemonic_InvalidWord(t *testing.T) {
	phrase := "abandon ability able about above absent absorb abstract absurd abuse access accident " +
		"acid acoustic acquire across act action actor actress xyznotaword actual adapt add"
	_, err := FromMnemonic(phrase)
	if err == nil {
		t.Error("expected error for invalid word")
	}
	if !strings.Contains(err.Error(), "xyznotaword") {
		t.Errorf("error should mention the invalid word, got: %v", err)
	}
}

func TestMnemonic_WrongWordCount(t *testing.T) {
	tests := []struct {
		name  string
		words int
	}{
		{"too few", 23},
		{"too many", 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := make([]string, tt.words)
			for i := range words {
				words[i] = "abandon"
			}
			_, err := FromMnemonic(strings.Join(words, " "))
			if err == nil {
				t.Error("expected error for wrong word count")
			}
		})
	}
}

func TestMnemonic_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	phrase, err := ks.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic failed: %v", err)
	}
	upper := strings.ToUpper(phrase)
	recovered, err := FromMnemonic(upper)
	if err != nil {
		t.Fatalf("FromMnemonic with uppercase failed: %v", err)
	}
	if ks.NodeID() != recovered.NodeID() {
		t.Errorf("node IDs differ with uppercase input")
	}
}

func TestMnemonic_InputNormalization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	phrase, err := ks.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic failed: %v", err)
	}
	messy := "  " + strings.Replace(phrase, " ", "  \t ", 1) + "  \n"
	recovered, err := FromMnemonic(messy)
	if err != nil {
		t.Fatalf("FromMnemonic with messy input failed: %v", err)
	}
	if ks.NodeID() != recovered.NodeID() {
		t.Errorf("node IDs differ with messy whitespace input")
	}
}

func TestMnemonic_BIP39Vector(t *testing.T) {
	seed := make([]byte, 32)
	phrase, err := encodeMnemonic(seed)
	if err != nil {
		t.Fatalf("encodeMnemonic failed: %v", err)
	}
	expected := "abandon abandon abandon abandon abandon abandon abandon abandon " +
		"abandon abandon abandon abandon abandon abandon abandon abandon " +
		"abandon abandon abandon abandon abandon abandon abandon art"
	if phrase != expected {
		t.Errorf("BIP39 test vector mismatch:\ngot:  %s\nwant: %s", phrase, expected)
	}
	decoded, err := decodeMnemonic(phrase)
	if err != nil {
		t.Fatalf("decodeMnemonic failed: %v", err)
	}
	for i, b := range decoded {
		if b != 0 {
			t.Errorf("decoded seed byte %d = %d, want 0", i, b)
		}
	}
}

func TestRecoverToFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	ks, _, err := Load(filepath.Join(dir, "original.key"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	phrase, err := ks.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic failed: %v", err)
	}
	recoveredPath := filepath.Join(dir, "recovered.key")
	recovered, err := RecoverToFile(phrase, recoveredPath)
	if err != nil {
		t.Fatalf("RecoverToFile failed: %v", err)
	}
	if ks.NodeID() != recovered.NodeID() {
		t.Errorf("node IDs differ: %q vs %q", ks.NodeID(), recovered.NodeID())
	}
	loaded, _, err := Load(recoveredPath)
	if err != nil {
		t.Fatalf("Load recovered file failed: %v", err)
	}
	if ks.NodeID() != loaded.NodeID() {
		t.Errorf("loaded node ID differs: %q vs %q", ks.NodeID(), loaded.NodeID())
	}
}

func TestRecoverToFile_Overwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	ks1, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	ks2, _, err := Load(filepath.Join(dir, "other.key"))
	if err != nil {
		t.Fatalf("Load other failed: %v", err)
	}
	phrase2, err := ks2.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic failed: %v", err)
	}
	_, err = RecoverToFile(phrase2, path)
	if err != nil {
		t.Fatalf("RecoverToFile failed: %v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load overwritten failed: %v", err)
	}
	if loaded.NodeID() != ks2.NodeID() {
		t.Errorf("expected node ID %q, got %q", ks2.NodeID(), loaded.NodeID())
	}
	if loaded.NodeID() == ks1.NodeID() {
		t.Error("overwritten file still has the original key")
	}
}

func TestRecoverToFile_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	ks, _, err := Load(filepath.Join(dir, "src.key"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	phrase, _ := ks.Mnemonic()
	_, err = RecoverToFile(phrase, path)
	if err != nil {
		t.Fatalf("RecoverToFile failed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600, got %o", info.Mode().Perm())
	}
}
