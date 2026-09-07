package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An upgraded system has a strings.json predating these keys. Without defaults
// the signup "ready" path would print an empty line — worse than the wrong
// message it replaces, because nothing appears at all.
func TestNewStringKeysHaveDefaults(t *testing.T) {
	dir := t.TempDir()
	// A strings.json with none of the new keys, as an existing install has.
	old := map[string]string{
		"newUserAccountCreated": "old text",
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "strings.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadStrings(dir)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}

	for name, got := range map[string]string{
		"NewUserAccountReady":      cfg.NewUserAccountReady,
		"NewUserPendingReview":     cfg.NewUserPendingReview,
		"MatrixAccountCannotLogon": cfg.MatrixAccountCannotLogon,
	} {
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s is empty on an upgraded strings.json; it would print nothing", name)
		}
	}

	// A value the install already set must not be overwritten.
	if cfg.NewUserAccountCreated != "old text" {
		t.Errorf("an existing customised string was overwritten: %q", cfg.NewUserAccountCreated)
	}
}

// The replacement matrix message takes three verbs where the old one took one.
// Reusing the old key would have appended "%!(EXTRA ...)" to every existing
// strings.json, so they are separate keys and must stay that way.
func TestMatrixMessageKeysHaveDistinctArity(t *testing.T) {
	// Read the shipped template: the legacy key has no code default, since
	// nothing reads it any more.
	b, err := os.ReadFile(filepath.Join("..", "..", "templates", "configs", "strings.json"))
	if err != nil {
		t.Fatalf("read shipped strings.json: %v", err)
	}
	var shipped map[string]string
	if err := json.Unmarshal(b, &shipped); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if n := strings.Count(shipped["matrixAccountNotValidated"], "%"); n != 1 {
		t.Errorf("legacy key should still take one verb, found %d — changing it breaks existing installs", n)
	}
	if n := strings.Count(shipped["matrixAccountCannotLogon"], "%"); n != 3 {
		t.Errorf("replacement key should take three verbs, found %d", n)
	}
}
