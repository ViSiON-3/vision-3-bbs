package qwkservice

import (
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func area(id int, tag string) *message.MessageArea {
	return &message.MessageArea{ID: id, Tag: tag, Name: tag}
}

func TestConferenceMap_LoadMissingIsEmpty(t *testing.T) {
	m, err := LoadConferenceMap(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadConferenceMap on missing file: %v", err)
	}
	if _, ok := m.EntryForNumber(0); ok {
		t.Error("expected empty map")
	}
}

func TestConferenceMap_SyncAssignsNumbers(t *testing.T) {
	m, _ := LoadConferenceMap(filepath.Join(t.TempDir(), "m.json"))
	changed := m.Sync([]*message.MessageArea{
		area(1, "GENERAL"), area(2, "PRIVMAIL"), area(7, "TECH"),
	})
	if !changed {
		t.Fatal("Sync should report changed on first assignment")
	}
	if e, ok := m.EntryForTag("PRIVMAIL"); !ok || e.QWKNumber != 0 || e.Kind != KindPrivateMail {
		t.Errorf("PRIVMAIL: want {0 private_mail}, got %+v ok=%v", e, ok)
	}
	if e, ok := m.EntryForTag("GENERAL"); !ok || e.QWKNumber != 1 || e.Kind != KindPublic {
		t.Errorf("GENERAL: want {1 public}, got %+v ok=%v", e, ok)
	}
	if e, _ := m.EntryForTag("TECH"); e.QWKNumber != 7 {
		t.Errorf("TECH: want number 7 (area.ID), got %d", e.QWKNumber)
	}
}

func TestConferenceMap_ZeroIDCollisionBumped(t *testing.T) {
	m, _ := LoadConferenceMap(filepath.Join(t.TempDir(), "m.json"))
	// A public area whose ID is 0 must not claim the reserved 0 slot.
	m.Sync([]*message.MessageArea{area(0, "ODD")})
	if e, _ := m.EntryForTag("ODD"); e.QWKNumber == 0 {
		t.Errorf("public area with ID 0 must be bumped off 0, got %d", e.QWKNumber)
	}
}

func TestConferenceMap_StableAcrossResync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	m, _ := LoadConferenceMap(path)
	m.Sync([]*message.MessageArea{area(1, "GENERAL"), area(2, "PRIVMAIL")})
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadConferenceMap(path)
	if err != nil {
		t.Fatal(err)
	}
	// Re-sync with a new area added and an existing area renamed (Name changed).
	renamed := area(1, "GENERAL")
	renamed.Name = "General Chat"
	changed := reloaded.Sync([]*message.MessageArea{renamed, area(2, "PRIVMAIL"), area(3, "NEWS")})
	if !changed {
		t.Fatal("adding NEWS should report changed")
	}
	if e, _ := reloaded.EntryForTag("GENERAL"); e.QWKNumber != 1 {
		t.Errorf("GENERAL number must stay 1 across resync, got %d", e.QWKNumber)
	}
	if e, _ := reloaded.EntryForTag("PRIVMAIL"); e.QWKNumber != 0 {
		t.Errorf("PRIVMAIL number must stay 0, got %d", e.QWKNumber)
	}
	if _, ok := reloaded.EntryForTag("NEWS"); !ok {
		t.Error("NEWS should have been assigned a number")
	}
}

func TestConferenceMap_SyncNoChangeWhenComplete(t *testing.T) {
	m, _ := LoadConferenceMap(filepath.Join(t.TempDir(), "m.json"))
	areas := []*message.MessageArea{area(1, "GENERAL"), area(2, "PRIVMAIL")}
	m.Sync(areas)
	if m.Sync(areas) {
		t.Error("second Sync with no new areas should report no change")
	}
}
