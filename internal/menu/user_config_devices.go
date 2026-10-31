package menu

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

const (
	// qwkDevicesKonfigKey is the Konfig hotkey for the device screen.
	qwkDevicesKonfigKey = 'O'
	// deviceNameColumns is the width of the device column in qwkDevicesHeader.
	deviceNameColumns = 32
	deviceDateFormat  = "2006-01-02"
)

// apiDevicesShown reports whether the current caller gets the device screen.
func apiDevicesShown(c *cmdCtx) bool {
	if c.userManager == nil || c.currentUser == nil {
		return false
	}
	return c.e.GetServerConfig().QWKAPI.Enabled || len(storedAPIDevices(c)) > 0
}

// storedAPIDevices returns the caller's valid devices from the stored record.
// A session's copy of the user does not follow pairings, so it cannot be used.
func storedAPIDevices(c *cmdCtx) []user.APIDevice {
	u, ok := c.userManager.GetUserByID(c.currentUser.ID)
	if !ok {
		return nil
	}
	return u.ValidAPIDevices()
}

// deviceScreenAvailable reports whether the sysop editor offers the device
// screen for the selected user.
func (st *userEditorState) deviceScreenAvailable() bool {
	if len(st.users) == 0 || st.selectedIndex < 0 || st.selectedIndex >= len(st.users) {
		return false
	}
	u, ok := st.userManager.GetUserByID(st.users[st.selectedIndex].ID)
	return ok && u.ShowsAPIDevices(st.e.GetServerConfig().QWKAPI.Enabled)
}

// withoutKonfigItem returns the sections with the item for key left out, so a
// board without the QWK API has no gap in the form.
func withoutKonfigItem(cols [2][]konfigSection, key byte) [2][]konfigSection {
	for c := range cols {
		for s := range cols[c] {
			kept := make([]*konfigItem, 0, len(cols[c][s].items))
			for _, it := range cols[c][s].items {
				if it.key != key {
					kept = append(kept, it)
				}
			}
			cols[c][s].items = kept
		}
	}
	return cols
}

// qwkDevicesValue is the Konfig value for the device item: how many are paired.
func qwkDevicesValue(st *konfigState) konfigValue {
	return konfigValue{strconv.Itoa(len(storedAPIDevices(st.c))), toneValue}
}

// editQwkDevices opens the device screen from Konfig.
func editQwkDevices(st *konfigState) error {
	st.redraw = true
	_, _, err := runCfgQwkDevices(st.c, "")
	return err
}

// runCfgQwkDevices lets the caller see and unpair their QWK API devices (#719).
func runCfgQwkDevices(c *cmdCtx, _ string) (*user.User, string, error) {
	u := c.currentUser
	if u == nil || c.userManager == nil {
		return nil, "", nil
	}
	if !apiDevicesShown(c) {
		c.e.writeString(c.terminal, c.outputMode, c.e.Strings().QwkDevicesDisabled)
		uiPause(readerNoticePause)
		return u, "", nil
	}
	screen := deviceScreen{e: c.e, s: c.s, terminal: c.terminal, mode: c.outputMode, um: c.userManager,
		width: c.termWidth, height: c.termHeight, userID: u.ID, by: u.Handle}
	if err := screen.run(); err != nil {
		return u, "LOGOFF", err
	}
	return u, "", nil
}

// deviceScreen lists one user's paired devices and unpairs them on request. It
// serves both the caller's own screen (#719) and the sysop editor (#718).
type deviceScreen struct {
	e             *MenuExecutor
	s             ssh.Session
	terminal      *term.Terminal
	mode          ansi.OutputMode
	um            *user.UserMgr
	width, height int
	userID        int
	owner         string // handle shown in the title; blank on the caller's own screen
	by            string // who is unpairing, for the security log
}

// run shows the screen until the viewer quits. A pairing cannot exceed the
// per-account cap of ten devices, so the list always fits without paging. It
// returns an error only when the session should end.
func (d deviceScreen) run() error {
	notice := ""
	for {
		u, ok := d.um.GetUserByID(d.userID)
		if !ok {
			return nil
		}
		devices := u.ValidAPIDevices()
		d.draw(devices, notice)
		if len(devices) == 0 {
			d.e.holdScreen(d.s, d.terminal, d.mode, d.width, d.height)
			return nil
		}
		d.e.writeString(d.terminal, d.mode, d.e.Strings().QwkDevicesPrompt)
		choice, err := readLineFromSessionIH(d.s, d.terminal)
		if err != nil {
			return err
		}
		choice = strings.ToUpper(strings.TrimSpace(choice))
		if choice == "" || choice == "Q" {
			return nil
		}
		notice = d.unpair(u, devices, choice)
	}
}

// unpair removes the device numbered choice on the list the viewer was shown and
// returns the notice to show. The device is found by ID, so a pairing or
// unpairing elsewhere meanwhile cannot make the number hit a different one.
func (d deviceScreen) unpair(u *user.User, devices []user.APIDevice, choice string) string {
	st := d.e.Strings()
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(devices) {
		return st.QwkDevicesInvalid
	}
	target := devices[n-1]
	removed, err := d.um.RemoveAPIDevice(u.ID, target.ID)
	if err != nil {
		return st.QwkDevicesSaveFailed
	}
	if !removed {
		return st.QwkDevicesInvalid
	}
	logging.Security("qwk api device unpaired", "handle", u.Handle, "device", target.ID, "name", target.Name, "by", d.by)
	return st.QwkDevicesRemoved
}

// draw paints the screen. Device names come from the client, so they go in after
// the colour codes are expanded and are never read as pipe codes.
func (d deviceScreen) draw(devices []user.APIDevice, notice string) {
	st := d.e.Strings()
	write := func(s string) { _ = terminalio.WriteProcessedBytes(d.terminal, []byte(s), d.mode) }
	pipe := func(s string) string { return string(ansi.ReplacePipeCodes([]byte(s))) }

	write(ansi.ClearScreen())
	title := pipe(st.QwkDevicesTitle)
	if d.owner != "" {
		title += pipe(" |08/ |07") + d.owner
	}
	write(title + "\r\n\r\n")
	if len(devices) == 0 {
		write(pipe(st.QwkDevicesEmpty) + "\r\n")
	} else {
		write(pipe(st.QwkDevicesHeader) + "\r\n")
		for i, dev := range devices {
			write(pipe("|15") + fmt.Sprintf("%2d  ", i+1) + pipe("|07") + d.deviceCell(dev.Name) + " " + pipe("|08") +
				dev.CreatedAt.Local().Format(deviceDateFormat) + "  " + deviceLastUsed(dev.LastUsed, st.QwkDevicesNever) + pipe("|07") + "\r\n")
		}
	}
	write("\r\n")
	if notice != "" {
		write(pipe(notice) + "\r\n")
	}
}

// deviceCell cuts and pads a name to the column, in on-screen columns.
func (d deviceScreen) deviceCell(name string) string {
	name = clipColumns(name, deviceNameColumns, d.mode)
	used := columnWidth(name, utf8.ValidString(name), d.mode)
	return name + strings.Repeat(" ", max(deviceNameColumns-used, 0))
}

// deviceLastUsed formats a device's last login, or never when it has not had one.
func deviceLastUsed(at time.Time, never string) string {
	if at.IsZero() {
		return never
	}
	return at.Local().Format(deviceDateFormat)
}
