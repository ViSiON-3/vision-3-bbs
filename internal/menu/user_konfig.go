package menu

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// USERCONFIG is the full-screen "K" user Konfig editor. It replaced the old
// USERCFG menu of one-shot CFG_* commands with a single form: an ANSI header
// (KONFIG.ANS from the menu set), every setting a caller can change laid out
// in two columns with its live value, and an editor suited to each field.
//
// Every change is written as soon as it is confirmed, so a dropped carrier
// never loses an edit. Each individual edit can be abandoned with Esc.
//
// Only settings the board actually honours are offered. User.OutputMode,
// MorePrompts, CustomPrompt and Colors are stored but nothing reads them, so
// they stay out of the form until something does.

// konfigTone picks the colour a value is drawn in.
type konfigTone int

const (
	toneValue konfigTone = iota // an ordinary set value
	toneDim                     // unset / default / "(none)"
	toneOn                      // a switch that is on
	toneOff                     // a switch that is off
)

// konfigValue is a field's display value, uncoloured, plus its tone.
type konfigValue struct {
	text string
	tone konfigTone
}

// konfigItem is one row of the form.
type konfigItem struct {
	key   byte   // hotkey, upper case
	label string // shown left of the value
	help  string // shown on the help row while the item is highlighted
	// flips reports that activating the item switches it in place rather
	// than opening an editor; only the key legend uses it.
	flips bool
	value func(st *konfigState) konfigValue
	// edit performs the change. It returns an error only when the session
	// should end (disconnect, idle timeout); everything the caller needs to
	// know otherwise goes in st.status.
	edit func(st *konfigState) error

	// Screen position, filled in by layoutKonfig.
	row, col, column int
}

// konfigSection is a titled group of items in one column.
type konfigSection struct {
	title string
	items []*konfigItem
}

// konfigHeading is a section title's screen position.
type konfigHeading struct {
	row, col int
	title    string
}

// Screen geometry. Everything fits in 21 rows, the smallest height the
// board allows, so the form draws whatever size the caller has set.
const (
	konfigHeaderRows = 5                    // rows reserved for KONFIG.ANS
	konfigTopRow     = konfigHeaderRows + 2 // one blank row under the art
	konfigFormRows   = 10                   // two sections a column, with their gaps
	konfigColWidth   = 38
	konfigLeftCol    = 2
	konfigRightCol   = 42
	konfigKeyWidth   = 4  // "[A] "
	konfigLabelWidth = 16 // label plus gap
	konfigValueWidth = konfigColWidth - konfigKeyWidth - konfigLabelWidth
	konfigRuleRow    = konfigTopRow + konfigFormRows
	konfigHelpRow    = konfigRuleRow + 1
	konfigEditRow    = konfigHelpRow + 1
	konfigLegendRow  = konfigEditRow + 2
	konfigLastRow    = konfigLegendRow
)

// Limits shared with new-user signup and the old CFG_* commands.
const (
	konfigMinWidth    = 40
	konfigMaxWidth    = 255
	konfigMinHeight   = 21
	konfigMaxHeight   = 60
	konfigMinPassword = 3
	// konfigMaxPassword is bcrypt's limit in bytes. Login takes passwords of
	// any length, but none longer than this can have been stored.
	konfigMaxPassword = 72
)

// konfigState is one run of the editor.
type konfigState struct {
	c        *cmdCtx
	ih       *editor.InputHandler
	items    []*konfigItem
	headings []konfigHeading
	sel      int
	status   string
	// hdrNames caches the header style names; see headerStyleNames.
	hdrNames map[string]string
	// redraw asks for a full repaint after the current edit, for editors
	// that take over the screen (the message editor, the header picker, the
	// file-column box).
	redraw bool
}

func (st *konfigState) user() *user.User { return st.c.currentUser }

// runUserKonfig is the USERCONFIG runnable. An argument names the menu to
// GOTO on exit; without one, control returns to the calling menu.
func runUserKonfig(c *cmdCtx, args string) (*user.User, string, error) {
	if c.currentUser == nil || c.userManager == nil {
		return nil, "", nil
	}

	// Work on a copy of the context: edits update the size it carries.
	ctx := *c
	st := &konfigState{c: &ctx, ih: getSessionIH(c.s)}
	st.items, st.headings = layoutKonfig(konfigSections())

	hidden := c.e.hideCursorIfNeeded(c.terminal, c.outputMode, cursorHideContextDefault)
	defer c.e.showCursorIfHidden(c.terminal, c.outputMode, hidden)

	next := ""
	if target := strings.TrimSpace(args); target != "" {
		next = "GOTO:" + strings.ToUpper(target)
	}

	if err := st.renderAll(); err != nil {
		return st.user(), "", err
	}
	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			return st.user(), "", err
		}
		done, err := st.handleKey(key)
		if err != nil {
			return st.user(), "", err
		}
		if done {
			return st.user(), next, nil
		}
	}
}

// handleKey applies one keystroke to the form. done reports that the caller
// asked to leave.
func (st *konfigState) handleKey(key int) (done bool, err error) {
	switch key {
	case editor.KeyEsc, 'q', 'Q':
		return true, nil
	case editor.KeyArrowUp:
		st.moveTo(st.sel - 1)
	case editor.KeyArrowDown:
		st.moveTo(st.sel + 1)
	case editor.KeyArrowLeft, editor.KeyArrowRight, editor.KeyTab:
		st.moveTo(st.acrossFrom(st.sel))
	case editor.KeyHome, editor.KeyPageUp:
		st.moveTo(0)
	case editor.KeyEnd, editor.KeyPageDown:
		st.moveTo(len(st.items) - 1)
	case editor.KeyEnter, ' ':
		return false, st.activate(st.sel)
	default:
		if idx := st.itemForKey(key); idx >= 0 {
			st.moveTo(idx)
			return false, st.activate(idx)
		}
	}
	return false, nil
}

// itemForKey returns the index of the item bound to key, or -1.
func (st *konfigState) itemForKey(key int) int {
	if key < 0 || key > 127 {
		return -1
	}
	k := byte(key)
	if k >= 'a' && k <= 'z' {
		k -= 'a' - 'A'
	}
	for i, it := range st.items {
		if it.key == k {
			return i
		}
	}
	return -1
}

// moveTo highlights item i, wrapping past either end.
func (st *konfigState) moveTo(i int) {
	n := len(st.items)
	i = ((i % n) + n) % n
	if i == st.sel {
		return
	}
	prev := st.sel
	st.sel = i
	st.status = ""
	_ = st.renderItem(prev)
	_ = st.renderItem(i)
	_ = st.renderHelp()
	_ = st.renderStatus()
}

// acrossFrom returns the item in the other column nearest to item i's row.
func (st *konfigState) acrossFrom(i int) int {
	from := st.items[i]
	best, bestDist := i, -1
	for j, it := range st.items {
		if it.column == from.column {
			continue
		}
		d := it.row - from.row
		if d < 0 {
			d = -d
		}
		if bestDist < 0 || d < bestDist {
			best, bestDist = j, d
		}
	}
	return best
}

// activate runs item i's editor and repaints what it changed.
func (st *konfigState) activate(i int) error {
	st.status = ""
	st.redraw = false
	if err := st.items[i].edit(st); err != nil {
		return err
	}
	if st.redraw {
		return st.renderAll()
	}
	if err := st.renderItem(i); err != nil {
		return err
	}
	return st.renderStatus()
}

// commit applies mutate to the caller's record and saves it, running undo if
// the save fails so the session never disagrees with users.json.
func (st *konfigState) commit(what string, mutate, undo func(u *user.User)) bool {
	u := st.user()
	mutate(u)
	if err := st.c.userManager.UpdateUser(u); err != nil {
		undo(u)
		slog.Error("failed to save user setting", "node", st.c.nodeNumber, "field", what, "error", err)
		st.status = fmt.Sprintf("|12Couldn't save %s. Please try again.|07", what)
		return false
	}
	return true
}

// saved sets the standard confirmation for a changed setting.
func (st *konfigState) saved(label, value string) {
	st.status = fmt.Sprintf("|10Saved.|07 %s is now |15%s|07.", label, value)
}

// --- Field definitions -----------------------------------------------------

func konfigSections() [2][]konfigSection {
	return [2][]konfigSection{
		{
			{title: "Terminal", items: []*konfigItem{
				{key: 'A', label: "Screen Width",
					help:  "Columns your terminal shows (40-255). Used for layout and word wrap.",
					value: func(st *konfigState) konfigValue { return numValue(st.user().ScreenWidth, 80) },
					edit:  editScreenWidth},
				{key: 'B', label: "Screen Height",
					help:  "Rows your terminal shows (21-60). Used for paging and full-screen views.",
					value: func(st *konfigState) konfigValue { return numValue(st.user().ScreenHeight, 25) },
					edit:  editScreenHeight},
				{key: 'C', label: "Encoding", flips: true,
					help:  "CP437 for SyncTERM, NetRunner and friends; UTF-8 for modern terminals.",
					value: encodingValue,
					edit:  toggleEncoding},
				{key: 'D', label: "Hot Keys", flips: true,
					help:  "On: menu commands run the moment you press their key, no Enter needed.",
					value: func(st *konfigState) konfigValue { return onOffValue(st.user().HotKeys) },
					edit:  toggleHotKeys},
			}},
			{title: "Messages", items: []*konfigItem{
				{key: 'E', label: "Header Style",
					help:  "How message headers look in the reader. Opens the style picker.",
					value: headerStyleValue,
					edit:  editHeaderStyle},
				{key: 'F', label: "Auto-Signature",
					help:  "Up to 5 lines added to the end of every message you post.",
					value: autoSigValue,
					edit:  editAutoSig},
			}},
		},
		{
			{title: "Personal", items: []*konfigItem{
				{key: 'G', label: "Real Name",
					help:  "Used in areas that require real names. First and last name.",
					value: func(st *konfigState) konfigValue { return textValue(st.user().RealName) },
					edit:  editRealName},
				{key: 'H', label: "Location",
					help:  "Your group or location, shown in user lists and last callers.",
					value: func(st *konfigState) konfigValue { return textValue(st.user().GroupLocation) },
					edit:  editLocation},
				{key: 'I', label: "User Note",
					help:  "A short line about you, shown in user lists and last callers.",
					value: func(st *konfigState) konfigValue { return textValue(st.user().PrivateNote) },
					edit:  editNote},
				{key: 'J', label: "Password",
					help:  "Change your login password. You'll need your current one.",
					value: func(*konfigState) konfigValue { return konfigValue{"********", toneDim} },
					edit:  editPassword},
			}},
			{title: "Files", items: []*konfigItem{
				{key: 'K', label: "Listing Mode", flips: true,
					help:  "Lightbar: arrow-key file browser. Classic: scrolling text list.",
					value: listingModeValue,
					edit:  toggleListingMode},
				{key: 'L', label: "File Columns",
					help:  "Choose which columns the file listing shows.",
					value: fileColumnsValue,
					edit:  editFileColumns},
			}},
		},
	}
}

// layoutKonfig assigns screen positions and flattens the items in hotkey
// order: down the left column, then down the right.
func layoutKonfig(cols [2][]konfigSection) ([]*konfigItem, []konfigHeading) {
	var items []*konfigItem
	var headings []konfigHeading
	for c, sections := range cols {
		col := konfigLeftCol
		if c == 1 {
			col = konfigRightCol
		}
		row := konfigTopRow
		for _, sec := range sections {
			headings = append(headings, konfigHeading{row: row, col: col, title: sec.title})
			row++
			for _, it := range sec.items {
				it.row, it.col, it.column = row, col, c
				items = append(items, it)
				row++
			}
			row++ // gap between sections
		}
	}
	return items, headings
}

// --- Values ----------------------------------------------------------------

func numValue(v, def int) konfigValue {
	if v == 0 {
		return konfigValue{strconv.Itoa(def) + " (default)", toneDim}
	}
	return konfigValue{strconv.Itoa(v), toneValue}
}

func textValue(s string) konfigValue {
	if strings.TrimSpace(s) == "" {
		return konfigValue{"(not set)", toneDim}
	}
	return konfigValue{s, toneValue}
}

func onOffValue(on bool) konfigValue {
	if on {
		return konfigValue{"On", toneOn}
	}
	return konfigValue{"Off", toneOff}
}

// effectiveEncoding is the encoding the caller has chosen, or the one this
// session negotiated when they have not chosen.
func (st *konfigState) effectiveEncoding() (enc string, chosen bool) {
	switch st.user().PreferredEncoding {
	case "utf8", "cp437":
		return st.user().PreferredEncoding, true
	}
	if st.c.outputMode == ansi.OutputModeUTF8 {
		return "utf8", false
	}
	return "cp437", false
}

// fileListModeDisplay names a file listing mode for display. Anything but
// "classic" is the lightbar browser.
func fileListModeDisplay(mode string) string {
	if strings.EqualFold(mode, "classic") {
		return "Classic"
	}
	return "Lightbar"
}

func encodingName(enc string) string {
	if enc == "utf8" {
		return "UTF-8"
	}
	return "CP437"
}

func encodingValue(st *konfigState) konfigValue {
	enc, chosen := st.effectiveEncoding()
	if !chosen {
		return konfigValue{encodingName(enc) + " (auto)", toneDim}
	}
	return konfigValue{encodingName(enc), toneValue}
}

// effectiveListingMode resolves the caller's file listing mode against the
// board default, the same way SELECTFILEAREA does.
func (st *konfigState) effectiveListingMode() (mode string, chosen bool) {
	mode = st.user().FileListingMode
	chosen = mode != ""
	if !chosen {
		mode = st.c.e.GetServerConfig().FileListingMode
	}
	if strings.EqualFold(mode, "classic") {
		return "classic", chosen
	}
	return "lightbar", chosen
}

func listingModeValue(st *konfigState) konfigValue {
	mode, chosen := st.effectiveListingMode()
	if !chosen {
		return konfigValue{fileListModeDisplay(mode) + " (default)", toneDim}
	}
	return konfigValue{fileListModeDisplay(mode), toneValue}
}

func headerStyleValue(st *konfigState) konfigValue {
	n := st.user().MsgHdr
	if n <= 0 {
		return konfigValue{"(not chosen)", toneDim}
	}
	if name := st.headerStyleNames()[strconv.Itoa(n)]; name != "" {
		return konfigValue{name, toneValue}
	}
	return konfigValue{fmt.Sprintf("Style %d", n), toneValue}
}

// headerStyleNames maps each MSGHDR.BAR return value to its display text,
// read once per visit. It reads the file directly rather than through
// loadLightbarOptions, which checks every hotkey against a MSGHDR.CFG that
// does not exist and would log a warning per style on every repaint.
func (st *konfigState) headerStyleNames() map[string]string {
	if st.hdrNames != nil {
		return st.hdrNames
	}
	st.hdrNames = map[string]string{}
	data, err := os.ReadFile(st.c.e.menuFile("bar", "MSGHDR.BAR"))
	if err != nil {
		return st.hdrNames
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if parts := strings.SplitN(line, ",", 7); len(parts) == 7 {
			st.hdrNames[strings.TrimSpace(parts[5])] = strings.TrimSpace(parts[6])
		}
	}
	return st.hdrNames
}

func autoSigValue(st *konfigState) konfigValue {
	sig := strings.TrimRight(st.user().AutoSignature, "\r\n")
	if sig == "" {
		return konfigValue{"(none)", toneDim}
	}
	n := strings.Count(sig, "\n") + 1
	if n == 1 {
		return konfigValue{"1 line", toneValue}
	}
	return konfigValue{fmt.Sprintf("%d lines", n), toneValue}
}

// fileColumns lists the file-listing columns in display order, with the
// hotkey the column box uses for each.
var fileColumns = []struct {
	key   byte
	label string
	get   func(u *user.User) *bool
}{
	{'N', "Name", func(u *user.User) *bool { return &u.FileListColumns.Name }},
	{'S', "Size", func(u *user.User) *bool { return &u.FileListColumns.Size }},
	{'D', "Date", func(u *user.User) *bool { return &u.FileListColumns.Date }},
	{'L', "Downloads", func(u *user.User) *bool { return &u.FileListColumns.Downloads }},
	{'U', "Uploader", func(u *user.User) *bool { return &u.FileListColumns.Uploader }},
	{'E', "Description", func(u *user.User) *bool { return &u.FileListColumns.Description }},
}

// fileColumnsAllDefault reports the "nothing chosen" state, in which the
// file lister shows every column (see showFileColumn).
func fileColumnsAllDefault(u *user.User) bool {
	for _, fc := range fileColumns {
		if *fc.get(u) {
			return false
		}
	}
	return true
}

func fileColumnsValue(st *konfigState) konfigValue {
	u := st.user()
	if fileColumnsAllDefault(u) {
		return konfigValue{"All", toneDim}
	}
	shown := 0
	for _, fc := range fileColumns {
		if *fc.get(u) {
			shown++
		}
	}
	if shown == len(fileColumns) {
		return konfigValue{"All", toneValue}
	}
	return konfigValue{fmt.Sprintf("%d of %d", shown, len(fileColumns)), toneValue}
}

// --- Toggles ---------------------------------------------------------------

func toggleHotKeys(st *konfigState) error {
	old := st.user().HotKeys
	if st.commit("Hot Keys",
		func(u *user.User) { u.HotKeys = !old },
		func(u *user.User) { u.HotKeys = old }) {
		st.saved("Hot Keys", onOffValue(!old).text)
	}
	return nil
}

func toggleEncoding(st *konfigState) error {
	cur, _ := st.effectiveEncoding()
	next := "utf8"
	if cur == "utf8" {
		next = "cp437"
	}
	old := st.user().PreferredEncoding
	if st.commit("Encoding",
		func(u *user.User) { u.PreferredEncoding = next },
		func(u *user.User) { u.PreferredEncoding = old }) {
		st.status = fmt.Sprintf("|10Saved.|07 Encoding is now |15%s|07, starting with your next login.", encodingName(next))
	}
	return nil
}

func toggleListingMode(st *konfigState) error {
	cur, _ := st.effectiveListingMode()
	next := "classic"
	if cur == "classic" {
		next = "lightbar"
	}
	old := st.user().FileListingMode
	if st.commit("Listing Mode",
		func(u *user.User) { u.FileListingMode = next },
		func(u *user.User) { u.FileListingMode = old }) {
		st.saved("Listing Mode", fileListModeDisplay(next))
	}
	return nil
}

// --- Line-edited fields ----------------------------------------------------

func editScreenWidth(st *konfigState) error {
	return st.editNumber("Screen Width", konfigMinWidth, konfigMaxWidth,
		func(u *user.User) *int { return &u.ScreenWidth }, 80)
}

func editScreenHeight(st *konfigState) error {
	return st.editNumber("Screen Height", konfigMinHeight, konfigMaxHeight,
		func(u *user.User) *int { return &u.ScreenHeight }, 25)
}

// editNumber edits a terminal dimension and applies it to the rest of the
// session straight away, as login does with a stored size.
func (st *konfigState) editNumber(label string, lo, hi int, field func(u *user.User) *int, def int) error {
	cur := *field(st.user())
	if cur == 0 {
		cur = def
	}
	input, ok, err := st.readField(label, strconv.Itoa(cur), 3, false, fmt.Sprintf("|08(%d-%d)", lo, hi))
	if err != nil || !ok {
		return err
	}
	v, convErr := strconv.Atoi(strings.TrimSpace(input))
	if convErr != nil || v < lo || v > hi {
		st.status = fmt.Sprintf("|12%s must be a number from %d to %d.|07", label, lo, hi)
		return nil
	}
	old := *field(st.user())
	if old == v {
		return nil
	}
	if !st.commit(label,
		func(u *user.User) { *field(u) = v },
		func(u *user.User) { *field(u) = old }) {
		return nil
	}
	st.applyTermSize()
	st.saved(label, strconv.Itoa(v))
	return nil
}

// applyTermSize makes the stored size the session's size for everything
// that runs after the editor, not only for the next login.
func (st *konfigState) applyTermSize() {
	u := st.user()
	w, h := st.c.termWidth, st.c.termHeight
	if u.ScreenWidth > 0 {
		w = u.ScreenWidth
	}
	if u.ScreenHeight > 0 {
		h = u.ScreenHeight
	}
	st.c.termWidth, st.c.termHeight = w, h
	setSessionTermSize(st.c.s, w, h)
	_ = st.c.terminal.SetSize(w, h)
}

func editRealName(st *konfigState) error {
	return st.editText("Real Name", newUserRealNameMaxLen,
		func(u *user.User) *string { return &u.RealName }, user.ValidateRealName)
}

func editLocation(st *konfigState) error {
	return st.editText("Location", newUserLocationMaxLen,
		func(u *user.User) *string { return &u.GroupLocation }, nil)
}

func editNote(st *konfigState) error {
	return st.editText("User Note", newUserNoteMaxLen,
		func(u *user.User) *string { return &u.PrivateNote }, nil)
}

// editText edits a single-line text field. An emptied field clears the
// value, subject to validate.
func (st *konfigState) editText(label string, maxLen int, field func(u *user.User) *string, validate func(string) error) error {
	old := *field(st.user())
	// Never shorten a value just by opening it: older editors allowed longer
	// ones (40-rune names, 35-rune notes) than new-user signup does.
	if n := utf8.RuneCountInString(old); n > maxLen {
		maxLen = n
	}
	input, ok, err := st.readField(label, old, maxLen, false, "")
	if err != nil || !ok {
		return err
	}
	v := strings.TrimSpace(input)
	if v == old {
		return nil
	}
	if validate != nil {
		if vErr := validate(v); vErr != nil {
			st.status = "|12" + capitalizeFirst(vErr.Error()) + ".|07"
			return nil
		}
	}
	if st.commit(label,
		func(u *user.User) { *field(u) = v },
		func(u *user.User) { *field(u) = old }) {
		if v == "" {
			st.status = fmt.Sprintf("|10Saved.|07 %s cleared.", label)
		} else {
			st.status = fmt.Sprintf("|10Saved.|07 %s updated.", label)
		}
	}
	return nil
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func editPassword(st *konfigState) error {
	current, ok, err := st.readField("Current password", "", konfigMaxPassword, true, "")
	if err != nil || !ok {
		return err
	}
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(st.user().PasswordHash), []byte(current)); bcryptErr != nil {
		st.status = "|12Incorrect password.|07 Nothing was changed."
		return nil
	}
	newPw, ok, err := st.readField("New password", "", konfigMaxPassword, true, "")
	if err != nil || !ok {
		return err
	}
	if len([]rune(newPw)) < konfigMinPassword {
		st.status = fmt.Sprintf("|12Passwords must be at least %d characters.|07 Nothing was changed.", konfigMinPassword)
		return nil
	}
	if len(newPw) > konfigMaxPassword {
		st.status = fmt.Sprintf("|12Passwords can be at most %d bytes.|07 Nothing was changed.", konfigMaxPassword)
		return nil
	}
	confirm, ok, err := st.readField("Type it again", "", konfigMaxPassword, true, "")
	if err != nil || !ok {
		return err
	}
	if confirm != newPw {
		st.status = "|12The passwords didn't match.|07 Nothing was changed."
		return nil
	}
	hashBytes, hashErr := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if hashErr != nil {
		slog.Error("failed to hash new password", "node", st.c.nodeNumber, "error", hashErr)
		st.status = "|12Couldn't change your password. Please try again.|07"
		return nil
	}
	hashed := string(hashBytes)
	old := st.user().PasswordHash
	if st.commit("your password",
		func(u *user.User) { u.PasswordHash = hashed },
		func(u *user.User) { u.PasswordHash = old }) {
		st.status = "|10Saved.|07 Your password has been changed."
	}
	return nil
}

// --- Screen-taking editors -------------------------------------------------

func editHeaderStyle(st *konfigState) error {
	st.redraw = true
	old := st.user().MsgHdr
	u, _, err := runGetHeaderType(st.c, "")
	if err != nil {
		return err
	}
	if u != nil {
		st.c.currentUser = u
	}
	if st.user().MsgHdr != old {
		st.saved("Header Style", headerStyleValue(st).text)
	}
	return nil
}

func editAutoSig(st *konfigState) error {
	if strings.TrimSpace(st.user().AutoSignature) != "" {
		choice, err := st.readChoice("|15Auto-Signature|08: |08[|15E|08]|07dit  |08[|15D|08]|07elete  |08[|15Esc|08]|07 Cancel", "ED")
		if err != nil {
			return err
		}
		switch choice {
		case 'D':
			old := st.user().AutoSignature
			if st.commit("Auto-Signature",
				func(u *user.User) { u.AutoSignature = "" },
				func(u *user.User) { u.AutoSignature = old }) {
				st.status = "|10Saved.|07 Auto-Signature deleted."
			}
			return nil
		case 'E':
		default:
			return nil
		}
	}

	st.redraw = true
	body, saved, truncated, err := runAutoSigEditor(st.c, st.user())
	if err != nil {
		if errors.Is(err, errAutoSigEditorFailed) {
			st.status = "|12The editor couldn't start.|07 Nothing was changed."
			return nil
		}
		return err
	}
	if !saved {
		st.status = "|07Auto-Signature not changed."
		return nil
	}
	old := st.user().AutoSignature
	if !st.commit("Auto-Signature",
		func(u *user.User) { u.AutoSignature = body },
		func(u *user.User) { u.AutoSignature = old }) {
		return nil
	}
	switch {
	case body == "":
		st.status = "|10Saved.|07 Auto-Signature cleared."
	case truncated:
		st.status = fmt.Sprintf("|10Saved.|07 Auto-Signature kept to its first %d lines.", maxAutoSigLines)
	default:
		st.status = "|10Saved.|07 Auto-Signature updated."
	}
	return nil
}
