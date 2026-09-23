package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// An rlogin door must survive a load/edit/save cycle through the config editor
// with every field intact: the fields are useless if ./config drops them.
func TestRLoginDoorRoundTripsThroughEditor(t *testing.T) {
	dir := t.TempDir()
	orig := `[{"code":"DOORSRV","name":"Door Server","type":"rlogin",` +
		`"host":"doors.example.com","port":3513,` +
		`"client_username":"sekrit","server_username":"[V3]{USERHANDLE}",` +
		`"terminal_type":"xtrn=LORD","connect_timeout":20,"disconnect_key":"^B"}]`
	if err := os.WriteFile(filepath.Join(dir, "doors.json"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	doors, err := config.LoadDoors(filepath.Join(dir, "doors.json"))
	if err != nil {
		t.Fatalf("LoadDoors: %v", err)
	}
	if err := saveDoors(dir, doors); err != nil {
		t.Fatalf("saveDoors: %v", err)
	}

	back, err := config.LoadDoors(filepath.Join(dir, "doors.json"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	d := back["DOORSRV"]
	for _, tc := range []struct{ name, got, want string }{
		{"type", d.Type, "rlogin"},
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

	// And the editor must actually offer the fields for that door.
	m := &Model{configs: &allConfigs{Doors: back}, recordEditIdx: 0}
	labels := map[string]bool{}
	for _, f := range m.fieldsDoor() {
		labels[f.Label] = true
	}
	for _, want := range []string{"Host", "Port", "Client User", "Server User", "Terminal Type", "Connect Timeout", "Disconnect Key"} {
		if !labels[want] {
			t.Errorf("config editor does not offer the %q field for an rlogin door", want)
		}
	}
	for _, unwanted := range []string{"I/O Mode", "Raw Terminal", "Use Shell", "Env Vars", "Dropfile Type"} {
		if labels[unwanted] {
			t.Errorf("config editor offers %q for an rlogin door, which has no local process", unwanted)
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
