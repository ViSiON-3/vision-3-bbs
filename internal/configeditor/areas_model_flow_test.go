package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// TestMsgAreaFields_NetworkTypes pins that each networked area type shows its
// own fields, that the Network picker offers the configured networks of that
// kind, and that the values persist.
func TestMsgAreaFields_NetworkTypes(t *testing.T) {
	cases := []struct {
		areaType, network string
		extra             map[string]string
		check             func(message.MessageArea) bool
	}{
		{"echomail", "fidonet", map[string]string{"Echo Tag": "FIDO_GEN", "Origin Addr": "1:2/3.1", "Sponsor": "Sam"},
			func(a message.MessageArea) bool {
				return a.EchoTag == "FIDO_GEN" && a.OriginAddr == "1:2/3.1" && a.Sponsor == "Sam"
			}},
		{"netmail", "fidonet", nil, func(message.MessageArea) bool { return true }},
		{"v3net", "felnet", map[string]string{"Echo Tag": "fel.general"},
			func(a message.MessageArea) bool { return a.EchoTag == "fel.general" }},
		{"qwknet", "dovenet", map[string]string{"QWK Conference": "2001", "Echo Tag": "General"},
			func(a message.MessageArea) bool { return a.QWKConference == 2001 && a.EchoTag == "General" }},
	}
	for _, tc := range cases {
		t.Run(tc.areaType, func(t *testing.T) {
			m, dir := newDiskModel(t)
			seedFTN(&m)
			m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{Network: "felnet"}, {Network: "felnet"}}
			m.configs.V3Net.Hub.Networks = []config.V3NetHubNetwork{{Name: "homenet"}}
			m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{"dovenet": {HubID: "VERT"}}
			m = press(t, openRecordList(t, m, "msgarea"), "i", "enter")
			m = pickRecField(t, m, "Area Type", tc.areaType)
			m = pickRecField(t, m, "Network", tc.network)
			for label, val := range tc.extra {
				m = setRecField(t, m, label, val)
			}
			m = setRecField(t, m, "Max Messages", "500")
			m = setRecField(t, m, "Max Age", "90")
			m = press(t, gotoField(t, m, "Real Name Only"), "space")
			m = press(t, gotoField(t, m, "Allow Anonymous"), "space")
			saveAndQuit(t, m)
			ac := reloadConfigs(t, dir)
			if len(ac.MsgAreas) != 1 {
				t.Fatalf("areas = %+v", ac.MsgAreas)
			}
			a := ac.MsgAreas[0]
			if a.AreaType != tc.areaType || a.Network != tc.network || a.MaxMessages != 500 || a.MaxAge != 90 ||
				!a.RealNameOnly || a.AllowAnon == nil || !*a.AllowAnon || !tc.check(a) {
				t.Errorf("area = %+v", a)
			}
		})
	}
}

// TestV3NetLeafPickerDedupes pins that the V3Net network picker lists each
// subscribed network once, and only subscriptions (not hosted networks).
func TestV3NetLeafPickerDedupes(t *testing.T) {
	m, _ := newDiskModel(t)
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{Network: "felnet"}, {Network: "felnet"}}
	m.configs.V3Net.Hub.Networks = []config.V3NetHubNetwork{{Name: "homenet"}, {Name: "felnet"}}
	var names []string
	for _, it := range m.buildV3NetNetworkLookupItems() {
		names = append(names, it.Value)
	}
	if strings.Join(names, ",") != "felnet" {
		t.Errorf("items = %v", names)
	}
}

// TestFileAreaConferencePicker pins that a file area's Conference is chosen
// from the configured conferences and saved by ID.
func TestFileAreaConferencePicker(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.Conferences = []conference.Conference{{ID: 1, Position: 1, Tag: "LOCAL", Name: "Local"}, {ID: 7, Position: 2, Tag: "NET", Name: "Networked"}}
	m = press(t, openRecordList(t, m, "filearea"), "i", "enter")
	m = pickRecField(t, m, "Conference", "7")
	wantScreen(t, m, "Networked")
	saveAndQuit(t, m)
	if fa := reloadConfigs(t, dir).FileAreas; len(fa) != 1 || fa[0].ConferenceID != 7 {
		t.Errorf("file areas = %+v", fa)
	}
}

// TestV3NetLeafFields_Persist pins the subscription record fields: boards are
// split on commas and blanks dropped.
func TestV3NetLeafFields_Persist(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{HubURL: "https://h", Network: "felnet", Boards: []string{"a.b"}}}
	m = press(t, openRecordList(t, m, "v3netleaf"), "enter")
	wantScreen(t, m, "1 area(s) subscribed")
	m = setRecField(t, m, "Hub URL", "https://hub2")
	m = setRecField(t, m, "Boards", "fel.one, ,fel.two")
	m = setRecField(t, m, "Poll Interval", "10m")
	m = press(t, gotoField(t, m, "Browse Areas"), "tab")
	if m.mode != modeV3NetAreaBrowser {
		t.Fatalf("tab on Browse Areas: mode = %v", m.mode)
	}
	m = press(t, m, "esc") // leave while loading
	m = setRecField(t, m, "Boards", "")
	m = setRecField(t, m, "Boards", "fel.one,fel.two")
	saveAndQuit(t, m)
	l := reloadConfigs(t, dir).V3Net.Leaves[0]
	if l.HubURL != "https://hub2" || strings.Join(l.Boards, ",") != "fel.one,fel.two" || l.PollInterval != "10m" {
		t.Errorf("leaf = %+v", l)
	}
}

// TestZipLabSteps_Persist pins the ZipLab pipeline-steps screen: toggles and
// file names are saved to ziplab.json.
func TestZipLabSteps_Persist(t *testing.T) {
	m, dir := newDiskModel(t)
	m = press(t, m, "B")
	if m.mode != modeSysConfigMenu || m.sysMenuTitle != "ZipLab Upload Processing" {
		t.Fatalf("mode/title = %v/%q", m.mode, m.sysMenuTitle)
	}
	var idx int
	for i, it := range m.sysMenuItems {
		if it.Label == "Pipeline Steps" {
			idx = i
		}
	}
	for i := 0; i < idx; i++ {
		m = press(t, m, "down")
	}
	m = press(t, m, "enter")
	before := m.configs.ZipLab.Steps
	for _, label := range []string{"Test Integrity", "Extract", "Virus Scan", "DIZ / Remove Ads", "Add Comment", "Include File"} {
		for m.sysFields[m.editField].Label != label {
			m = press(t, m, "down")
		}
		m = press(t, m, "space")
	}
	for label, val := range map[string]string{"Patterns File": "ads.txt", "Comment File": "c.txt", "File to Include": "ad.nfo"} {
		for m.sysFields[m.editField].Label != label {
			m = press(t, m, "down")
		}
		m = press(t, replaceText(t, press(t, m, "enter"), val), "enter")
	}
	saveAndQuit(t, m)
	st := reloadConfigs(t, dir).ZipLab.Steps
	if st.TestIntegrity.Enabled == before.TestIntegrity.Enabled || st.ExtractToTemp.Enabled == before.ExtractToTemp.Enabled ||
		st.VirusScan.Enabled == before.VirusScan.Enabled || st.RemoveAds.Enabled == before.RemoveAds.Enabled ||
		st.AddComment.Enabled == before.AddComment.Enabled || st.IncludeFile.Enabled == before.IncludeFile.Enabled {
		t.Errorf("toggles not all flipped: before=%+v after=%+v", before, st)
	}
	if st.RemoveAds.PatternsFile != "ads.txt" || st.AddComment.CommentFile != "c.txt" || st.IncludeFile.FilePath != "ad.nfo" {
		t.Errorf("files = %q %q %q", st.RemoveAds.PatternsFile, st.AddComment.CommentFile, st.IncludeFile.FilePath)
	}
}

// TestQuitConfirm_CleanSession pins the plain Exit? dialog: No and Escape
// stay in the editor, Yes and Enter-on-Yes quit.
func TestQuitConfirm_CleanSession(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "esc")
	if m.mode != modeQuitConfirm {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, "Exit?")
	m = press(t, m, "n", "Q", "esc", "Q", "right", "enter")
	if m.mode != modeTopMenu {
		t.Fatalf("declined quit: mode = %v", m.mode)
	}
	m = press(t, m, "Q")
	if _, cmd := m.Update(keyMsg("enter")); !isQuit(cmd) {
		t.Error("enter on Yes did not quit")
	}
	if _, cmd := m.Update(keyMsg("Y")); !isQuit(cmd) {
		t.Error("Y did not quit")
	}
}

// TestExitConfirm_DirtySession pins the unsaved-changes exit dialog: Escape
// cancels, and No quits without writing anything.
func TestExitConfirm_DirtySession(t *testing.T) {
	m, dir := newDiskModel(t)
	m.dirty = true
	m = press(t, m, "Q")
	if m.mode != modeExitConfirm {
		t.Fatalf("mode = %v", m.mode)
	}
	m = press(t, m, "esc")
	if m.mode != modeTopMenu {
		t.Fatalf("esc: mode = %v", m.mode)
	}
	m = press(t, m, "Q", "left")
	if _, cmd := m.Update(keyMsg("enter")); !isQuit(cmd) {
		t.Fatal("No did not quit")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Error("quitting without saving wrote config.json")
	}
}

// TestSaveFailure_StaysInEditor pins that a save that cannot be written keeps
// the editor open with the error shown, for both the exit dialog and the
// save-and-continue prompt.
func TestSaveFailure_StaysInEditor(t *testing.T) {
	m, dir := newDiskModel(t)
	// Replace the configs directory with a file so every write fails.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.configs.Server.BoardName = "Changed"
	m.dirty = true
	m = press(t, m, "Q")
	res, cmd := m.Update(keyMsg("y"))
	m = asModel(t, res)
	if isQuit(cmd) || m.mode != modeTopMenu || !strings.HasPrefix(m.message, "SAVE ERROR") || !m.dirty {
		t.Fatalf("quit=%v mode=%v dirty=%v msg=%q", isQuit(cmd), m.mode, m.dirty, m.message)
	}
	wantScreen(t, m, "SAVE ERROR")

	m.configs.V3Net.Hub.Networks = []config.V3NetHubNetwork{{Name: "felnet"}}
	m = press(t, openRecordList(t, m, "v3nethub"), "esc")
	if m.mode != modeNavSaveConfirm {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, "Save")
	m = press(t, m, "y")
	if m.mode != modeRecordList || !strings.HasPrefix(m.message, "SAVE ERROR") {
		t.Errorf("failed nav save: mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, m, "esc", "enter")
	if m.mode != modeRecordList {
		t.Errorf("failed nav save (enter): mode = %v", m.mode)
	}
}

// TestModelLifecycle pins startup and resize handling: the optional splash is
// dismissed by its timer or any key, Init sets the title, resizes clamp to
// the minimum size, and the startup message is shown.
func TestModelLifecycle(t *testing.T) {
	m, _ := newDiskModel(t)
	if m.Init() == nil {
		t.Error("Init returned no command")
	}
	s := m.WithStartupSplash().WithStartupMessage("binkd.conf regenerated")
	if s.Init() == nil || !s.splashActive {
		t.Fatal("splash not enabled")
	}
	if screen(s) == screen(m) {
		t.Error("splash renders the same as the menu")
	}
	s = press(t, s, "1")
	if s.splashActive || s.mode != modeTopMenu {
		t.Errorf("key did not just dismiss splash: active=%v mode=%v", s.splashActive, s.mode)
	}
	wantScreen(t, s, "binkd.conf regenerated")
	s2 := asModel(t, first(m.WithStartupSplash().Update(splashDoneMsg{})))
	if s2.splashActive {
		t.Error("timer did not dismiss splash")
	}

	m = asModel(t, first(m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})))
	if m.width != minWidth || m.height != minHeight {
		t.Errorf("small resize = %dx%d", m.width, m.height)
	}
	m = asModel(t, first(m.Update(tea.WindowSizeMsg{Width: 132, Height: 50})))
	if m.width != 132 || m.height != 50 {
		t.Errorf("resize = %dx%d", m.width, m.height)
	}
	m.mode = modeHelp
	m = press(t, m, "x")
	if m.mode != modeTopMenu {
		t.Errorf("help: mode = %v", m.mode)
	}
}

// TestCategoryMenu_Navigation pins the category sub-menu keys: movement,
// digit hotkeys (out-of-range ignored), and Q/Escape back to the top menu.
func TestCategoryMenu_Navigation(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "3")
	if m.mode != modeCategoryMenu {
		t.Fatalf("mode = %v", m.mode)
	}
	wantScreen(t, m, "Areas and Conferences", "File Areas")
	m = press(t, m, "end", "down")
	if m.catMenuCursor != 2 {
		t.Errorf("end: cursor = %d", m.catMenuCursor)
	}
	m = press(t, m, "home", "up", "down")
	if m.catMenuCursor != 1 {
		t.Errorf("cursor = %d", m.catMenuCursor)
	}
	m = press(t, m, "9", "x")
	if m.mode != modeCategoryMenu {
		t.Fatalf("stray keys: mode = %v", m.mode)
	}
	m = press(t, m, "enter")
	if m.mode != modeRecordList || m.recordType != "filearea" {
		t.Fatalf("enter: mode=%v type=%q", m.mode, m.recordType)
	}
	m = press(t, m, "esc")
	if m.mode != modeCategoryMenu {
		t.Fatalf("list esc: mode = %v", m.mode)
	}
	m = press(t, m, "q")
	if m.mode != modeTopMenu {
		t.Errorf("q: mode = %v", m.mode)
	}
	m = press(t, m, "4", "esc")
	if m.mode != modeTopMenu {
		t.Errorf("esc: mode = %v", m.mode)
	}
}
