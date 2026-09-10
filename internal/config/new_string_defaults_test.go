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

// An upgrade must not need a strings.json edit to get the new-user notice.
// notifySysopNewUser defaults on so no config change is required; if the string
// it formats has no fallback, an older strings.json leaves it empty and
// notifySysopsOfNewUser skips silently — the setting would read as enabled
// while nothing ever arrived.
func TestNewUserSysopPageSurvivesAnOlderStringsFile(t *testing.T) {
	dir := t.TempDir()
	// A strings.json with no mention of the key, as any pre-upgrade file has.
	if err := os.WriteFile(filepath.Join(dir, "strings.json"),
		[]byte(`{"pauseString":"press a key"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadStrings(dir)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if cfg.NewUserSysopPage == "" {
		t.Fatal("newUserSysopPage is empty for an older strings.json; the notice would be skipped silently")
	}
	if !strings.Contains(cfg.NewUserSysopPage, "%s") || !strings.Contains(cfg.NewUserSysopPage, "%d") {
		t.Errorf("fallback %q lacks the handle/node verbs the call site passes", cfg.NewUserSysopPage)
	}
	// The queued half of the same feature: with no fallback, the SYSOPNOTICES
	// step renders nothing for a sysop who was offline at signup — the case the
	// queue exists to cover.
	if cfg.NewUserSysopNotice == "" {
		t.Fatal("newUserSysopNotice is empty for an older strings.json; queued notices would fall back to the live page wording")
	}
	if n := strings.Count(cfg.NewUserSysopNotice, "%s"); n != 2 {
		t.Errorf("fallback %q takes %d %%s verbs, want 2 (handle and age)", cfg.NewUserSysopNotice, n)
	}
	if !strings.Contains(cfg.NewUserSysopNotice, "%d") {
		t.Errorf("fallback %q lacks the node verb the call site passes", cfg.NewUserSysopNotice)
	}
}
