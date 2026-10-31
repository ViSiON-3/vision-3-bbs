package menu

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// devicesCtx returns a session for a user with the named devices paired.
func devicesCtx(t *testing.T, enabled bool, keys string, names ...string) (*cmdCtx, *testterm.Term, *user.UserMgr) {
	t.Helper()
	um, u := newUserConfigTestUser(t)
	for _, name := range names {
		d := user.APIDevice{ID: "id-" + name, Name: name, PublicKey: "key-" + name,
			PasswordStamp: user.PasswordStamp(u.PasswordHash), CreatedAt: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)}
		if err := um.AddAPIDevice(u.ID, d, 10); err != nil {
			t.Fatal(err)
		}
	}
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	e := konfigTestExecutor(t)
	e.SetServerConfig(config.ServerConfig{QWKAPI: config.QWKAPIConfig{Enabled: enabled}})
	return &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}, screen, um
}

func konfigHasDevices(c *cmdCtx) bool {
	cols := konfigSections(c.e.Strings())
	if !apiDevicesShown(c) {
		cols = withoutKonfigItem(cols, qwkDevicesKonfigKey)
	}
	items, _ := layoutKonfig(cols)
	for _, it := range items {
		if it.key == qwkDevicesKonfigKey {
			return true
		}
	}
	return false
}

// The API is optional: a board without it gets no Konfig item, unless the caller
// has devices left from when it was on.
func TestKonfigDevicesItemFollowsTheAPI(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		devices []string
		want    bool
	}{
		{"off", false, nil, false},
		{"on", true, nil, true},
		{"off with leftovers", false, []string{"Phone"}, true},
	}
	for _, tt := range tests {
		c, _, _ := devicesCtx(t, tt.enabled, "", tt.devices...)
		if got := konfigHasDevices(c); got != tt.want {
			t.Errorf("%s: shown = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDeviceScreenRefusedWithoutAPI(t *testing.T) {
	c, screen, _ := devicesCtx(t, false, "")
	if _, _, err := runCfgQwkDevices(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := screen.Snapshot(); !strings.Contains(got, "not enabled") {
		t.Errorf("screen = %q", got)
	}
}

func TestDeviceScreenUnpairsByNumber(t *testing.T) {
	c, screen, um := devicesCtx(t, true, "9\r1\rq\r", "Old Phone", "New Phone")
	if _, _, err := runCfgQwkDevices(c, ""); err != nil {
		t.Fatal(err)
	}
	u, _ := um.GetUserByID(c.currentUser.ID)
	if got := u.ValidAPIDevices(); len(got) != 1 || got[0].Name != "New Phone" {
		t.Fatalf("devices = %+v, want only New Phone", got)
	}
	if got := screen.Snapshot(); !strings.Contains(got, "New Phone") || !strings.Contains(got, "unpaired") {
		t.Errorf("screen = %q", got)
	}
}

// A device names itself, so a name must not be read as colour codes.
func TestDeviceScreenShowsNameAsWritten(t *testing.T) {
	c, screen, _ := devicesCtx(t, true, "q\r", "|12Phone")
	if _, _, err := runCfgQwkDevices(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := screen.Snapshot(); !strings.Contains(got, "|12Phone") {
		t.Errorf("name was interpreted: %q", got)
	}
}

// The sysop editor uses the same screen on someone else's account, and the title
// says whose.
func TestDeviceScreenForSysopNamesTheOwner(t *testing.T) {
	c, screen, um := devicesCtx(t, true, "1\r\r", "Phone")
	d := deviceScreen{e: c.e, s: c.s, terminal: c.terminal, mode: c.outputMode, um: um, width: 80, height: 24,
		userID: c.currentUser.ID, owner: c.currentUser.Handle, by: "Sysop"}
	if err := d.run(); err != nil {
		t.Fatal(err)
	}
	if u, _ := um.GetUserByID(c.currentUser.ID); len(u.ValidAPIDevices()) != 0 {
		t.Error("device still paired")
	}
	if got := screen.Snapshot(); !strings.Contains(got, c.currentUser.Handle) {
		t.Errorf("owner missing: %q", got)
	}
}
