package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func setupAreasConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "conferences.json"), []conferenceConfig{
		{ID: 1, Tag: "LOCAL", Name: "Local"},
		{ID: 2, Tag: "FIDONET", Name: "Fidonet"},
	})
	writeJSONFile(t, filepath.Join(dir, "message_areas.json"), []message.MessageArea{
		{ID: 1, Position: 1, Tag: "GENERAL", ConferenceID: 1, AreaType: "local"},
		{ID: 2, Position: 2, Tag: "FD_NET", ConferenceID: 2, AreaType: "netmail", Network: "fidonet"},
		{ID: 3, Position: 3, Tag: "FD_LINUX", ConferenceID: 2, AreaType: "echomail", Network: "fidonet"},
		{ID: 4, Position: 4, Tag: "FD_BBS", ConferenceID: 2, AreaType: "echomail", Network: "fidonet", RealNameOnly: true},
		{ID: 5, Position: 5, Tag: "FD_OLD", ConferenceID: 2, AreaType: "echo", Network: "fidonet"},
	})
	return dir
}

func readAreas(t *testing.T, dir string) map[string]message.MessageArea {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "message_areas.json"))
	if err != nil {
		t.Fatal(err)
	}
	var areas []message.MessageArea
	if err := json.Unmarshal(data, &areas); err != nil {
		t.Fatal(err)
	}
	byTag := make(map[string]message.MessageArea, len(areas))
	for _, a := range areas {
		byTag[a.Tag] = a
	}
	return byTag
}

// TestAreasSetByConference is the issue #631 case: turn on Real Name Only for
// every echo in a conference in one command, leaving other areas alone.
func TestAreasSetByConference(t *testing.T) {
	dir := setupAreasConfig(t)
	if rc := runAreasSet([]string{"--config", dir, "--conference", "fidonet", "--type", "echomail",
		"--real-name", "on", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	got := readAreas(t, dir)
	for tag, want := range map[string]bool{"GENERAL": false, "FD_NET": false, "FD_LINUX": true, "FD_BBS": true, "FD_OLD": true} {
		if got[tag].RealNameOnly != want {
			t.Errorf("%s RealNameOnly = %v, want %v", tag, got[tag].RealNameOnly, want)
		}
	}
}

func TestAreasSetConferenceByIDAndMultipleSettings(t *testing.T) {
	dir := setupAreasConfig(t)
	if rc := runAreasSet([]string{"--config", dir, "--conference", "2", "--anonymous", "on",
		"--max-messages", "500", "--acs-write", "s20", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	got := readAreas(t, dir)
	for _, tag := range []string{"FD_NET", "FD_LINUX", "FD_BBS", "FD_OLD"} {
		a := got[tag]
		if a.AllowAnon == nil || !*a.AllowAnon || a.MaxMessages != 500 || a.ACSWrite != "s20" {
			t.Errorf("%s not updated: %+v", tag, a)
		}
	}
	if g := got["GENERAL"]; g.AllowAnon != nil || g.MaxMessages != 0 || g.ACSWrite != "" {
		t.Errorf("GENERAL should be untouched: %+v", g)
	}
}

func TestAreasSetTagGlob(t *testing.T) {
	dir := setupAreasConfig(t)
	if rc := runAreasSet([]string{"--config", dir, "--tag", "fd_l*", "--auto-join", "yes", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	got := readAreas(t, dir)
	if !got["FD_LINUX"].AutoJoin || got["FD_BBS"].AutoJoin {
		t.Errorf("glob matched wrong areas: LINUX=%v BBS=%v", got["FD_LINUX"].AutoJoin, got["FD_BBS"].AutoJoin)
	}
}

func TestAreasSetDryRunWritesNothing(t *testing.T) {
	dir := setupAreasConfig(t)
	path := filepath.Join(dir, "message_areas.json")
	before, _ := os.ReadFile(path)
	if rc := runAreasSet([]string{"--config", dir, "--all", "--real-name", "on", "--dry-run", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("dry run modified message_areas.json")
	}
	if _, err := os.Stat(path + ".lock"); err == nil {
		t.Error("dry run created the lock sidecar")
	}
}

func TestAreasSetRejectsBadInput(t *testing.T) {
	dir := setupAreasConfig(t)
	cases := map[string][]string{
		"no filter":          {"--real-name", "on"},
		"all plus filter":    {"--all", "--type", "echomail", "--real-name", "on"},
		"no settings":        {"--conference", "FIDONET"},
		"bad switch":         {"--conference", "FIDONET", "--real-name", "maybe"},
		"bad autocorrect":    {"--conference", "FIDONET", "--autocorrect", "maybe"},
		"negative number":    {"--conference", "FIDONET", "--max-age", "-1"},
		"unknown conference": {"--conference", "NOPE", "--real-name", "on"},
		"no matches":         {"--network", "fsxnet", "--real-name", "on"},
		"stray argument":     {"--conference", "FIDONET", "stray", "--real-name", "on"},
	}
	for name, args := range cases {
		if rc := runAreasSet(append([]string{"--config", dir}, args...)); rc == 0 {
			t.Errorf("%s: expected failure", name)
		}
	}
}

func TestAreasListRejectsStrayArgument(t *testing.T) {
	dir := setupAreasConfig(t)
	if rc := runAreasList([]string{"--config", dir, "--conference", "FIDONET", "stray", "--type", "echomail"}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestAreasHelpExitsZero(t *testing.T) {
	if rc := runAreasList([]string{"--help"}); rc != 0 {
		t.Errorf("list --help rc = %d, want 0", rc)
	}
	if rc := runAreasSet([]string{"-h"}); rc != 0 {
		t.Errorf("set -h rc = %d, want 0", rc)
	}
}

// --autocorrect sets the area's switch on or off, and default removes it so
// the area follows the board again.
func TestAreasSetAutocorrect(t *testing.T) {
	dir := setupAreasConfig(t)
	if rc := runAreasSet([]string{"--config", dir, "--type", "echomail", "--autocorrect", "on", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	got := readAreas(t, dir)
	if a := got["FD_LINUX"].Autocorrect; a == nil || !*a {
		t.Errorf("FD_LINUX Autocorrect = %v, want on", a)
	}
	if a := got["GENERAL"].Autocorrect; a != nil {
		t.Errorf("GENERAL Autocorrect = %v, want unset", a)
	}
	if rc := runAreasSet([]string{"--config", dir, "--tag", "FD_LINUX", "--autocorrect", "off", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if a := readAreas(t, dir)["FD_LINUX"].Autocorrect; a == nil || *a {
		t.Errorf("FD_LINUX Autocorrect = %v, want off", a)
	}
	if rc := runAreasSet([]string{"--config", dir, "--tag", "FD_LINUX", "--autocorrect", "default", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if a := readAreas(t, dir)["FD_LINUX"].Autocorrect; a != nil {
		t.Errorf("FD_LINUX Autocorrect = %v, want unset", a)
	}
}

// A new sponsor set by name drops the old account binding, so the BBS binds
// the new name (#1020) instead of leaving the old account in place.
func TestAreasSetSponsorClearsBinding(t *testing.T) {
	dir := setupAreasConfig(t)
	areas := readAreas(t, dir)
	g := areas["GENERAL"]
	g.Sponsor, g.SponsorID = "Old", 5
	writeJSONFile(t, filepath.Join(dir, "message_areas.json"), []message.MessageArea{g})
	if rc := runAreasSet([]string{"--config", dir, "--tag", "GENERAL", "--sponsor", "New", "--quiet"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if got := readAreas(t, dir)["GENERAL"]; got.Sponsor != "New" || got.SponsorID != 0 {
		t.Errorf("sponsor = %q, id %d; want New, 0", got.Sponsor, got.SponsorID)
	}
}
