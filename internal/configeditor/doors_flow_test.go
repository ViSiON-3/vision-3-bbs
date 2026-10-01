package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// setRecField opens the record field label, replaces its text with val and
// presses Enter, failing if the value is refused.
func setRecField(t *testing.T, m Model, label, val string) Model {
	t.Helper()
	m = press(t, gotoField(t, m, label), "enter")
	m = press(t, replaceText(t, m, val), "enter")
	if m.mode != modeRecordEdit {
		t.Fatalf("%s=%q refused: %q", label, val, m.message)
	}
	return m
}

// pickRecField opens the lookup field label and chooses the item whose
// value is val.
func pickRecField(t *testing.T, m Model, label, val string) Model {
	t.Helper()
	m = press(t, gotoField(t, m, label), "enter")
	if m.mode != modeLookupPicker {
		t.Fatalf("%s: mode = %v, want picker", label, m.mode)
	}
	m = press(t, m, "home")
	for m.pickerItems[m.pickerCursor].Value != val {
		if m.pickerCursor == len(m.pickerItems)-1 {
			t.Fatalf("%s has no choice %q", label, val)
		}
		m = press(t, m, "down")
	}
	return press(t, m, "enter")
}

// newDoorRecord opens Door Programs, inserts a door and opens it.
func newDoorRecord(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := newDiskModel(t)
	m = press(t, openRecordList(t, m, "door"), "i", "enter")
	if m.mode != modeRecordEdit {
		t.Fatalf("mode = %v", m.mode)
	}
	return m, dir
}

// savedDoor saves, reloads and returns the single door in doors.json.
func savedDoor(t *testing.T, m Model, dir string) config.DoorConfig {
	t.Helper()
	saveAndQuit(t, m)
	ac := reloadConfigs(t, dir)
	if len(ac.Doors) != 1 {
		t.Fatalf("doors = %v", ac.Doors)
	}
	for _, d := range ac.Doors {
		return d
	}
	return config.DoorConfig{}
}

// TestDoorFields_SyncJS pins the Synchronet JS door fields and their CSV
// parsing, and that the type change swaps in the JS-only fields.
func TestDoorFields_SyncJS(t *testing.T) {
	m, dir := newDoorRecord(t)
	m = pickRecField(t, m, "Type", "synchronet_js")
	m = setRecField(t, m, "Script", "lord.js")
	m = setRecField(t, m, "Exec Dir", "/sbbs/exec")
	m = setRecField(t, m, "Library Paths", "/a, /b")
	m = setRecField(t, m, "Script Args", "x,y")
	wantScreen(t, m, "lord.js", "/sbbs/exec")
	d := savedDoor(t, m, dir)
	if d.Type != "synchronet_js" || d.Script != "lord.js" || d.ExecDir != "/sbbs/exec" ||
		strings.Join(d.LibraryPaths, "|") != "/a|/b" || strings.Join(d.Args, "|") != "x|y" {
		t.Errorf("door = %+v", d)
	}
}

// TestDoorFields_V3Script pins the VPL script door fields.
func TestDoorFields_V3Script(t *testing.T) {
	m, dir := newDoorRecord(t)
	m = pickRecField(t, m, "Type", "v3_script")
	m = setRecField(t, m, "Script", "game.js")
	m = setRecField(t, m, "Script Args", "one")
	for _, f := range m.recordFields {
		if f.Label == "Exec Dir" || f.Label == "Cleanup Command" {
			t.Errorf("VPL door shows %q", f.Label)
		}
	}
	d := savedDoor(t, m, dir)
	if d.Type != "v3_script" || d.Script != "game.js" || len(d.Args) != 1 {
		t.Errorf("door = %+v", d)
	}
}

// TestDoorFields_RLogin pins the RLogin door fields: port and timeout ranges
// and disconnect-key syntax are enforced, and the values persist.
func TestDoorFields_RLogin(t *testing.T) {
	m, dir := newDoorRecord(t)
	m = pickRecField(t, m, "Type", "rlogin")
	m = setRecField(t, m, "Host", "  doors.example  ")
	m = setRecField(t, m, "Port", "2513")
	m = setRecField(t, m, "Client User", "pw")
	m = setRecField(t, m, "Server User", "[V3]{USERHANDLE}")
	m = setRecField(t, m, "Terminal Type", "xtrn=LORD")
	m = press(t, gotoField(t, m, "Connect Timeout"), "enter")
	m = press(t, replaceText(t, m, "301"), "enter")
	if m.mode != modeRecordField {
		t.Fatal("timeout 301 accepted")
	}
	m = press(t, replaceText(t, m, "30"), "enter")
	m = press(t, gotoField(t, m, "Disconnect Key"), "enter")
	m = press(t, replaceText(t, m, "^^^"), "enter")
	if m.mode != modeRecordField || !strings.HasPrefix(m.message, "Invalid:") {
		t.Fatalf("bad disconnect key accepted: %q", m.message)
	}
	m = press(t, replaceText(t, m, "none"), "enter")
	m = setRecField(t, m, "Cleanup Command", "")
	d := savedDoor(t, m, dir)
	if d.Type != "rlogin" || d.Host != "doors.example" || d.Port != 2513 || d.ClientUsername != "pw" ||
		d.ServerUsername != "[V3]{USERHANDLE}" || d.TerminalType != "xtrn=LORD" || d.ConnectTimeout != 30 || d.DisconnectKey != "none" {
		t.Errorf("door = %+v", d)
	}
}

// TestDoorFields_Telnet pins the Telnet door fields through the record editor:
// the login is typed as escapes, Raw TCP is a yes/no, and the RLogin handshake
// fields are not offered.
func TestDoorFields_Telnet(t *testing.T) {
	m, dir := newDoorRecord(t)
	m = pickRecField(t, m, "Type", "telnet")
	for _, f := range m.recordFields {
		if f.Label == "Client User" || f.Label == "Server User" {
			t.Fatalf("telnet door offers the rlogin field %q", f.Label)
		}
	}
	m = setRecField(t, m, "Host", "doors.example")
	m = setRecField(t, m, "Port", "2323")
	m = setRecField(t, m, "Terminal Type", "ANSI-BBS")
	m = setRecField(t, m, "Send On Connect", `{USERHANDLE}\rsecret\r`)
	m = press(t, gotoField(t, m, "Raw TCP"), "space")
	m = setRecField(t, m, "Cleanup Command", "")
	d := savedDoor(t, m, dir)
	if d.Type != "telnet" || d.Host != "doors.example" || d.Port != 2323 || d.TerminalType != "ANSI-BBS" ||
		d.SendOnConnect != "{USERHANDLE}\rsecret\r" || !d.RawTCP {
		t.Errorf("door = %+v", d)
	}
}

// TestDoorFields_DOS pins the DOS door fields: commands split on commas,
// emulator choice, cleanup command parsing, and env vars.
func TestDoorFields_DOS(t *testing.T) {
	m, dir := newDoorRecord(t)
	m = pickRecField(t, m, "Type", "dos")
	for _, f := range m.recordFields {
		if f.Label == "Dropfile Case" || f.Label == "I/O Mode" {
			t.Errorf("DOS door shows %q", f.Label)
		}
	}
	m = setRecField(t, m, "Commands", "CD GAME, GAME.EXE")
	m = pickRecField(t, m, "Dropfile Type", "DOOR.SYS")
	m = pickRecField(t, m, "Dropfile Location", "node")
	// A DOS door gets every dropfile, DROPFILE.INI included, so it offers
	// the DROPFILE.INI privacy switch whatever its dropfile type.
	m = press(t, gotoField(t, m, "Hide Personal"), "space")
	m = setRecField(t, m, "Drive C Path", "/dos/c")
	m = pickRecField(t, m, "DOS Emulator", "dosemu")
	m = setRecField(t, m, "FOSSIL Driver", `C:\X00.EXE`)
	m = setRecField(t, m, "DOSemu Config", "my.rc")
	m = setRecField(t, m, "Cleanup Command", "rm -f, a")
	m = press(t, gotoField(t, m, "Env Vars"), "enter")
	m = press(t, replaceText(t, m, "NOEQUALS"), "enter")
	if m.mode != modeRecordField {
		t.Fatal("malformed env vars accepted")
	}
	m = press(t, replaceText(t, m, "TERM=ansi, LANG=C"), "enter")
	wantScreen(t, m, "/dos/c")
	d := savedDoor(t, m, dir)
	if !d.IsDOS || strings.Join(d.Commands, "|") != "CD GAME|GAME.EXE" || d.DropfileType != "DOOR.SYS" ||
		d.DropfileLocation != "node" || d.DriveCPath != "/dos/c" || d.DOSEmulator != "dosemu" ||
		d.FossilDriver != `C:\X00.EXE` || d.DosemuConfig != "my.rc" || !d.DropfileHidePersonal {
		t.Errorf("door = %+v", d)
	}
	if d.CleanupCommand != "rm" || strings.Join(d.CleanupArgs, "|") != "-f|a" {
		t.Errorf("cleanup = %q %v", d.CleanupCommand, d.CleanupArgs)
	}
	if d.EnvironmentVars["TERM"] != "ansi" || d.EnvironmentVars["LANG"] != "C" {
		t.Errorf("env = %v", d.EnvironmentVars)
	}
}

// TestDoorFields_Native pins the native door fields: renaming the code
// re-keys the door, and case, I/O mode, access level and toggles persist.
func TestDoorFields_Native(t *testing.T) {
	m, dir := newDoorRecord(t)
	m = press(t, m, "enter")
	m = press(t, replaceText(t, m, "bad code!"), "enter")
	if m.mode != modeRecordField {
		t.Fatal("invalid door code accepted")
	}
	m = press(t, replaceText(t, m, "lord"), "enter")
	if _, ok := m.configs.Doors["LORD"]; !ok || m.recordFields[m.editField].Label != "Code" {
		t.Fatalf("rename: doors=%v field=%q", m.configs.Doors, m.recordFields[m.editField].Label)
	}
	m = setRecField(t, m, "Commands", "./lord {NODE}, -x")
	m = pickRecField(t, m, "Dropfile Case", "lower")
	m = pickRecField(t, m, "I/O Mode", "SOCKET")
	m = press(t, gotoField(t, m, "Min Access Level"), "enter")
	m = press(t, replaceText(t, m, "300"), "enter")
	if m.mode != modeRecordField {
		t.Fatal("access level 300 accepted")
	}
	m = press(t, replaceText(t, m, "20"), "enter")
	m = press(t, gotoField(t, m, "Single Instance"), "space")
	m = press(t, gotoField(t, m, "Raw Terminal"), "space")
	d := savedDoor(t, m, dir)
	if d.Code != "LORD" || strings.Join(d.Commands, "|") != "./lord|{NODE}|-x" || d.DropfileCase != "lower" ||
		d.IOMode != "SOCKET" || d.MinAccessLevel != 20 || !d.SingleInstance || !d.RequiresRawTerminal {
		t.Errorf("door = %+v", d)
	}
}
