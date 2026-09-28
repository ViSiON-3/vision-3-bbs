package configeditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// jamExts are the four files that make up a JAM message base.
var jamExts = []string{".jhr", ".jdt", ".jdx", ".jlr"}

// newHubAreasModel returns a disk-backed model hosting network "felnet" with
// one area (fel.general) and a localhost self-leaf, opened on the network's
// record edit screen with the cursor on the "Areas" field.
func newHubAreasModel(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := newDiskModel(t)
	m.configs.V3Net.Hub.Networks = []config.V3NetHubNetwork{{Name: "felnet", Description: "Fel Net"}}
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{
		{HubURL: "http://localhost:8765", Network: "felnet", Boards: []string{"fel.general"}, PollInterval: "5m"},
		{HubURL: "https://other.example", Network: "felnet", Boards: []string{"fel.general"}, PollInterval: "5m"},
	}
	m.configs.MsgAreas = []message.MessageArea{
		{ID: 1, Position: 1, Tag: "loc.misc", Name: "Local", AreaType: "local", BasePath: "msgbases/local"},
		{ID: 2, Position: 2, Tag: "fel.general", EchoTag: "fel.general", Name: "General", AreaType: "v3net",
			Network: "felnet", BasePath: "msgbases/fel.general"},
	}
	m = press(t, m, "6", "3", "enter")
	if m.mode != modeRecordEdit || m.recordType != "v3nethub" {
		t.Fatalf("mode/type = %v/%q, want record edit of v3nethub", m.mode, m.recordType)
	}
	for i := 0; m.recordFields[m.editField].Label != "Areas"; i++ {
		if i > len(m.recordFields) {
			t.Fatal("no Areas field on hub network record")
		}
		m = press(t, m, "down")
	}
	m = press(t, m, "enter")
	if m.mode != modeV3NetHubAreas || m.hubAreaNetwork != "felnet" {
		t.Fatalf("mode/network = %v/%q, want hub areas of felnet", m.mode, m.hubAreaNetwork)
	}
	return m, dir
}

// writeJAM creates empty JAM files for base (relative to the data dir).
func writeJAM(t *testing.T, m Model, base string) {
	t.Helper()
	abs := m.resolveJAMBase(base)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ext := range jamExts {
		if err := os.WriteFile(abs+ext, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// jamCount reports how many of base's JAM files exist.
func jamCount(m Model, base string) int {
	n := 0
	for _, ext := range jamExts {
		if _, err := os.Stat(m.resolveJAMBase(base) + ext); err == nil {
			n++
		}
	}
	return n
}

// TestHubAreas_ListShowsOnlyNetworkAreas pins that the area manager lists the
// network's v3net areas and not local ones.
func TestHubAreas_ListShowsOnlyNetworkAreas(t *testing.T) {
	m, _ := newHubAreasModel(t)
	if got := m.hubNetworkAreaIndices(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("indices = %v, want [1]", got)
	}
	wantScreen(t, m, "Network Areas — felnet", "fel.general", "msgbases/fel.general")
	// Cursor keys clamp on a one-row list.
	m = press(t, m, "down", "end", "up", "home")
	if m.hubAreaCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.hubAreaCursor)
	}
}

// TestHubAreas_InsertValidatesAndPersists pins the insert form: a bad or
// duplicate tag and an empty name are refused, the local path defaults to
// msgbases/<tag>, and the saved area joins the hub seed list and self-leaf.
func TestHubAreas_InsertValidatesAndPersists(t *testing.T) {
	m, dir := newHubAreasModel(t)
	m = press(t, m, "I")
	if m.mode != modeV3NetAreaInsert {
		t.Fatalf("mode = %v, want insert", m.mode)
	}

	m = press(t, typeText(t, m, "NOPERIOD"), "enter")
	if m.hubAreaInsertStep != 0 || m.message == "" {
		t.Fatalf("bad tag accepted: step=%d msg=%q", m.hubAreaInsertStep, m.message)
	}
	m = press(t, replaceText(t, m, "fel.general"), "enter")
	if m.message != "Area tag already exists" {
		t.Fatalf("duplicate tag message = %q", m.message)
	}
	m = press(t, replaceText(t, m, "fel.chat"), "enter")
	if m.hubAreaInsertStep != 1 {
		t.Fatalf("step = %d, want 1 after valid tag", m.hubAreaInsertStep)
	}
	m = press(t, m, "enter")
	if m.message != "Area name cannot be empty" {
		t.Fatalf("empty name message = %q", m.message)
	}
	m = press(t, typeText(t, m, "Chat"), "tab")
	m = press(t, typeText(t, m, "Chatter"), "down")
	if m.hubAreaInsertStep != 3 || m.textInput.Value() != "msgbases/fel.chat" {
		t.Fatalf("step=%d path=%q, want default local path", m.hubAreaInsertStep, m.textInput.Value())
	}
	wantScreen(t, m, "Insert Area", "fel.chat", "Chat")

	// Walking back up keeps what was typed.
	m = press(t, m, "up", "shift+tab", "up")
	if m.hubAreaInsertStep != 0 || m.textInput.Value() != "fel.chat" || m.hubAreaInsertName != "Chat" {
		t.Fatalf("back-nav lost values: step=%d val=%q name=%q", m.hubAreaInsertStep, m.textInput.Value(), m.hubAreaInsertName)
	}
	m = press(t, m, "enter", "enter", "enter")
	m.textInput.SetValue("")
	m = press(t, m, "enter")
	if m.message != "Local path cannot be empty" {
		t.Fatalf("empty path message = %q", m.message)
	}
	m = press(t, typeText(t, m, "msgbases/chat"), "enter")
	if m.mode != modeV3NetHubAreas || !m.dirty {
		t.Fatalf("mode=%v dirty=%v after insert", m.mode, m.dirty)
	}
	if m.hubAreaCursor != 1 {
		t.Errorf("cursor = %d, want the new area (1)", m.hubAreaCursor)
	}

	m = press(t, m, "S")
	if m.dirty {
		t.Fatalf("S did not save: %q", m.message)
	}
	ac := reloadConfigs(t, dir)
	var got *message.MessageArea
	for i := range ac.MsgAreas {
		if ac.MsgAreas[i].Tag == "fel.chat" {
			got = &ac.MsgAreas[i]
		}
	}
	if got == nil {
		t.Fatal("fel.chat not saved to message_areas.json")
	}
	if got.Name != "Chat" || got.Description != "Chatter" || got.BasePath != "msgbases/chat" ||
		got.AreaType != "v3net" || got.Network != "felnet" {
		t.Errorf("saved area = %+v", *got)
	}
	if n := len(ac.V3Net.Hub.InitialAreas); n != 1 || ac.V3Net.Hub.InitialAreas[0].Tag != "fel.chat" {
		t.Errorf("initial areas = %+v", ac.V3Net.Hub.InitialAreas)
	}
	if b := ac.V3Net.Leaves[0].Boards; len(b) != 2 || b[1] != "fel.chat" {
		t.Errorf("self-leaf boards = %v, want fel.chat appended", b)
	}
	if b := ac.V3Net.Leaves[1].Boards; len(b) != 1 {
		t.Errorf("remote leaf boards changed: %v", b)
	}
}

// TestHubAreas_InsertEscapeDiscards pins that Escape on the insert form adds
// nothing.
func TestHubAreas_InsertEscapeDiscards(t *testing.T) {
	m, _ := newHubAreasModel(t)
	m = press(t, typeText(t, press(t, m, "I"), "fel.new"), "enter", "esc")
	if m.mode != modeV3NetHubAreas || m.dirty || len(m.configs.MsgAreas) != 2 {
		t.Errorf("mode=%v dirty=%v areas=%d after cancel", m.mode, m.dirty, len(m.configs.MsgAreas))
	}
}

// TestHubAreas_EditRenamesTagAndMovesJAM pins the edit form: the tag change
// is applied to the area and the self-leaf, and confirming the JAM prompt
// moves the message base files to the new path.
func TestHubAreas_EditRenamesTagAndMovesJAM(t *testing.T) {
	m, _ := newHubAreasModel(t)
	writeJAM(t, m, "msgbases/fel.general")

	m = press(t, m, "E")
	if m.mode != modeV3NetAreaRename || m.textInput.Value() != "fel.general" {
		t.Fatalf("mode=%v val=%q, want edit form prefilled", m.mode, m.textInput.Value())
	}
	m = press(t, replaceText(t, m, "Bad Tag"), "enter")
	if m.hubAreaEditStep != 0 {
		t.Fatal("invalid tag accepted")
	}
	m = press(t, replaceText(t, m, "loc.misc"), "enter")
	if m.message != "Area tag already exists" {
		t.Fatalf("duplicate tag message = %q", m.message)
	}
	m = press(t, replaceText(t, m, "fel.main"), "enter")
	m.textInput.SetValue("")
	m = press(t, m, "enter")
	if m.message != "Area name cannot be empty" {
		t.Fatalf("empty name message = %q", m.message)
	}
	m = press(t, replaceText(t, m, "Main"), "tab", "down")
	// Back up through the form and forward again.
	m = press(t, m, "up", "up", "shift+tab", "tab", "tab", "tab")
	if m.hubAreaEditStep != 3 {
		t.Fatalf("step = %d, want 3", m.hubAreaEditStep)
	}
	m.textInput.SetValue("")
	m = press(t, m, "enter")
	if m.message != "Local path cannot be empty" {
		t.Fatalf("empty path message = %q", m.message)
	}
	m = press(t, typeText(t, m, "msgbases/new/fel.main"), "enter")
	if m.mode != modeV3NetAreaRenameJAM {
		t.Fatalf("mode = %v, want JAM rename prompt", m.mode)
	}
	wantScreen(t, m, "Rename JAM Files")
	m = press(t, m, "left", "right", "y")
	if m.mode != modeV3NetHubAreas {
		t.Fatalf("mode = %v after JAM rename", m.mode)
	}

	a := m.configs.MsgAreas[1]
	if a.Tag != "fel.main" || a.EchoTag != "fel.main" || a.Name != "Main" || a.BasePath != "msgbases/new/fel.main" {
		t.Errorf("edited area = %+v", a)
	}
	if b := m.configs.V3Net.Leaves[0].Boards; b[0] != "fel.main" {
		t.Errorf("self-leaf boards = %v, want renamed tag", b)
	}
	if b := m.configs.V3Net.Leaves[1].Boards; b[0] != "fel.general" {
		t.Errorf("remote leaf boards = %v, want untouched", b)
	}
	if jamCount(m, "msgbases/fel.general") != 0 || jamCount(m, "msgbases/new/fel.main") != 4 {
		t.Errorf("JAM files not moved: old=%d new=%d",
			jamCount(m, "msgbases/fel.general"), jamCount(m, "msgbases/new/fel.main"))
	}
}

// TestHubAreas_EditDeclineJAMKeepsFiles pins that answering No to the JAM
// rename prompt updates the config but leaves the files where they were.
func TestHubAreas_EditDeclineJAMKeepsFiles(t *testing.T) {
	for _, answer := range []string{"n", "esc", "enter"} {
		t.Run(answer, func(t *testing.T) {
			m, _ := newHubAreasModel(t)
			writeJAM(t, m, "msgbases/fel.general")
			m = press(t, m, "E", "enter", "enter", "enter")
			m = press(t, replaceText(t, m, "msgbases/moved"), "enter")
			if m.mode != modeV3NetAreaRenameJAM {
				t.Fatalf("mode = %v, want JAM prompt", m.mode)
			}
			if answer == "enter" {
				m = press(t, m, "left") // move off the default Yes
			}
			m = press(t, m, answer)
			if m.mode != modeV3NetHubAreas || m.configs.MsgAreas[1].BasePath != "msgbases/moved" {
				t.Fatalf("mode=%v base=%q", m.mode, m.configs.MsgAreas[1].BasePath)
			}
			if jamCount(m, "msgbases/fel.general") != 4 {
				t.Error("JAM files moved despite declining")
			}
		})
	}
}

// TestHubAreas_EditEscapeDiscards pins that Escape leaves the area unchanged.
func TestHubAreas_EditEscapeDiscards(t *testing.T) {
	m, _ := newHubAreasModel(t)
	m = press(t, replaceText(t, press(t, m, "E"), "fel.zzz"), "enter", "esc")
	if m.mode != modeV3NetHubAreas || m.configs.MsgAreas[1].Tag != "fel.general" || m.dirty {
		t.Errorf("mode=%v tag=%q dirty=%v", m.mode, m.configs.MsgAreas[1].Tag, m.dirty)
	}
}

// TestHubAreas_DeleteConfirmAndJAMCleanup pins the delete flow: No keeps the
// area, Yes removes it, and the follow-up prompt deletes its JAM files.
func TestHubAreas_DeleteConfirmAndJAMCleanup(t *testing.T) {
	m, _ := newHubAreasModel(t)
	writeJAM(t, m, "msgbases/fel.general")

	m = press(t, m, "D")
	if m.mode != modeV3NetAreaDeleteConfirm || m.confirmYes {
		t.Fatalf("mode=%v yes=%v, want delete confirm defaulting to No", m.mode, m.confirmYes)
	}
	wantScreen(t, m, "Remove Area")
	m = press(t, m, "n")
	if len(m.configs.MsgAreas) != 2 {
		t.Fatal("No removed the area")
	}
	m = press(t, m, "D", "esc")
	if len(m.configs.MsgAreas) != 2 {
		t.Fatal("Escape removed the area")
	}

	m = press(t, m, "D", "right", "enter")
	if len(m.configs.MsgAreas) != 1 || !m.dirty {
		t.Fatalf("areas=%d dirty=%v after delete", len(m.configs.MsgAreas), m.dirty)
	}
	if m.mode != modeV3NetAreaDeleteJAM {
		t.Fatalf("mode = %v, want JAM delete prompt", m.mode)
	}
	wantScreen(t, m, "Delete JAM Files")
	m = press(t, m, "y")
	if m.mode != modeV3NetHubAreas || jamCount(m, "msgbases/fel.general") != 0 {
		t.Errorf("mode=%v remaining JAM=%d", m.mode, jamCount(m, "msgbases/fel.general"))
	}
	// Nothing left: D and E are no-ops.
	m = press(t, m, "D", "E")
	if m.mode != modeV3NetHubAreas {
		t.Errorf("mode = %v on empty list", m.mode)
	}
}

// TestHubAreas_DeleteKeepJAM pins that declining JAM deletion keeps the files
// and that an area with no JAM files skips the prompt.
func TestHubAreas_DeleteKeepJAM(t *testing.T) {
	m, _ := newHubAreasModel(t)
	writeJAM(t, m, "msgbases/fel.general")
	m = press(t, m, "D", "y", "n")
	if m.mode != modeV3NetHubAreas || jamCount(m, "msgbases/fel.general") != 4 {
		t.Errorf("mode=%v JAM=%d, want files kept", m.mode, jamCount(m, "msgbases/fel.general"))
	}

	m2, _ := newHubAreasModel(t)
	m2 = press(t, m2, "D", "y")
	if m2.mode != modeV3NetHubAreas || len(m2.configs.MsgAreas) != 1 {
		t.Errorf("mode=%v areas=%d, want direct return without JAM prompt", m2.mode, len(m2.configs.MsgAreas))
	}
}

// TestHubAreas_EscapeOffersSave pins that leaving the manager with unsaved
// changes asks first, and that No leaves disk untouched.
func TestHubAreas_EscapeOffersSave(t *testing.T) {
	m, dir := newHubAreasModel(t)
	m = press(t, m, "D", "y", "esc")
	if m.mode != modeNavSaveConfirm {
		t.Fatalf("mode = %v, want save prompt", m.mode)
	}
	m = press(t, m, "esc")
	if m.mode != modeV3NetHubAreas {
		t.Fatalf("esc on prompt: mode = %v", m.mode)
	}
	m = press(t, m, "Q", "n")
	if m.mode != modeRecordEdit {
		t.Fatalf("mode = %v, want record edit", m.mode)
	}
	if _, err := os.Stat(filepath.Join(dir, "message_areas.json")); !os.IsNotExist(err) {
		t.Errorf("declined save still wrote message_areas.json (err=%v)", err)
	}

	// Yes writes, then continues.
	m = press(t, m, "down", "down", "down")
	m.mode = modeV3NetHubAreas
	m = press(t, m, "esc", "y")
	if m.mode != modeRecordEdit || m.dirty {
		t.Fatalf("mode=%v dirty=%v after save-and-continue", m.mode, m.dirty)
	}
	if ac := reloadConfigs(t, dir); len(ac.MsgAreas) != 1 {
		t.Errorf("saved areas = %d, want 1", len(ac.MsgAreas))
	}
}
