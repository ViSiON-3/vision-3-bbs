package menu

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

const (
	keyUp    = "\x1b[A"
	keyDown  = "\x1b[B"
	keyRight = "\x1b[C"
	keyEsc   = "\x1b"
	keyClear = "\x15" // Ctrl-U
)

// runKonfig drives USERCONFIG with keys on an 80x24 screen and returns the
// screen, the user it handed back, and its next action.
func runKonfig(t *testing.T, um *user.UserMgr, u *user.User, keys string, args string) (*testterm.Term, *user.User, string) {
	t.Helper()
	return runKonfigMode(t, um, u, keys, args, ansi.OutputModeUTF8)
}

// runKonfigMode is runKonfig on a session using the given encoding.
func runKonfigMode(t *testing.T, um *user.UserMgr, u *user.User, keys, args string, mode ansi.OutputMode) (*testterm.Term, *user.User, string) {
	t.Helper()
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	c := &cmdCtx{
		e:           &MenuExecutor{},
		s:           sess,
		terminal:    term.NewTerminal(sess, ""),
		userManager: um,
		currentUser: u,
		nodeNumber:  1,
		outputMode:  mode,
		termWidth:   80,
		termHeight:  24,
	}
	// Scripts must end by leaving the editor (q): once the keys run out the
	// session blocks like a live connection would.
	got, next, err := runUserKonfig(c, args)
	if err != nil {
		t.Fatalf("runUserKonfig: %v", err)
	}
	return screen, got, next
}

func reloadUser(t *testing.T, um *user.UserMgr) *user.User {
	t.Helper()
	u, ok := um.GetUser("Tester")
	if !ok {
		t.Fatal("user vanished")
	}
	return u
}

func TestKonfigDrawsEverySettingInsideTheScreen(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, _, next := runKonfig(t, um, u, "q", "")
	if next != "" {
		t.Errorf("next = %q, want empty (return to calling menu)", next)
	}
	snap := screen.Snapshot()
	t.Logf("\n%s", snap)

	for _, want := range []string{
		"[A] Screen Width", "[B] Screen Height", "[C] Encoding", "[D] Hot Keys",
		"[E] Header Style", "[F] Auto-Signature", "[G] Real Name", "[H] Location",
		"[I] User Note", "[J] Password", "[K] Listing Mode", "[L] File Columns",
		"Terminal", "Messages", "Personal", "Files",
	} {
		if !strings.Contains(snap, want) {
			t.Errorf("screen is missing %q", want)
		}
	}
	// The first item starts highlighted and its help is shown.
	if !strings.Contains(screen.Row(konfigHelpRow), "Columns your terminal shows") {
		t.Errorf("help row = %q", screen.Row(konfigHelpRow))
	}
	// Values the user already has are shown.
	if !strings.Contains(screen.Row(konfigTopRow+2), "Loc") {
		t.Error("existing values are not shown")
	}
	for r := 1; r <= 24; r++ {
		if n := utf8.RuneCountInString(strings.TrimRight(screen.Row(r), " ")); n > 79 {
			t.Errorf("row %d is %d columns wide", r, n)
		}
	}
}

// The stock header art must fit in the rows the form leaves for it, in both
// encodings, or the form would draw over it.
func TestKonfigStockHeaderArtFits(t *testing.T) {
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		um, u := newUserConfigTestUser(t)
		var opts []testterm.Option
		if mode == ansi.OutputModeCP437 {
			opts = append(opts, testterm.CP437())
		}
		screen := testterm.New(80, 24, opts...)
		sess := testterm.NewSession(screen, "q")
		t.Cleanup(func() { resetSessionIH(sess) })
		c := &cmdCtx{e: &MenuExecutor{MenuSetPath: "../../menus/v3"}, s: sess,
			terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
			outputMode: mode, termWidth: 80, termHeight: 24}
		if _, _, err := runUserKonfig(c, ""); err != nil {
			t.Fatal(err)
		}
		t.Logf("%v:\n%s", mode, screen.Snapshot())
		snap := screen.Snapshot()
		if !strings.HasSuffix(strings.TrimRight(screen.Row(1), " "), "[ViSiON/3]") {
			t.Errorf("%v: row 1 should end at [ViSiON/3]; got %q", mode, screen.Row(1))
		}
		if !strings.HasPrefix(screen.Row(2), "█ █") {
			t.Errorf("%v: row 2 should start the logo at column 1; got %q", mode, screen.Row(2))
		}
		if !strings.Contains(snap, "User: Tester") {
			t.Errorf("%v: |UH not substituted", mode)
		}
		if !strings.Contains(snap, fmt.Sprintf("Level: %d", u.AccessLevel)) {
			t.Errorf("%v: |LEVEL not substituted", mode)
		}
		if strings.Contains(snap, "|UH") || strings.Contains(snap, "|LEVEL") {
			t.Errorf("%v: a raw token reached the screen", mode)
		}
		if strings.TrimSpace(screen.Row(konfigHeaderRows+1)) != "" {
			t.Errorf("%v: row %d under the art is not blank: %q", mode, konfigHeaderRows+1, screen.Row(konfigHeaderRows+1))
		}
		if !strings.Contains(screen.Row(konfigTopRow), "Terminal") {
			t.Errorf("%v: form displaced; row %d = %q", mode, konfigTopRow, screen.Row(konfigTopRow))
		}
	}
}

// The form must fit the smallest screen a caller can set, and the items must
// stay clear of the rows below them.
func TestKonfigLayoutFitsMinimumHeight(t *testing.T) {
	if konfigLastRow > konfigMinHeight {
		t.Fatalf("form needs %d rows; the smallest allowed screen has %d", konfigLastRow, konfigMinHeight)
	}
	items, headings := layoutKonfig(konfigSections())
	for _, h := range headings {
		if h.row < konfigTopRow || h.row >= konfigRuleRow {
			t.Errorf("heading %q on row %d, outside rows %d-%d", h.title, h.row, konfigTopRow, konfigRuleRow-1)
		}
	}
	for _, it := range items {
		if it.row < konfigTopRow || it.row >= konfigRuleRow {
			t.Errorf("item %c on row %d, outside rows %d-%d", it.key, it.row, konfigTopRow, konfigRuleRow-1)
		}
	}
	if boxBottom := colBoxTop + len(fileColumns) + 3; boxBottom >= konfigRuleRow {
		t.Errorf("file-column box ends on row %d, over the rule on row %d", boxBottom, konfigRuleRow)
	}
}

// A sub-screen that leaves the cursor showing must not leave it blinking
// under the form once the form repaints.
func TestKonfigRepaintHidesCursor(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, "")
	t.Cleanup(func() { resetSessionIH(sess) })
	c := &cmdCtx{e: &MenuExecutor{}, s: sess, terminal: term.NewTerminal(sess, ""),
		userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	st := &konfigState{c: c}
	st.items, st.headings = layoutKonfig(konfigSections())

	_, _ = sess.Write([]byte("\x1b[?25h")) // as the header picker leaves it
	if err := st.renderAll(); err != nil {
		t.Fatal(err)
	}
	if screen.CursorVisible() {
		t.Fatal("cursor still visible after a full repaint")
	}
}

func TestKonfigShowsHeaderStyleName(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	u.MsgHdr = 4
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, "q")
	t.Cleanup(func() { resetSessionIH(sess) })
	c := &cmdCtx{e: &MenuExecutor{MenuSetPath: "../../menus/v3"}, s: sess,
		terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	// Style 4 in the stock MSGHDR.BAR, cut to the value column.
	if !strings.Contains(screen.Snapshot(), "LiQUiD Blue Box") {
		t.Errorf("header style name not shown:\n%s", screen.Snapshot())
	}
	if !strings.Contains(screen.Row(konfigLegendRow), "[Q/ESC] Done") {
		t.Errorf("legend row = %q", screen.Row(konfigLegendRow))
	}
}

func TestKonfigHotkeyTogglesAndSaves(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, got, _ := runKonfig(t, um, u, "dq", "")
	if !got.HotKeys || !reloadUser(t, um).HotKeys {
		t.Fatal("Hot Keys not switched on and saved")
	}
	if !strings.Contains(screen.Row(konfigEditRow), "Hot Keys is now On") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}
}

func TestKonfigArrowsAndEnterActivateHighlightedItem(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	// Down three times from Screen Width lands on Hot Keys.
	_, got, _ := runKonfig(t, um, u, keyDown+keyDown+keyDown+"\rq", "")
	if !got.HotKeys {
		t.Fatal("Enter on the highlighted Hot Keys row did not switch it")
	}
}

func TestKonfigRightArrowCrossesColumns(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	// Right from Screen Width goes to Real Name; Down, Down reaches User Note.
	_, got, _ := runKonfig(t, um, u, keyRight+keyDown+keyDown+"\r"+keyClear+"hello\rq", "")
	if got.PrivateNote != "hello" {
		t.Fatalf("PrivateNote = %q, want hello", got.PrivateNote)
	}
}

func TestKonfigTextFieldEditsAndClears(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	_, _, _ = runKonfig(t, um, u, "h"+keyClear+"Somewhere, CA\rq", "")
	if got := reloadUser(t, um).GroupLocation; got != "Somewhere, CA" {
		t.Fatalf("GroupLocation = %q", got)
	}
	_, _, _ = runKonfig(t, um, reloadUser(t, um), "h"+keyClear+"\rq", "")
	if got := reloadUser(t, um).GroupLocation; got != "" {
		t.Fatalf("GroupLocation = %q, want cleared", got)
	}
}

func TestKonfigEscAbandonsAnEdit(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	_, got, _ := runKonfig(t, um, u, "hxyz"+keyEsc+"q", "")
	if got.GroupLocation != "Loc" || reloadUser(t, um).GroupLocation != "Loc" {
		t.Fatalf("GroupLocation = %q, want it untouched", got.GroupLocation)
	}
}

func TestKonfigRealNameIsValidated(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, got, _ := runKonfig(t, um, u, "g"+keyClear+"Bob\rq", "")
	if got.RealName != "Real Name" {
		t.Fatalf("RealName = %q, want it untouched", got.RealName)
	}
	if !strings.Contains(screen.Row(konfigEditRow), "Real name must") {
		t.Errorf("status row = %q, want the validation message", screen.Row(konfigEditRow))
	}
}

func TestKonfigLineEditorKeys(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	// "Loc" -> Home, insert "My ", End, Backspace -> "My Lo"
	_, got, _ := runKonfig(t, um, u, "h\x1b[HMy \x1b[F\x7f\rq", "")
	if got.GroupLocation != "My Lo" {
		t.Fatalf("GroupLocation = %q, want %q", got.GroupLocation, "My Lo")
	}
}

func TestKonfigScreenWidthRangeAndLiveApply(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, got, _ := runKonfig(t, um, u, "a"+keyClear+"20\rq", "")
	if got.ScreenWidth == 20 {
		t.Fatal("out-of-range width was accepted")
	}
	if !strings.Contains(screen.Row(konfigEditRow), "40 to 255") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}

	sess := testterm.NewSession(testterm.New(80, 24), "a"+keyClear+"132\rq")
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: &MenuExecutor{}, s: sess, terminal: term.NewTerminal(sess, ""),
		userManager: um, currentUser: got, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	got, _, err := runUserKonfig(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ScreenWidth != 132 || reloadUser(t, um).ScreenWidth != 132 {
		t.Fatalf("ScreenWidth = %d, want 132", got.ScreenWidth)
	}
	w, h, ok := takeSessionTermSize(sess)
	if !ok || w != 132 || h != 24 {
		t.Fatalf("session size = %d x %d (%v), want 132 x 24", w, h, ok)
	}
}

// Encoding cycles Auto, the encoding the session is using, the other one,
// then back to Auto. The first press never changes what the caller sees.
func TestKonfigEncodingCycle(t *testing.T) {
	for _, tc := range []struct {
		mode ansi.OutputMode
		want []string
	}{
		{ansi.OutputModeUTF8, []string{"utf8", "cp437", ""}},
		{ansi.OutputModeCP437, []string{"cp437", "utf8", ""}},
	} {
		um, u := newUserConfigTestUser(t)
		for _, want := range tc.want {
			_, u, _ = runKonfigMode(t, um, u, "cq", "", tc.mode)
			if u.PreferredEncoding != want || reloadUser(t, um).PreferredEncoding != want {
				t.Fatalf("session %v: PreferredEncoding = %q, want %q", tc.mode, u.PreferredEncoding, want)
			}
		}
	}
}

func TestKonfigEncodingWarnsOnMismatch(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, u, _ := runKonfig(t, um, u, "cq", "") // utf8 on a UTF-8 session
	if row := screen.Row(konfigEditRow); strings.Contains(row, "this terminal is using") {
		t.Errorf("matching choice warned: %q", row)
	}
	screen, _, _ = runKonfig(t, um, u, "cq", "") // cp437 on a UTF-8 session
	if row := screen.Row(konfigEditRow); !strings.Contains(row, "this terminal is using UTF-8") {
		t.Errorf("mismatch not warned: %q", row)
	}
}

func TestKonfigEncodingValue(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	for _, tc := range []struct {
		pref, want string
	}{
		{"", "Auto (UTF-8)"},
		{"utf8", "UTF-8"},
		{"cp437", "CP437"},
	} {
		u.PreferredEncoding = tc.pref
		screen, _, _ := runKonfig(t, um, u, "q", "")
		if got := konfigFieldText(screen, "[C] Encoding"); !strings.HasPrefix(got, tc.want) {
			t.Errorf("pref %q: Encoding shows %q, want %q", tc.pref, got, tc.want)
		}
	}
}

// konfigFieldText returns what follows label on the screen row that has it.
func konfigFieldText(screen *testterm.Term, label string) string {
	for row := 1; row <= 24; row++ {
		if _, after, ok := strings.Cut(screen.Row(row), label); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func TestKonfigListingModeSwitches(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	_, got, _ := runKonfig(t, um, u, "kq", "")
	if got.FileListingMode != "classic" {
		t.Fatalf("FileListingMode = %q, want classic", got.FileListingMode)
	}
}

func TestKonfigPasswordChange(t *testing.T) {
	um, u := newUserConfigTestUser(t)

	// Wrong current password: nothing changes.
	screen, got, _ := runKonfig(t, um, u, "jwrong\rq", "")
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("password")) != nil {
		t.Fatal("password changed despite a wrong current password")
	}
	if !strings.Contains(screen.Row(konfigEditRow), "Incorrect password") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}

	// Mismatched confirmation: nothing changes.
	_, got, _ = runKonfig(t, um, got, "jpassword\rsecret1\rsecret2\rq", "")
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("password")) != nil {
		t.Fatal("password changed despite a mismatched confirmation")
	}

	// The password is never echoed.
	screen, got, _ = runKonfig(t, um, got, "jpassword\rsecret1\rsecret1\rq", "")
	if strings.Contains(screen.Snapshot(), "secret1") {
		t.Error("password was echoed")
	}
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte("secret1")) != nil {
		t.Fatal("new password not set on the session's user")
	}
	if bcrypt.CompareHashAndPassword([]byte(reloadUser(t, um).PasswordHash), []byte("secret1")) != nil {
		t.Fatal("new password not saved")
	}
}

// Passwords longer than the room on the edit row scroll instead of being
// cut, so a long existing password can still be verified and replaced.
func TestKonfigLongPassword(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	long := strings.Repeat("abcdefghij", 6) // 60 characters
	hash, err := bcrypt.GenerateFromPassword([]byte(long), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	u.PasswordHash = string(hash)
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	next := strings.Repeat("0123456789", 7) // 70 characters
	_, got, _ := runKonfig(t, um, u, "j"+long+"\r"+next+"\r"+next+"\rq", "")
	if bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte(next)) != nil {
		t.Fatal("a long password was not accepted in full")
	}
}

// Opening a field whose stored value is longer than the form's limit must
// not shorten it.
func TestKonfigKeepsOverlongExistingValue(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	note := strings.Repeat("n", 35) // the old CFG_NOTE limit
	u.PrivateNote = note
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	_, got, _ := runKonfig(t, um, u, "i\rq", "")
	if got.PrivateNote != note {
		t.Fatalf("PrivateNote = %q (%d), want the 35-character value untouched", got.PrivateNote, len(got.PrivateNote))
	}
}

func TestKonfigFileColumns(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	// From "all" (nothing chosen), turning Name off leaves the other five on.
	_, got, _ := runKonfig(t, um, u, "ln"+keyEsc+"q", "")
	c := got.FileListColumns
	if c.Name || !c.Size || !c.Date || !c.Downloads || !c.Uploader || !c.Description {
		t.Fatalf("columns = %+v, want everything but Name", c)
	}
	if reloadUser(t, um).FileListColumns != c {
		t.Fatal("columns not saved")
	}

	// The last column cannot be turned off.
	_, got, _ = runKonfig(t, um, got, "lsdlue"+keyEsc+"q", "")
	c = got.FileListColumns
	if !c.Description || c.Size || c.Date || c.Downloads || c.Uploader {
		t.Fatalf("columns = %+v, want only Description left on", c)
	}
}

func TestKonfigLegacyEntryReturnsToNamedMenu(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	_, _, next := runKonfig(t, um, u, "q", "main")
	if next != "GOTO:MAIN" {
		t.Fatalf("next = %q, want GOTO:MAIN", next)
	}
}

// --- Hot keys --------------------------------------------------------------

func TestHotKeyNeedsLine(t *testing.T) {
	cmds := []CommandRecord{
		{Keys: "//", Command: "RUN:X"},
		{Keys: "M", Command: "GOTO:MAIL"},
		{Keys: "1 10", Command: "RUN:ONE"},
		{Keys: "!Z", Command: "RUN:BANG"},
		{Keys: "^M", Command: "GOTO:MAIN"},
	}
	cases := map[string]bool{
		"M": false, // whole command
		"X": false, // unknown, but nothing longer starts with it
		"1": true,  // could be 10
		"!": true,  // could be !Z
		"/": true,  // /G hangup
		"^": false, // ^M is Enter, never typed
	}
	for k, want := range cases {
		if got := hotKeyNeedsLine(cmds, k, nil); got != want {
			t.Errorf("hotKeyNeedsLine(%q) = %v, want %v", k, got, want)
		}
	}
	if !hotKeyNeedsLine([]CommandRecord{{Keys: "##"}}, "4", nil) {
		t.Error("a digit on a menu that takes numbers must wait for the rest")
	}
	if hotKeyNeedsLine([]CommandRecord{{Keys: "##"}}, "A", nil) {
		t.Error("## must not hold up letters")
	}
}

func TestHotKeyNeedsLineIgnoresCommandsTheCallerCannotUse(t *testing.T) {
	cmds := []CommandRecord{{Keys: "X", ACS: "*"}, {Keys: "XA", ACS: "S255"}}
	sysop := func(string) bool { return true }
	caller := func(acs string) bool { return acs != "S255" }
	if !hotKeyNeedsLine(cmds, "X", sysop) {
		t.Error("a sysop could mean XA, so X must wait")
	}
	if hotKeyNeedsLine(cmds, "X", caller) {
		t.Error("a caller who cannot run XA should get X at once")
	}
}

func hotKeyLoop(t *testing.T, keys string, cmds []CommandRecord) *runLoopState {
	t.Helper()
	sess := testterm.NewSession(testterm.New(80, 24), keys)
	t.Cleanup(func() { resetSessionIH(sess) })
	return &runLoopState{s: sess, terminal: term.NewTerminal(sess, ""), commands: cmds}
}

func TestReadHotKeyInput(t *testing.T) {
	cmds := []CommandRecord{{Keys: "M"}, {Keys: "10"}}

	if got, err := hotKeyLoop(t, "m", cmds).readHotKeyInput(); err != nil || got != "M" {
		t.Errorf("single key = (%q, %v), want M", got, err)
	}
	// Keys that do nothing are skipped rather than returned.
	if got, err := hotKeyLoop(t, keyUp+"m", cmds).readHotKeyInput(); err != nil || got != "M" {
		t.Errorf("after an arrow = (%q, %v), want M", got, err)
	}
	if got, err := hotKeyLoop(t, "\r", cmds).readHotKeyInput(); err != nil || got != "" {
		t.Errorf("Enter = (%q, %v), want empty", got, err)
	}
	if got, err := hotKeyLoop(t, "\x10", cmds).readHotKeyInput(); err != nil || got != "\x10" {
		t.Errorf("^P = (%q, %v), want ^P", got, err)
	}
	// A key that starts a longer command falls back to a line.
	if got, err := hotKeyLoop(t, "10\r", cmds).readHotKeyInput(); err != nil || got != "10" {
		t.Errorf("multi-key = (%q, %v), want 10", got, err)
	}
	if got, err := hotKeyLoop(t, "/g\r", cmds).readHotKeyInput(); err != nil || strings.ToUpper(got) != "/G" {
		t.Errorf("/G = (%q, %v), want /G", got, err)
	}
}

// TestKonfigScreenHeightEditsAndRejects pins the Screen Height field: an
// in-range value is saved and applied, one out of range is refused with the
// allowed range and nothing saved.
func TestKonfigScreenHeightEditsAndRejects(t *testing.T) {
	env := newMenuEnv(t)

	env.runCmd("USERCONFIG", env.caller, "", "b"+keyClear+"99\rq")
	if got := env.mustDiskUser(env.caller.ID).ScreenHeight; got != 0 {
		t.Fatalf("out-of-range height saved as %d", got)
	}
	r := env.runCmd("USERCONFIG", env.caller, "", "b"+keyClear+"30\rq")
	if !r.has("Screen Height is now 30") {
		t.Errorf("no saved notice:\n%s", r.text())
	}
	if got := env.mustDiskUser(env.caller.ID).ScreenHeight; got != 30 {
		t.Errorf("saved height = %d, want 30", got)
	}
}

// TestKonfigHeaderStylePicksAndSaves pins the Header Style field: it opens
// the MSGHDR picker, and the style picked there (Down, Space) is saved and
// named in the form.
func TestKonfigHeaderStylePicksAndSaves(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("USERCONFIG", env.caller, "", "e"+keyDown+" q")
	if got := env.mustDiskUser(env.caller.ID).MsgHdr; got != 2 {
		t.Fatalf("saved MsgHdr = %d, want 2\n%s", got, r.text())
	}
	if !r.has("Generic Blue Box") {
		t.Errorf("style name not shown:\n%s", r.text())
	}
}

// TestKonfigAutoSigCreateEditDelete pins the Auto-Signature field: with none
// set it opens the editor and saves what was typed; with one set, D deletes
// it and Esc at the choice leaves it alone.
func TestKonfigAutoSigCreateEditDelete(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("USERCONFIG", env.caller, "", "fline one\rline two\x1aq")
	if got := env.mustDiskUser(env.caller.ID).AutoSignature; got != "line one\nline two" {
		t.Fatalf("saved sig = %q\n%s", got, r.text())
	}
	if !r.has("Auto-Signature updated.", "2 lines") {
		t.Errorf("status or value missing:\n%s", r.text())
	}

	env.caller = env.mustDiskUser(env.caller.ID)
	env.runCmd("USERCONFIG", env.caller, "", "f"+keyEsc+"q")
	if got := env.mustDiskUser(env.caller.ID).AutoSignature; got == "" {
		t.Fatal("Esc at the choice deleted the signature")
	}
	r = env.runCmd("USERCONFIG", env.caller, "", "fdq")
	if got := env.mustDiskUser(env.caller.ID).AutoSignature; got != "" {
		t.Errorf("sig = %q after delete", got)
	}
	if !r.has("Auto-Signature deleted.") {
		t.Errorf("no delete notice:\n%s", r.text())
	}
}

// TestKonfigAutoSigTruncatesAndAbandons pins that a signature longer than
// the line limit is cut and the cut is reported, and that leaving the editor
// without saving (Ctrl-A, Y) changes nothing.
func TestKonfigAutoSigTruncatesAndAbandons(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("USERCONFIG", env.caller, "", "f1\r2\r3\r4\r5\r6\r7\x1aq")
	if got := env.mustDiskUser(env.caller.ID).AutoSignature; got != "1\n2\n3\n4\n5" {
		t.Fatalf("saved sig = %q, want the first 5 lines", got)
	}
	if !r.has("kept to its first 5 lines") {
		t.Errorf("no truncation notice:\n%s", r.text())
	}

	env.caller = env.mustDiskUser(env.caller.ID)
	r = env.runCmd("USERCONFIG", env.caller, "", "feEXTRA\x01Yq")
	if got := env.mustDiskUser(env.caller.ID).AutoSignature; got != "1\n2\n3\n4\n5" {
		t.Errorf("abandoned edit changed sig to %q\n%s", got, r.text())
	}
	if !r.has("Auto-Signature not changed.") {
		t.Errorf("no not-changed notice:\n%s", r.text())
	}
}
