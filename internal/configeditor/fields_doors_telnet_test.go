package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// A telnet door edited through the editor's own field closures and saved:
// what is typed has to reach the struct, and the struct the file. The login
// field is the one to watch, since the editor shows it as escapes and the
// file holds the real characters.
func TestTelnetDoorRoundTripsThroughEditorFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doors.json"),
		[]byte(`[{"code":"DOORSRV","name":"Door Server","type":"telnet"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	doors, err := config.LoadDoors(filepath.Join(dir, "doors.json"))
	if err != nil {
		t.Fatalf("LoadDoors: %v", err)
	}

	m := &Model{configs: &allConfigs{Doors: doors}, configPath: dir, recordEditIdx: 0}

	typed := map[string]string{
		"Host":            "doors.example.com",
		"Port":            "2323",
		"Terminal Type":   "ANSI-BBS",
		"Send On Connect": `{USERHANDLE}\rsecret\r`,
		"Raw TCP":         "N",
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
			t.Fatalf("the config editor offers no %q field for a telnet door", label)
		}
		if err := f.Set(val); err != nil {
			t.Fatalf("setting %s=%q: %v", label, val, err)
		}
	}
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
		{"terminal_type", d.TerminalType, "ANSI-BBS"},
		{"send_on_connect", d.SendOnConnect, "{USERHANDLE}\rsecret\r"},
		{"disconnect_key", d.DisconnectKey, "^B"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if d.Port != 2323 || d.ConnectTimeout != 20 || d.RawTCP {
		t.Errorf("port %d, connect_timeout %d, raw_tcp %v", d.Port, d.ConnectTimeout, d.RawTCP)
	}

	// The rlogin handshake has no meaning over telnet, and a local process's
	// settings none at all.
	for _, unwanted := range []string{"Client User", "Server User", "I/O Mode", "Raw Terminal", "Use Shell", "Env Vars", "Dropfile Type"} {
		if _, offered := byLabel[unwanted]; offered {
			t.Errorf("config editor offers %q for a telnet door", unwanted)
		}
	}
}

func TestTelnetDoorSendOnConnectRejectsABadEscape(t *testing.T) {
	m := &Model{
		configs:       &allConfigs{Doors: map[string]config.DoorConfig{"DOORSRV": {Code: "DOORSRV", Type: "telnet"}}},
		recordEditIdx: 0,
	}
	for _, f := range m.fieldsDoor() {
		if f.Label != "Send On Connect" {
			continue
		}
		if err := f.Set(`neo\q`); err == nil {
			t.Error("a mistyped escape was accepted")
		}
		if got := m.configs.Doors["DOORSRV"].SendOnConnect; got != "" {
			t.Errorf("send_on_connect = %q after a rejected value", got)
		}
		return
	}
	t.Fatal("no Send On Connect field")
}

// A telnet door without a host is pointed out on the way out of the record,
// as an rlogin one is.
func TestDoorExitWarningCoversTelnet(t *testing.T) {
	got := doorExitWarning(config.DoorConfig{Code: "DOORSRV", Type: "telnet"}, true)
	if !strings.Contains(got, "a Telnet door needs a host") {
		t.Errorf("warning = %q, want the missing host named", got)
	}
	if got := doorExitWarning(config.DoorConfig{Code: "DOORSRV", Type: "telnet", Host: "doors.example.com"}, true); got != "" {
		t.Errorf("warning = %q for a telnet door that can run", got)
	}
}
