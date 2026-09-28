package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// recordListKeys are the top-menu keystrokes that open each record list.
var recordListKeys = map[string][]string{
	"msgarea":    {"3", "1"},
	"filearea":   {"3", "2"},
	"conference": {"3", "3"},
	"ftn":        {"4", "1"},
	"ftnlink":    {"4", "2"},
	"qwknet":     {"5", "1"},
	"v3netleaf":  {"6", "2"},
	"v3nethub":   {"6", "3"},
	"door":       {"7"},
	"protocol":   {"8"},
	"archiver":   {"9"},
	"event":      {"0"},
	"login":      {"A"},
}

// openRecordList navigates from the top menu to recordType's list.
func openRecordList(t *testing.T, m Model, recordType string) Model {
	t.Helper()
	m = press(t, m, recordListKeys[recordType]...)
	if m.mode != modeRecordList || m.recordType != recordType {
		t.Fatalf("mode/type = %v/%q, want list of %s", m.mode, m.recordType, recordType)
	}
	return m
}

// gotoField moves the record-edit cursor to the field labelled label.
func gotoField(t *testing.T, m Model, label string) Model {
	t.Helper()
	for i := 0; m.recordFields[m.editField].Label != label; i++ {
		if i > len(m.recordFields) {
			t.Fatalf("no field %q", label)
		}
		m = press(t, m, "down")
	}
	return m
}

// saveAndQuit backs out to the top menu (accepting any save prompt on the
// way), then exits through the save-changes dialog, failing unless the
// editor quits.
func saveAndQuit(t *testing.T, m Model) {
	t.Helper()
	for i := 0; m.mode != modeTopMenu; i++ {
		if i > 8 {
			t.Fatalf("stuck in mode %v backing out", m.mode)
		}
		if m.mode == modeNavSaveConfirm {
			m = press(t, m, "y")
			continue
		}
		m = press(t, m, "esc")
	}
	m = press(t, m, "esc")
	res, cmd := m.Update(keyMsg("y"))
	if !isQuit(cmd) {
		t.Fatalf("exit did not quit (mode %v, msg %q)", asModel(t, res).mode, asModel(t, res).message)
	}
}

// seedFTN gives the model one echomail network so link records have a home.
func seedFTN(m *Model) {
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"fidonet": {OwnAddress: "1:2/3", InternalTosserEnabled: true},
	}
}

// TestRecords_InsertEditPersist pins, for each list-style record type, that
// Insert adds a record, editing a string field on it changes the record, and
// the change survives a save and reload.
func TestRecords_InsertEditPersist(t *testing.T) {
	cases := []struct {
		recordType string
		seed       func(*Model)
		label      string
		value      string
		saved      func(allConfigs) bool
	}{
		{"msgarea", nil, "Name", "Chat Area", func(ac allConfigs) bool {
			return len(ac.MsgAreas) == 1 && ac.MsgAreas[0].Name == "Chat Area"
		}},
		{"filearea", nil, "Name", "Uploads", func(ac allConfigs) bool {
			return len(ac.FileAreas) == 1 && ac.FileAreas[0].Name == "Uploads"
		}},
		{"conference", nil, "Name", "Main Conf", func(ac allConfigs) bool {
			return len(ac.Conferences) == 1 && ac.Conferences[0].Name == "Main Conf"
		}},
		{"door", nil, "Name", "Tetris", func(ac allConfigs) bool {
			return len(ac.Doors) == 1 && ac.Doors["NEWDOOR1"].Name == "Tetris"
		}},
		{"event", nil, "Name", "Nightly", func(ac allConfigs) bool {
			return len(ac.Events.Events) == 1 && ac.Events.Events[0].Name == "Nightly"
		}},
		{"ftn", nil, "Network Name", "fsxnet", func(ac allConfigs) bool {
			_, ok := ac.FTN.Networks["fsxnet"]
			return ok && len(ac.FTN.Networks) == 1
		}},
		{"ftnlink", seedFTN, "Address", "1:2/100", func(ac allConfigs) bool {
			l := ac.FTN.Networks["fidonet"].Links
			return len(l) == 1 && l[0].Address == "1:2/100"
		}},
		{"protocol", nil, "Name", "ZedZap", func(ac allConfigs) bool {
			p := ac.Protocols
			return len(p) > 0 && p[len(p)-1].Name == "ZedZap"
		}},
		{"archiver", nil, "Name", "Squeeze", func(ac allConfigs) bool {
			a := ac.Archivers.Archivers
			return len(a) > 0 && a[len(a)-1].Name == "Squeeze"
		}},
		{"login", nil, "Data", "WELCOME", func(ac allConfigs) bool {
			l := ac.LoginSeq
			return len(l) > 0 && l[len(l)-1].Data == "WELCOME"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.recordType, func(t *testing.T) {
			m, dir := newDiskModel(t)
			if tc.seed != nil {
				tc.seed(&m)
			}
			m = openRecordList(t, m, tc.recordType)
			before := m.recordCount()
			m = press(t, m, "i")
			if m.recordCount() != before+1 || !m.dirty {
				t.Fatalf("insert: count %d -> %d dirty=%v", before, m.recordCount(), m.dirty)
			}
			wantScreen(t, m, m.recordTypeTitle())
			if m.mode == modeRecordList {
				m = press(t, m, "enter")
			}
			if m.mode != modeRecordEdit || m.recordEditIdx != m.recordCursor {
				t.Fatalf("mode=%v edit=%d cursor=%d", m.mode, m.recordEditIdx, m.recordCursor)
			}
			m = gotoField(t, m, tc.label)
			m = press(t, m, "enter")
			if m.mode != modeRecordField {
				t.Fatalf("mode = %v, want field edit", m.mode)
			}
			m = press(t, replaceText(t, m, tc.value), "enter")
			if m.mode != modeRecordEdit {
				t.Fatalf("apply failed: %q", m.message)
			}
			wantScreen(t, m, tc.value)
			saveAndQuit(t, m)
			if ac := reloadConfigs(t, dir); !tc.saved(ac) {
				t.Errorf("edit to %s %q not persisted", tc.label, tc.value)
			}
		})
	}
}

// TestRecords_EditSeededV3NetRecords pins field edits on V3Net subscription
// and hosted-network records, whose lists offer a save before leaving.
func TestRecords_EditSeededV3NetRecords(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{HubURL: "https://hub.example", Network: "felnet", PollInterval: "5m"}}
	m.configs.V3Net.Hub.Networks = []config.V3NetHubNetwork{{Name: "homenet", Description: "Home"}}

	m = press(t, openRecordList(t, m, "v3netleaf"), "enter")
	wantScreen(t, m, "https://hub.example")
	m = press(t, gotoField(t, m, "Origin"), "enter")
	m = press(t, typeText(t, m, "Fel Origin"), "enter")
	m = press(t, gotoField(t, m, "Network"), "enter")
	m = press(t, replaceText(t, m, ""), "enter")
	if m.mode != modeRecordField || m.message != "Invalid: network name cannot be empty" {
		t.Fatalf("empty network accepted: mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, m, "esc")
	m = press(t, gotoField(t, m, "Newscan Default"), "space")

	// Leaving asks to save; Yes writes and returns to the list.
	m = press(t, m, "esc")
	if m.mode != modeNavSaveConfirm {
		t.Fatalf("mode = %v, want save prompt", m.mode)
	}
	m = press(t, m, "right", "left", "enter")
	if m.mode != modeRecordList || m.dirty {
		t.Fatalf("mode=%v dirty=%v", m.mode, m.dirty)
	}
	ac := reloadConfigs(t, dir)
	if l := ac.V3Net.Leaves[0]; l.Origin != "Fel Origin" || l.AutoJoinEnabled() {
		t.Errorf("leaf = %+v autojoin=%v", l, l.AutoJoinEnabled())
	}

	m = press(t, m, "esc", "esc")
	m = press(t, openRecordList(t, m, "v3nethub"), "enter")
	m = press(t, gotoField(t, m, "Description"), "enter")
	m = press(t, replaceText(t, m, "Home Network"), "enter")
	m = press(t, gotoField(t, m, "Network Name"), "enter")
	m = press(t, replaceText(t, m, ""), "enter")
	if !strings.Contains(m.message, "cannot be empty") {
		t.Errorf("empty hub name msg = %q", m.message)
	}
	m = press(t, m, "esc", "esc")
	if m.mode != modeNavSaveConfirm {
		t.Fatalf("mode = %v", m.mode)
	}
	m = press(t, m, "S") // unrelated key is ignored
	m = press(t, m, "n")
	if m.mode != modeRecordList || !m.dirty {
		t.Fatalf("no: mode=%v dirty=%v", m.mode, m.dirty)
	}
	// S on the list saves.
	m = press(t, m, "S")
	if m.dirty {
		t.Fatalf("S did not save: %q", m.message)
	}
	if ac := reloadConfigs(t, dir); ac.V3Net.Hub.Networks[0].Description != "Home Network" {
		t.Errorf("hub network = %+v", ac.V3Net.Hub.Networks[0])
	}
}

// TestRecords_DeleteConfirm pins record deletion for every list type: No and
// Escape keep the record, Yes removes the one under the cursor.
func TestRecords_DeleteConfirm(t *testing.T) {
	for rt := range recordListKeys {
		t.Run(rt, func(t *testing.T) {
			m, _ := newDiskModel(t)
			seedFTN(&m)
			m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{"dovenet": {}}
			m.recordType = rt
			m.insertRecord()
			m.insertRecord()
			m.recordType = ""
			m = openRecordList(t, m, rt)
			total := m.recordCount()
			if total == 0 {
				t.Fatal("no records seeded")
			}
			m = press(t, m, "end", "d")
			if m.mode != modeDeleteConfirm {
				t.Fatalf("mode = %v", m.mode)
			}
			wantScreen(t, m, "Delete Record")
			m = press(t, m, "n", "d", "esc", "delete", "left", "right", "enter")
			if m.recordCount() != total || m.mode != modeRecordList {
				t.Fatalf("declined delete changed count %d -> %d", total, m.recordCount())
			}
			m = press(t, m, "d", "y")
			if m.recordCount() != total-1 || !m.dirty || m.mode != modeRecordList {
				t.Errorf("count=%d want %d dirty=%v mode=%v", m.recordCount(), total-1, m.dirty, m.mode)
			}
			if m.recordCursor != m.recordCount()-1 && m.recordCount() > 0 {
				t.Errorf("cursor = %d after deleting last row", m.recordCursor)
			}
		})
	}
}

// TestRecords_ListNavigationAndScroll pins cursor and scroll behaviour on a
// list longer than the viewport.
func TestRecords_ListNavigationAndScroll(t *testing.T) {
	m, _ := newDiskModel(t)
	m.recordType = "event"
	for i := 0; i < 30; i++ {
		m.insertRecord()
	}
	m.recordType = ""
	m = openRecordList(t, m, "event")
	m = press(t, m, "pgdown")
	if m.recordCursor != 13 {
		t.Errorf("pgdown cursor = %d, want 13", m.recordCursor)
	}
	m = press(t, m, "pgdown", "pgdown", "pgdown")
	if m.recordCursor != 29 || m.recordScroll != 17 {
		t.Errorf("cursor=%d scroll=%d, want 29/17", m.recordCursor, m.recordScroll)
	}
	wantScreen(t, m, "New Event 30")
	m = press(t, m, "pgup")
	if m.recordCursor != 16 {
		t.Errorf("pgup cursor = %d", m.recordCursor)
	}
	m = press(t, m, "home", "up", "pgup")
	if m.recordCursor != 0 || m.recordScroll != 0 {
		t.Errorf("home: cursor=%d scroll=%d", m.recordCursor, m.recordScroll)
	}
	m = press(t, m, "down", "end")
	if m.recordCursor != 29 {
		t.Errorf("end cursor = %d", m.recordCursor)
	}
	// Enter then PgUp/PgDn page between records.
	m = press(t, m, "enter", "pgdown")
	if m.recordEditIdx != 29 {
		t.Errorf("pgdown past last: idx = %d", m.recordEditIdx)
	}
	m = press(t, m, "pgup", "pgup")
	if m.recordEditIdx != 27 {
		t.Errorf("pgup: idx = %d", m.recordEditIdx)
	}
	m = press(t, m, "esc")
	if m.mode != modeRecordList {
		t.Errorf("esc: mode = %v", m.mode)
	}
}

// TestRecords_EmptyListHints pins the empty-state hint for each list, and
// that Enter, D and P do nothing on an empty list.
func TestRecords_EmptyListHints(t *testing.T) {
	for rt := range recordListKeys {
		if rt == "protocol" || rt == "archiver" {
			continue // seeded with built-in defaults when their file is absent
		}
		t.Run(rt, func(t *testing.T) {
			m, _ := newDiskModel(t)
			if rt == "ftnlink" {
				seedFTN(&m)
			}
			m = openRecordList(t, m, rt)
			wantScreen(t, m, m.recordTypeTitle(), m.emptyRecordListHint()[:20])
			m = press(t, m, "enter", "d", "p", "pgdown", "end")
			if m.mode != modeRecordList || m.recordCursor != 0 {
				t.Errorf("mode=%v cursor=%d", m.mode, m.recordCursor)
			}
		})
	}
}

// TestRecords_FieldValidation pins record-field editing rules: integer fields
// drop letters and reject out-of-range values, Escape discards, Up applies,
// and Y/N fields flip with Enter and Space.
func TestRecords_FieldValidation(t *testing.T) {
	m, _ := newDiskModel(t)
	m = openRecordList(t, m, "event")
	m = press(t, m, "i", "enter")
	m = press(t, gotoField(t, m, "Timeout (sec)"), "enter")
	m.textInput.SetValue("")
	m = typeText(t, m, "1a2")
	if m.textInput.Value() != "12" {
		t.Errorf("letters accepted: %q", m.textInput.Value())
	}
	m = press(t, replaceText(t, m, "-5"), "enter")
	if m.mode != modeRecordField || !strings.HasPrefix(m.message, "Invalid: must be") {
		t.Fatalf("range: mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, m, "up")
	if m.mode != modeRecordField {
		t.Fatal("up applied an invalid value")
	}
	m = press(t, replaceText(t, m, "x"), "enter")
	if m.message != "Invalid: not a number" {
		t.Errorf("nan msg = %q", m.message)
	}
	m = press(t, replaceText(t, m, "45"), "up")
	if m.configs.Events.Events[0].TimeoutSeconds != 45 || m.recordFields[m.editField].Label == "Timeout (sec)" {
		t.Errorf("timeout=%d field=%q", m.configs.Events.Events[0].TimeoutSeconds, m.recordFields[m.editField].Label)
	}

	m = press(t, gotoField(t, m, "Name"), "enter")
	m = press(t, replaceText(t, m, "Discard"), "esc")
	if m.configs.Events.Events[0].Name == "Discard" || m.mode != modeRecordEdit {
		t.Errorf("esc applied: name=%q mode=%v", m.configs.Events.Events[0].Name, m.mode)
	}

	m = gotoField(t, m, "Enabled")
	m = press(t, m, "enter")
	if !m.configs.Events.Events[0].Enabled {
		t.Error("enter did not toggle Enabled")
	}
	m = press(t, m, "space")
	if m.configs.Events.Events[0].Enabled {
		t.Error("space did not toggle Enabled back")
	}
	// Space on a text field does nothing.
	m = press(t, gotoField(t, m, "Name"), "space")
	if m.mode != modeRecordEdit {
		t.Errorf("space on text field: mode = %v", m.mode)
	}
}

// TestRecords_LookupPickerInRecord pins that choosing from a record field's
// picker stores the value and rebuilds the field list.
func TestRecords_LookupPickerInRecord(t *testing.T) {
	m, _ := newDiskModel(t)
	m = openRecordList(t, m, "msgarea")
	m = press(t, m, "i", "enter")
	m = press(t, gotoField(t, m, "Area Type"), "enter")
	if m.mode != modeLookupPicker || m.pickerReturnMode != modeRecordEdit {
		t.Fatalf("mode = %v", m.mode)
	}
	if m.pickerItems[m.pickerCursor].Value != "local" {
		t.Errorf("preselected = %+v, want local", m.pickerItems[m.pickerCursor])
	}
	wantScreen(t, m, "local")
	m = press(t, m, "esc")
	if m.mode != modeRecordEdit || m.configs.MsgAreas[0].AreaType != "local" {
		t.Fatalf("esc: mode=%v type=%q", m.mode, m.configs.MsgAreas[0].AreaType)
	}
	var echoIdx int
	for i, it := range m.pickerItems {
		if it.Value == "echomail" {
			echoIdx = i
		}
	}
	m = press(t, m, "enter", "home")
	for i := 0; i < echoIdx; i++ {
		m = press(t, m, "down")
	}
	m = press(t, m, "enter")
	if m.configs.MsgAreas[0].AreaType != "echomail" || !m.dirty || m.mode != modeRecordEdit {
		t.Errorf("type=%q dirty=%v mode=%v", m.configs.MsgAreas[0].AreaType, m.dirty, m.mode)
	}
}

// TestRecords_QWKGlobalSettings pins that G on the QWK network list opens the
// global settings record and its edits persist.
func TestRecords_QWKGlobalSettings(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{"dovenet": {HubID: "VERT", Enabled: true}}
	m = openRecordList(t, m, "qwknet")
	wantScreen(t, m, "dovenet", "VERT")
	m = press(t, m, "G")
	if m.mode != modeRecordEdit || m.recordEditIdx != -1 {
		t.Fatalf("mode=%v idx=%d", m.mode, m.recordEditIdx)
	}
	m = press(t, gotoField(t, m, "Inbound Path"), "enter")
	m = press(t, replaceText(t, m, "data/qwk/in"), "enter")
	saveAndQuit(t, m)
	ac := reloadConfigs(t, dir)
	if ac.QWKNet.InboundPath != "data/qwk/in" || ac.QWKNet.Networks["dovenet"].HubID != "VERT" {
		t.Errorf("qwknet = %+v", ac.QWKNet)
	}
}

// TestRecords_FTNGlobalSettings pins that G on the echomail network list
// opens the FTN global settings.
func TestRecords_FTNGlobalSettings(t *testing.T) {
	m, _ := newDiskModel(t)
	seedFTN(&m)
	m = openRecordList(t, m, "ftn")
	wantScreen(t, m, "fidonet", "1:2/3")
	m = press(t, m, "G")
	if m.mode != modeRecordEdit || m.recordEditIdx != -1 || len(m.recordFields) == 0 {
		t.Fatalf("mode=%v idx=%d fields=%d", m.mode, m.recordEditIdx, len(m.recordFields))
	}
	// Other types ignore G.
	m = press(t, m, "esc", "esc", "2", "G")
	if m.mode != modeRecordList {
		t.Errorf("G on links: mode = %v", m.mode)
	}
}
