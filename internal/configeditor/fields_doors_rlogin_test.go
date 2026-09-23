package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// An rlogin door must survive being edited through the config editor's own
// field closures and saved: values typed into the editor have to reach the
// struct, and the struct has to reach the file.
//
// The earlier version of this test called saveDoors on a freshly loaded record
// without touching a single Get or Set, so a field wired to the wrong struct
// member would have round-tripped perfectly and told us nothing.
func TestRLoginDoorRoundTripsThroughEditorFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doors.json"),
		[]byte(`[{"code":"DOORSRV","name":"Door Server","type":"rlogin"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	doors, err := config.LoadDoors(filepath.Join(dir, "doors.json"))
	if err != nil {
		t.Fatalf("LoadDoors: %v", err)
	}

	m := &Model{configs: &allConfigs{Doors: doors}, configPath: dir, recordEditIdx: 0}

	// Type a value into each field, the way a sysop would.
	typed := map[string]string{
		"Host":            "doors.example.com",
		"Port":            "3513",
		"Client User":     "sekrit",
		"Server User":     "[V3]{USERHANDLE}",
		"Terminal Type":   "xtrn=LORD",
		"Connect Timeout": "20",
		"Disconnect Key":  "^B",
	}
	byLabel := map[string]fieldDef{}
	for _, f := range m.fieldsDoor() {
		byLabel[f.Label] = f
	}
	for label, val := range typed {
		f, ok := byLabel[label]
		if !ok {
			t.Fatalf("the config editor offers no %q field for an rlogin door", label)
		}
		if err := f.Set(val); err != nil {
			t.Fatalf("setting %s=%q: %v", label, val, err)
		}
	}

	// What was typed must read back from the field, not just be accepted.
	for _, f := range m.fieldsDoor() {
		if want, ok := typed[f.Label]; ok {
			if got := f.Get(); got != want {
				t.Errorf("%s reads back as %q, want %q", f.Label, got, want)
			}
		}
	}

	if err := saveDoors(dir, m.configs.Doors); err != nil {
		t.Fatalf("saveDoors: %v", err)
	}
	back, err := config.LoadDoors(filepath.Join(dir, "doors.json"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	d := back["DOORSRV"]
	for _, tc := range []struct{ name, got, want string }{
		{"host", d.Host, "doors.example.com"},
		{"client_username", d.ClientUsername, "sekrit"},
		{"server_username", d.ServerUsername, "[V3]{USERHANDLE}"},
		{"terminal_type", d.TerminalType, "xtrn=LORD"},
		{"disconnect_key", d.DisconnectKey, "^B"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if d.Port != 3513 {
		t.Errorf("port = %d, want 3513", d.Port)
	}
	if d.ConnectTimeout != 20 {
		t.Errorf("connect_timeout = %d, want 20", d.ConnectTimeout)
	}

	// Fields belonging to a local process must not be offered for a door that
	// is only a socket to somewhere else.
	for _, unwanted := range []string{"I/O Mode", "Raw Terminal", "Use Shell", "Env Vars", "Dropfile Type"} {
		if _, offered := byLabel[unwanted]; offered {
			t.Errorf("config editor offers %q for an rlogin door, which runs no local process", unwanted)
		}
	}
}

// A door that cannot run must be pointed out while the sysop is still looking
// at the record, one keystroke from the field that is wrong.
func TestDoorExitWarning(t *testing.T) {
	for name, tc := range map[string]struct {
		door config.DoorConfig
		ok   bool
		warn bool
	}{
		"no host":           {config.DoorConfig{Code: "A", Type: "rlogin"}, true, true},
		"blank host":        {config.DoorConfig{Code: "A", Type: "rlogin", Host: "  "}, true, true},
		"port out of range": {config.DoorConfig{Code: "A", Type: "rlogin", Host: "h", Port: 70000}, true, true},
		"negative timeout":  {config.DoorConfig{Code: "A", Type: "rlogin", Host: "h", ConnectTimeout: -1}, true, true},
		"bad key":           {config.DoorConfig{Code: "A", Type: "rlogin", Host: "h", DisconnectKey: "banana"}, true, true},
		"workable":          {config.DoorConfig{Code: "A", Type: "rlogin", Host: "h"}, true, false},
		"fully specified":   {config.DoorConfig{Code: "A", Type: "rlogin", Host: "h", Port: 3513, DisconnectKey: "^B"}, true, false},
		// Other types are not this check's business, and a native door with no
		// command has always been allowed.
		"native":     {config.DoorConfig{Code: "A"}, true, false},
		"dos":        {config.DoorConfig{Code: "A", IsDOS: true}, true, false},
		"v3 script":  {config.DoorConfig{Code: "A", Type: "v3_script"}, true, false},
		"not a door": {config.DoorConfig{}, false, false},
	} {
		got := doorExitWarning(tc.door, tc.ok)
		if tc.warn && got == "" {
			t.Errorf("%s: expected a warning", name)
		}
		if !tc.warn && got != "" {
			t.Errorf("%s: unexpected warning %q", name, got)
		}
	}
}

// The warning has to name the door so a sysop with several knows which one.
func TestDoorExitWarningNamesTheDoor(t *testing.T) {
	got := doorExitWarning(config.DoorConfig{Code: "DOORSRV", Type: "rlogin"}, true)
	if !strings.Contains(got, "DOORSRV") {
		t.Errorf("warning = %q, want it to name the door", got)
	}
	if !strings.Contains(got, "host") {
		t.Errorf("warning = %q, want it to say what is missing", got)
	}
}

// Warning, not blocking: the save still goes through, and it must go through
// for every other subsystem too. A door typo that stopped a sysop persisting
// unrelated FTN or V3Net work would be a worse bug than the one being caught.
func TestSaveAllIsNotBlockedByAnUnrunnableDoor(t *testing.T) {
	dir := t.TempDir()
	m := &Model{
		configPath: dir,
		dirty:      true,
		configs: &allConfigs{
			Server: config.ServerConfig{BoardName: "Test BBS"},
			Doors: map[string]config.DoorConfig{
				"BROKEN": {Code: "BROKEN", Type: "rlogin"}, // no host
			},
		},
	}
	if !m.saveAll() {
		t.Fatalf("saveAll refused everything over one door: %s", m.message)
	}
	if _, err := os.Stat(filepath.Join(dir, "doors.json")); err != nil {
		t.Errorf("doors.json was not written: %v", err)
	}
	if m.dirty {
		t.Error("model still marked dirty after a successful save")
	}
}

// saveAll reporting failure is what keeps the quit and navigate paths from
// throwing a session's work away, and that matters for any save error -- a full
// disk, a permission problem -- not just doors.
func TestSaveAllReportsFailureOnAnUnwritableConfigPath(t *testing.T) {
	m := &Model{
		configPath: filepath.Join(t.TempDir(), "does", "not", "exist"),
		dirty:      true,
		configs:    &allConfigs{Server: config.ServerConfig{BoardName: "Test BBS"}},
	}
	if m.saveAll() {
		t.Error("saveAll reported success writing to a path that does not exist")
	}
	if !strings.HasPrefix(m.message, "SAVE ERROR") {
		t.Errorf("message = %q, want it to start with SAVE ERROR", m.message)
	}
	if !m.dirty {
		t.Error("model was marked clean despite nothing being written")
	}
}

// Escape is not the only way out of a record: PageUp and PageDown move to
// another one. Keying the warning to Escape alone would let a sysop page
// straight past a broken door and never be told.
func TestEveryRecordExitWarnsAboutAnUnrunnableDoor(t *testing.T) {
	doors := map[string]config.DoorConfig{
		"AAABROKEN": {Code: "AAABROKEN", Type: "rlogin"},                        // no host
		"BBBOK":     {Code: "BBBOK", Type: "rlogin", Host: "doors.example.com"}, // fine
	}
	for name, key := range map[string]tea.KeyMsg{
		"escape":   {Type: tea.KeyEscape},
		"pagedown": {Type: tea.KeyPgDown},
	} {
		m := Model{
			recordType:    "door",
			recordEditIdx: 0, // AAABROKEN sorts first
			configs:       &allConfigs{Doors: doors},
		}
		m.recordFields = m.buildRecordFields()
		out, _ := m.updateRecordEdit(key)
		got := out.(Model).message
		if !strings.Contains(got, "AAABROKEN") {
			t.Errorf("%s: message = %q, want a warning naming the door being left", name, got)
		}
	}

	// Leaving the workable door must not warn, and must clear a warning left
	// over from the broken one.
	m := Model{
		recordType:    "door",
		recordEditIdx: 1, // BBBOK
		configs:       &allConfigs{Doors: doors},
		message:       `WARNING: door "AAABROKEN": an RLogin door needs a host.`,
	}
	m.recordFields = m.buildRecordFields()
	out, _ := m.updateRecordEdit(tea.KeyMsg{Type: tea.KeyPgUp})
	if got := out.(Model).message; got != "" {
		t.Errorf("message = %q, want the stale warning cleared when leaving a workable door", got)
	}
}

// The warning describes an unsaved record, so it must not claim the change is
// already on disk.
func TestDoorExitWarningDoesNotClaimTheRecordWasSaved(t *testing.T) {
	got := doorExitWarning(config.DoorConfig{Code: "DOORSRV", Type: "rlogin"}, true)
	if strings.Contains(got, "Saved as-is") {
		t.Errorf("warning = %q, but leaving a record writes nothing", got)
	}
	if !strings.Contains(got, "can still be saved") {
		t.Errorf("warning = %q, want it to say the record can still be saved", got)
	}
}
