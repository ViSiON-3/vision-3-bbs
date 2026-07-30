package message

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newEchoTagTestManager(t *testing.T) *MessageManager {
	t.Helper()
	mm, err := NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

// Two areas on the SAME network sharing an EchoTag silently overwrite each
// other in areasByEchoTag, so inbound echomail for that tag lands in whichever
// area was indexed last — and map ordering makes that unstable across restarts.
// Adding such an area must be refused.
func TestAddAreaRejectsDuplicateEchoTagSameNetwork(t *testing.T) {
	mm := newEchoTagTestManager(t)

	if _, err := mm.AddArea(MessageArea{Tag: "FD_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "fsxnet"}); err != nil {
		t.Fatalf("first AddArea: %v", err)
	}
	_, err := mm.AddArea(MessageArea{Tag: "OTHER_LINUX", Name: "Linux 2", EchoTag: "LINUX", Network: "fsxnet"})
	if err == nil {
		t.Fatal("second AddArea with a duplicate EchoTag on the same network was accepted")
	}
	if !strings.Contains(err.Error(), "LINUX") {
		t.Errorf("error should name the conflicting echo tag, got: %v", err)
	}
}

// The same echo tag on a DIFFERENT network is legitimate — FTN echoes of the
// same name genuinely exist on separate networks, and the tosser already
// disambiguates by network when routing.
func TestAddAreaAllowsSameEchoTagOnDifferentNetwork(t *testing.T) {
	mm := newEchoTagTestManager(t)

	if _, err := mm.AddArea(MessageArea{Tag: "FD_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "fsxnet"}); err != nil {
		t.Fatalf("first AddArea: %v", err)
	}
	if _, err := mm.AddArea(MessageArea{Tag: "AN_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "agoranet"}); err != nil {
		t.Fatalf("same echo tag on a different network should be allowed: %v", err)
	}
}

// UpdateAreaByID must apply the same rule, or the editor becomes a way around it.
func TestUpdateAreaRejectsDuplicateEchoTagSameNetwork(t *testing.T) {
	mm := newEchoTagTestManager(t)

	if _, err := mm.AddArea(MessageArea{Tag: "FD_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "fsxnet"}); err != nil {
		t.Fatalf("AddArea 1: %v", err)
	}
	id2, err := mm.AddArea(MessageArea{Tag: "FD_BBS", Name: "BBS", EchoTag: "BBS", Network: "fsxnet"})
	if err != nil {
		t.Fatalf("AddArea 2: %v", err)
	}

	err = mm.UpdateAreaByID(id2, MessageArea{ID: id2, Tag: "FD_BBS", Name: "BBS", EchoTag: "LINUX", Network: "fsxnet"})
	if err == nil {
		t.Fatal("UpdateAreaByID accepted an EchoTag already used on the same network")
	}
}

// Updating an area without touching its EchoTag must not trip the new check
// against its own existing entry.
func TestUpdateAreaAllowsKeepingItsOwnEchoTag(t *testing.T) {
	mm := newEchoTagTestManager(t)

	id, err := mm.AddArea(MessageArea{Tag: "FD_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "fsxnet"})
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	if err := mm.UpdateAreaByID(id, MessageArea{ID: id, Tag: "FD_LINUX", Name: "Linux Renamed", EchoTag: "LINUX", Network: "fsxnet"}); err != nil {
		t.Errorf("updating an area while keeping its own EchoTag was rejected: %v", err)
	}
}

// An existing config may already contain duplicate echo tags -- those configs
// must keep loading (rejecting them would lock the sysop out of the editor they
// need to fix it), but the collision has to be visible in the log.
func TestLoadAreasWarnsOnDuplicateEchoTagAndKeepsBothAreas(t *testing.T) {
	configDir, dataDir := t.TempDir(), t.TempDir()
	areas := `[
		{"id":1,"tag":"FD_LINUX","name":"Linux","echo_tag":"LINUX","network":"fsxnet","area_type":"echomail"},
		{"id":2,"tag":"OTHER_LINUX","name":"Linux 2","echo_tag":"LINUX","network":"fsxnet","area_type":"echomail"}
	]`
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatalf("write areas: %v", err)
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	mm, err := NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatalf("loading a config with duplicate echo tags must not fail: %v", err)
	}
	if _, ok := mm.GetAreaByID(1); !ok {
		t.Error("area 1 was dropped")
	}
	if _, ok := mm.GetAreaByID(2); !ok {
		t.Error("area 2 was dropped")
	}
	out := logs.String()
	if !strings.Contains(out, "duplicate FTN echo tag") {
		t.Errorf("no warning logged for the duplicate echo tag; log was:\n%s", out)
	}
	if !strings.Contains(out, "OTHER_LINUX") || !strings.Contains(out, "FD_LINUX") {
		t.Errorf("warning should name both areas; log was:\n%s", out)
	}
}

// A rejected update must not leave the indexes half-modified: the area has to
// remain findable under the tag it still has on disk.
func TestUpdateAreaRejectedByEchoTagLeavesIndexesIntact(t *testing.T) {
	mm := newEchoTagTestManager(t)

	if _, err := mm.AddArea(MessageArea{Tag: "FD_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "fsxnet"}); err != nil {
		t.Fatalf("AddArea 1: %v", err)
	}
	id2, err := mm.AddArea(MessageArea{Tag: "FD_BBS", Name: "BBS", EchoTag: "BBS", Network: "fsxnet"})
	if err != nil {
		t.Fatalf("AddArea 2: %v", err)
	}

	// Rename and steal the other area's echo tag in one update: must be refused.
	if err := mm.UpdateAreaByID(id2, MessageArea{ID: id2, Tag: "FD_BBS_NEW", Name: "BBS", EchoTag: "LINUX", Network: "fsxnet"}); err == nil {
		t.Fatal("update with a duplicate echo tag was accepted")
	}

	if _, ok := mm.GetAreaByTag("FD_BBS"); !ok {
		t.Error("after a rejected update the area is no longer findable by its unchanged tag")
	}
	if _, ok := mm.GetAreaByTag("FD_BBS_NEW"); ok {
		t.Error("the rejected new tag was indexed")
	}
	if a, ok := mm.GetAreaByEchoTag("LINUX"); !ok || a.Tag != "FD_LINUX" {
		t.Errorf("echo tag LINUX no longer resolves to FD_LINUX, got %+v (found=%v)", a, ok)
	}
}

// The tosser tries GetAreaByTag before the echo-tag index and that lookup is
// not network-gated, so an area whose Tag equals another area's EchoTag takes
// the mail unconditionally -- the echo-tagged area never sees any.
func TestAddAreaRejectsEchoTagThatIsAnotherAreasTag(t *testing.T) {
	mm := newEchoTagTestManager(t)

	if _, err := mm.AddArea(MessageArea{Tag: "LINUX", Name: "Local Linux", Network: ""}); err != nil {
		t.Fatalf("AddArea local: %v", err)
	}
	_, err := mm.AddArea(MessageArea{Tag: "FD_LINUX", Name: "Linux", EchoTag: "LINUX", Network: "fsxnet"})
	if err == nil {
		t.Fatal("AddArea accepted an echo tag that is already another area's local tag")
	}
	if !strings.Contains(err.Error(), "local tag") {
		t.Errorf("error should say the tag is a local tag, got: %v", err)
	}
}

// An area keeping EchoTag == its own Tag must not be treated as conflicting
// with itself, on add or on update.
func TestAreaMayUseItsOwnTagAsEchoTag(t *testing.T) {
	mm := newEchoTagTestManager(t)

	id, err := mm.AddArea(MessageArea{Tag: "FSX_GEN", Name: "General", EchoTag: "FSX_GEN", Network: "fsxnet"})
	if err != nil {
		t.Fatalf("an area whose EchoTag equals its own Tag was rejected: %v", err)
	}
	if err := mm.UpdateAreaByID(id, MessageArea{ID: id, Tag: "FSX_GEN", Name: "General Chat", EchoTag: "FSX_GEN", Network: "fsxnet"}); err != nil {
		t.Errorf("updating that area was rejected: %v", err)
	}
}

func TestLoadAreasWarnsWhenEchoTagIsAnotherAreasTag(t *testing.T) {
	configDir, dataDir := t.TempDir(), t.TempDir()
	// Deliberately listed with the echo-tagged area FIRST, so the check cannot
	// depend on load order.
	areas := `[
		{"id":1,"tag":"FD_LINUX","name":"Linux","echo_tag":"LINUX","network":"fsxnet","area_type":"echomail"},
		{"id":2,"tag":"LINUX","name":"Local Linux","area_type":"local"}
	]`
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatalf("write areas: %v", err)
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	if _, err := NewMessageManager(dataDir, configDir, "TestBBS", nil); err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "another area's local tag") {
		t.Errorf("no warning logged for the tag/echo-tag collision; log was:\n%s", out)
	}
}
