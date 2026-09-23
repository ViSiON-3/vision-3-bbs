package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// A door the BBS could not run must be caught where a sysop can fix it, rather
// than saved and left to fail when a caller opens it.
func TestValidateDoorsRejectsUnrunnableRLoginDoors(t *testing.T) {
	for name, door := range map[string]config.DoorConfig{
		"no host":          {Code: "A", Type: "rlogin"},
		"blank host":       {Code: "A", Type: "rlogin", Host: "   "},
		"port too high":    {Code: "A", Type: "rlogin", Host: "h", Port: 70000},
		"negative port":    {Code: "A", Type: "rlogin", Host: "h", Port: -1},
		"negative timeout": {Code: "A", Type: "rlogin", Host: "h", ConnectTimeout: -5},
		"bad key":          {Code: "A", Type: "rlogin", Host: "h", DisconnectKey: "banana"},
	} {
		if err := validateDoors(map[string]config.DoorConfig{"A": door}); err == nil {
			t.Errorf("%s: expected the save to be refused", name)
		}
	}
}

func TestValidateDoorsAcceptsWorkableConfigurations(t *testing.T) {
	for name, door := range map[string]config.DoorConfig{
		"minimal rlogin":   {Code: "A", Type: "rlogin", Host: "doors.example.com"},
		"fully specified":  {Code: "A", Type: "rlogin", Host: "h", Port: 3513, ConnectTimeout: 20, DisconnectKey: "^B"},
		"disconnect off":   {Code: "A", Type: "rlogin", Host: "h", DisconnectKey: "none"},
		"port 0 means 513": {Code: "A", Type: "rlogin", Host: "h", Port: 0},
	} {
		if err := validateDoors(map[string]config.DoorConfig{"A": door}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Validation is scoped to rlogin so it can never reject a door that already
// worked. A native door with no command is nonsense too, but it has always
// saved, and making it fail now would break existing configurations on upgrade.
func TestValidateDoorsLeavesOtherTypesAlone(t *testing.T) {
	for name, door := range map[string]config.DoorConfig{
		"native with nothing set": {Code: "A"},
		"dos with nothing set":    {Code: "A", IsDOS: true},
		"script with no script":   {Code: "A", Type: "v3_script"},
		// Host-less, but not an rlogin door, so not this check's business.
		"syncjs with no script": {Code: "A", Type: "synchronet_js"},
	} {
		if err := validateDoors(map[string]config.DoorConfig{"A": door}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The message has to name the offending door, and name the same one each time:
// a map iteration would otherwise report a different record on each save.
func TestValidateDoorsNamesTheDoorDeterministically(t *testing.T) {
	doors := map[string]config.DoorConfig{
		"ZEBRA": {Code: "ZEBRA", Type: "rlogin"},
		"ALPHA": {Code: "ALPHA", Type: "rlogin"},
		"OK":    {Code: "OK", Type: "rlogin", Host: "h"},
	}
	for i := 0; i < 20; i++ {
		err := validateDoors(doors)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "ALPHA") {
			t.Fatalf("error = %v, want the first door by code named every time", err)
		}
	}
}

// saveAll must check before it writes anything. The savers commit one file at
// a time, so a record rejected partway through would leave some files updated
// and the rest stale. Calling validateDoors directly, as the other tests here
// do, would not notice the check being moved after the first saver.
func TestSaveAllWritesNothingWhenADoorIsRejected(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "config.json")
	if err := os.WriteFile(sentinel, []byte(`{"boardName":"Before"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}

	m := &Model{
		configPath: dir,
		dirty:      true,
		configs: &allConfigs{
			// A changed sentinel that the first saver would write out, and a
			// door that must stop the save before it gets there.
			Server: config.ServerConfig{BoardName: "After"},
			Doors: map[string]config.DoorConfig{
				"BROKEN": {Code: "BROKEN", Type: "rlogin"}, // no host
			},
		},
	}

	if m.saveAll() {
		t.Error("saveAll reported success for a door that cannot run")
	}
	if !strings.HasPrefix(m.message, "SAVE ERROR") {
		t.Errorf("message = %q, want it to start with SAVE ERROR", m.message)
	}
	if !strings.Contains(m.message, "BROKEN") {
		t.Errorf("message = %q, want it to name the offending door", m.message)
	}
	if m.dirty != true {
		t.Error("the model was marked clean despite nothing being written")
	}

	after, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("config.json was rewritten before the door was rejected:\n before: %s\n after:  %s", before, after)
	}
}

// The counterpart: a workable configuration still saves, so the guard above
// cannot be satisfied by refusing everything.
func TestSaveAllWritesWhenDoorsAreValid(t *testing.T) {
	dir := t.TempDir()
	m := &Model{
		configPath: dir,
		dirty:      true,
		configs: &allConfigs{
			Server: config.ServerConfig{BoardName: "Test BBS"},
			Doors: map[string]config.DoorConfig{
				"DOORSRV": {Code: "DOORSRV", Type: "rlogin", Host: "doors.example.com"},
			},
		},
	}
	if !m.saveAll() {
		t.Fatalf("saveAll refused a valid configuration: %s", m.message)
	}
	if _, err := os.Stat(filepath.Join(dir, "doors.json")); err != nil {
		t.Errorf("doors.json was not written: %v", err)
	}
	if m.dirty {
		t.Error("model still marked dirty after a successful save")
	}
}

// The FTN wizard rewrites binkd.conf on its way to saveAll, so it has to check
// the doors before it starts: otherwise a door that cannot run would leave
// binkd.conf describing a network that never reached ftn.json.
func TestFTNWizardRefusesBeforeTouchingBinkdConf(t *testing.T) {
	dir := t.TempDir()
	binkd := filepath.Join(dir, "..", "data", "ftn", "binkd.conf")
	if err := os.MkdirAll(filepath.Dir(binkd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binkd, []byte("# original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := Model{
		configPath: dir,
		configs: &allConfigs{
			Doors: map[string]config.DoorConfig{
				"BROKEN": {Code: "BROKEN", Type: "rlogin"}, // no host
			},
		},
	}
	out, _ := m.confirmFTNWizard()

	if !strings.HasPrefix(out.message, "SAVE ERROR") {
		t.Errorf("message = %q, want the wizard refused with SAVE ERROR", out.message)
	}
	got, err := os.ReadFile(binkd)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# original\n" {
		t.Errorf("binkd.conf was rewritten despite the save being refused:\n%s", got)
	}
}
