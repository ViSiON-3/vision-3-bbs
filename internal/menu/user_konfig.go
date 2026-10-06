package menu

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
// All of its text comes from the konfig* strings in strings.json.
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
	redraw        bool
	physicalWidth int
	resizeUpdates <-chan struct{}
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
	st.physicalWidth = st.reportedWidth()
	// main owns the PTY resize channel and updates the shared physical width.
	// Poll that state while awaiting keys; never compete for its events.
	stopWatching := st.watchWidth()
	defer stopWatching()
	st.relayout()

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
		key, err := st.readKey(nil)
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
	if st.width() < 80 {
		return (i + 1) % len(st.items)
	}
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
		st.status = fmt.Sprintf(st.c.e.Strings().KonfigSaveError, what)
		return false
	}
	return true
}

// saved sets the standard confirmation for a changed setting.
func (st *konfigState) saved(label, value string) {
	st.status = fmt.Sprintf(st.c.e.Strings().KonfigSavedFormat, label, value)
}

// --- Field definitions -----------------------------------------------------

func konfigSections(str *config.StringsConfig) [2][]konfigSection {
	return [2][]konfigSection{
		{
			{title: str.KonfigSectionTerminal, items: []*konfigItem{
				{key: 'A', label: str.KonfigScreenWidthLabel,
					help:  str.KonfigScreenWidthHelp,
					value: func(st *konfigState) konfigValue { return st.numValue(st.user().ScreenWidth, 80) },
					edit:  editScreenWidth},
				{key: 'B', label: str.KonfigScreenHeightLabel,
					help:  str.KonfigScreenHeightHelp,
					value: func(st *konfigState) konfigValue { return st.numValue(st.user().ScreenHeight, 25) },
					edit:  editScreenHeight},
				{key: 'C', label: str.KonfigEncodingLabel, flips: true,
					help:  str.KonfigEncodingHelp,
					value: encodingValue,
					edit:  toggleEncoding},
				{key: 'D', label: str.KonfigHotKeysLabel, flips: true,
					help:  str.KonfigHotKeysHelp,
					value: func(st *konfigState) konfigValue { return st.onOffValue(st.user().HotKeys) },
					edit:  toggleHotKeys},
			}},
			{title: str.KonfigSectionMessages, items: []*konfigItem{
				{key: 'E', label: str.KonfigHeaderStyleLabel,
					help:  str.KonfigHeaderStyleHelp,
					value: headerStyleValue,
					edit:  editHeaderStyle},
				{key: 'F', label: str.KonfigAutoSigLabel,
					help:  str.KonfigAutoSigHelp,
					value: autoSigValue,
					edit:  editAutoSig},
			}},
		},
		{
			{title: str.KonfigSectionPersonal, items: []*konfigItem{
				{key: 'G', label: str.KonfigRealNameLabel,
					help:  str.KonfigRealNameHelp,
					value: func(st *konfigState) konfigValue { return st.textValue(st.user().RealName) },
					edit:  editRealName},
				{key: 'H', label: str.KonfigLocationLabel,
					help:  str.KonfigLocationHelp,
					value: func(st *konfigState) konfigValue { return st.textValue(st.user().GroupLocation) },
					edit:  editLocation},
				{key: 'I', label: str.KonfigUserNoteLabel,
					help:  str.KonfigUserNoteHelp,
					value: func(st *konfigState) konfigValue { return st.textValue(st.user().PrivateNote) },
					edit:  editNote},
				{key: 'J', label: str.KonfigPasswordLabel,
					help:  str.KonfigPasswordHelp,
					value: func(*konfigState) konfigValue { return konfigValue{"********", toneDim} },
					edit:  editPassword},
			}},
			{title: str.KonfigSectionFiles, items: []*konfigItem{
				{key: 'K', label: str.KonfigListingModeLabel, flips: true,
					help:  str.KonfigListingModeHelp,
					value: listingModeValue,
					edit:  toggleListingMode},
				{key: 'L', label: str.KonfigFileColumnsLabel,
					help:  str.KonfigFileColumnsHelp,
					value: fileColumnsValue,
					edit:  editFileColumns},
			}},
		},
	}
}

// layoutKonfig assigns screen positions and flattens the items in hotkey
// order: down the left column, then down the right.
func layoutKonfig(cols [2][]konfigSection, widths ...int) ([]*konfigItem, []konfigHeading) {
	compact := len(widths) > 0 && widths[0] < 80
	compactRow := 2
	var items []*konfigItem
	var headings []konfigHeading
	for c, sections := range cols {
		col := konfigLeftCol
		if c == 1 {
			col = konfigRightCol
		}
		row := konfigTopRow
		if compact {
			col, row = konfigLeftCol, compactRow
		}
		for _, sec := range sections {
			headings = append(headings, konfigHeading{row: row, col: col, title: sec.title})
			row++
			for _, it := range sec.items {
				it.row, it.col, it.column = row, col, c
				items = append(items, it)
				row++
			}
			if !compact {
				row++
			} // gap between sections
		}
		compactRow = row
	}
	return items, headings
}

// --- Values ----------------------------------------------------------------

func (st *konfigState) numValue(v, def int) konfigValue {
	if v == 0 {
		return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigDefaultFormat, strconv.Itoa(def)), toneDim}
	}
	return konfigValue{strconv.Itoa(v), toneValue}
}

func (st *konfigState) textValue(s string) konfigValue {
	if strings.TrimSpace(s) == "" {
		return konfigValue{st.c.e.Strings().KonfigNotSet, toneDim}
	}
	return konfigValue{s, toneValue}
}

func (st *konfigState) onOffValue(on bool) konfigValue {
	if on {
		return konfigValue{st.c.e.Strings().KonfigOn, toneOn}
	}
	return konfigValue{st.c.e.Strings().KonfigOff, toneOff}
}

// sessionEncoding is the encoding this session is using.
func (st *konfigState) sessionEncoding() string {
	if st.c.outputMode == ansi.OutputModeUTF8 {
		return "utf8"
	}
	return "cp437"
}

// fileListModeDisplay names a file listing mode for display. Anything but
// "classic" is the lightbar browser.
func (st *konfigState) fileListModeDisplay(mode string) string {
	if strings.EqualFold(mode, "classic") {
		return st.c.e.Strings().KonfigClassic
	}
	return st.c.e.Strings().KonfigLightbar
}

func encodingName(enc string) string {
	if enc == "utf8" {
		return "UTF-8"
	}
	return "CP437"
}

func encodingValue(st *konfigState) konfigValue {
	switch enc := st.user().PreferredEncoding; enc {
	case "utf8", "cp437":
		return konfigValue{encodingName(enc), toneValue}
	}
	return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigAutoFormat, encodingName(st.sessionEncoding())), toneDim}
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
		return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigDefaultFormat, st.fileListModeDisplay(mode)), toneDim}
	}
	return konfigValue{st.fileListModeDisplay(mode), toneValue}
}

func headerStyleValue(st *konfigState) konfigValue {
	n := st.user().MsgHdr
	if n <= 0 {
		return konfigValue{st.c.e.Strings().KonfigNotChosen, toneDim}
	}
	if name := st.headerStyleNames()[strconv.Itoa(n)]; name != "" {
		return konfigValue{name, toneValue}
	}
	return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigStyleFormat, n), toneValue}
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
		return konfigValue{st.c.e.Strings().KonfigNone, toneDim}
	}
	n := strings.Count(sig, "\n") + 1
	if n == 1 {
		return konfigValue{st.c.e.Strings().KonfigOneLine, toneValue}
	}
	return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigLinesFormat, n), toneValue}
}

// fileColumns lists the file-listing columns in display order, with the
// hotkey the column box uses for each.
var fileColumns = []struct {
	key   byte
	label func(str *config.StringsConfig) string
	get   func(u *user.User) *bool
}{
	{'N', func(str *config.StringsConfig) string { return str.KonfigColumnName }, func(u *user.User) *bool { return &u.FileListColumns.Name }},
	{'S', func(str *config.StringsConfig) string { return str.KonfigColumnSize }, func(u *user.User) *bool { return &u.FileListColumns.Size }},
	{'D', func(str *config.StringsConfig) string { return str.KonfigColumnDate }, func(u *user.User) *bool { return &u.FileListColumns.Date }},
	{'L', func(str *config.StringsConfig) string { return str.KonfigColumnDownloads }, func(u *user.User) *bool { return &u.FileListColumns.Downloads }},
	{'U', func(str *config.StringsConfig) string { return str.KonfigColumnUploader }, func(u *user.User) *bool { return &u.FileListColumns.Uploader }},
	{'E', func(str *config.StringsConfig) string { return str.KonfigColumnDescription }, func(u *user.User) *bool { return &u.FileListColumns.Description }},
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
		return konfigValue{st.c.e.Strings().KonfigAll, toneDim}
	}
	shown := 0
	for _, fc := range fileColumns {
		if *fc.get(u) {
			shown++
		}
	}
	if shown == len(fileColumns) {
		return konfigValue{st.c.e.Strings().KonfigAll, toneValue}
	}
	return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigCountFormat, shown, len(fileColumns)), toneValue}
}

// --- Toggles ---------------------------------------------------------------

func toggleHotKeys(st *konfigState) error {
	old := st.user().HotKeys
	label := st.c.e.Strings().KonfigHotKeysLabel
	if st.commit(label,
		func(u *user.User) { u.HotKeys = !old },
		func(u *user.User) { u.HotKeys = old }) {
		st.saved(label, st.onOffValue(!old).text)
	}
	return nil
}

// toggleEncoding cycles Auto, the encoding this session is using, the other
// one, and back to Auto, so the first press never changes what the caller
// sees. Auto detects the encoding on every call.
func toggleEncoding(st *konfigState) error {
	session := st.sessionEncoding()
	other := "cp437"
	if session == "cp437" {
		other = "utf8"
	}
	old := st.user().PreferredEncoding
	var next string
	switch old {
	case session:
		next = other
	case other:
		next = ""
	default:
		next = session
	}
	str := st.c.e.Strings()
	if !st.commit(str.KonfigEncodingLabel,
		func(u *user.User) { u.PreferredEncoding = next },
		func(u *user.User) { u.PreferredEncoding = old }) {
		return nil
	}
	switch next {
	case "":
		st.status = str.KonfigEncodingAuto
	case session:
		st.status = fmt.Sprintf(str.KonfigEncodingSaved, encodingName(next))
	default:
		st.status = fmt.Sprintf(str.KonfigEncodingMismatch, encodingName(session), encodingName(next))
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
	label := st.c.e.Strings().KonfigListingModeLabel
	if st.commit(label,
		func(u *user.User) { u.FileListingMode = next },
		func(u *user.User) { u.FileListingMode = old }) {
		st.saved(label, st.fileListModeDisplay(next))
	}
	return nil
}

// --- Line-edited fields ----------------------------------------------------

func editScreenWidth(st *konfigState) error {
	return st.editNumber(st.c.e.Strings().KonfigScreenWidthLabel, konfigMinWidth, konfigMaxWidth,
		func(u *user.User) *int { return &u.ScreenWidth }, 80)
}

func editScreenHeight(st *konfigState) error {
	return st.editNumber(st.c.e.Strings().KonfigScreenHeightLabel, konfigMinHeight, konfigMaxHeight,
		func(u *user.User) *int { return &u.ScreenHeight }, 25)
}

// editNumber edits a terminal dimension and applies it to the rest of the
// session straight away, as login does with a stored size.
func (st *konfigState) editNumber(label string, lo, hi int, field func(u *user.User) *int, def int) error {
	cur := *field(st.user())
	if cur == 0 {
		cur = def
	}
	str := st.c.e.Strings()
	input, ok, err := st.readField(label, strconv.Itoa(cur), 3, false, fmt.Sprintf(str.KonfigRangeHint, lo, hi))
	if err != nil || !ok {
		return err
	}
	v, convErr := strconv.Atoi(strings.TrimSpace(input))
	if convErr != nil || v < lo || v > hi {
		st.status = fmt.Sprintf(str.KonfigRangeError, label, lo, hi)
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
	st.relayout()
	st.redraw = true
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
	return st.editText(st.c.e.Strings().KonfigRealNameLabel, newUserRealNameMaxLen,
		func(u *user.User) *string { return &u.RealName }, user.ValidateRealName)
}

func editLocation(st *konfigState) error {
	return st.editText(st.c.e.Strings().KonfigLocationLabel, newUserLocationMaxLen,
		func(u *user.User) *string { return &u.GroupLocation }, nil)
}

func editNote(st *konfigState) error {
	return st.editText(st.c.e.Strings().KonfigUserNoteLabel, newUserNoteMaxLen,
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
	str := st.c.e.Strings()
	if validate != nil {
		if vErr := validate(v); vErr != nil {
			st.status = st.validationMessage(vErr)
			return nil
		}
	}
	if st.commit(label,
		func(u *user.User) { *field(u) = v },
		func(u *user.User) { *field(u) = old }) {
		if v == "" {
			st.status = fmt.Sprintf(str.KonfigClearedFormat, label)
		} else {
			st.status = fmt.Sprintf(str.KonfigUpdatedFormat, label)
		}
	}
	return nil
}

// validationMessage is the configured text for a validator's error. The real
// name is the only validated field.
func (st *konfigState) validationMessage(err error) string {
	str := st.c.e.Strings()
	switch {
	case errors.Is(err, user.ErrRealNameRequired):
		return str.KonfigRealNameRequired
	case errors.Is(err, user.ErrRealNameTooShort):
		return fmt.Sprintf(str.KonfigRealNameTooShort, user.RealNameMinLen)
	default:
		return str.KonfigRealNameNeedsSpace
	}
}

func editPassword(st *konfigState) error {
	str := st.c.e.Strings()
	current, ok, err := st.readField(str.KonfigPwCurrent, "", konfigMaxPassword, true, "")
	if err != nil || !ok {
		return err
	}
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(st.user().PasswordHash), []byte(current)); bcryptErr != nil {
		st.status = str.KonfigPwIncorrect
		return nil
	}
	newPw, ok, err := st.readField(str.KonfigPwNew, "", konfigMaxPassword, true, "")
	if err != nil || !ok {
		return err
	}
	if len([]rune(newPw)) < konfigMinPassword {
		st.status = fmt.Sprintf(str.KonfigPwTooShort, konfigMinPassword)
		return nil
	}
	if len(newPw) > konfigMaxPassword {
		st.status = fmt.Sprintf(str.KonfigPwTooLong, konfigMaxPassword)
		return nil
	}
	confirm, ok, err := st.readField(str.KonfigPwAgain, "", konfigMaxPassword, true, "")
	if err != nil || !ok {
		return err
	}
	if confirm != newPw {
		st.status = str.KonfigPwMismatch
		return nil
	}
	hashBytes, hashErr := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if hashErr != nil {
		slog.Error("failed to hash new password", "node", st.c.nodeNumber, "error", hashErr)
		st.status = str.KonfigPwFailed
		return nil
	}
	hashed := string(hashBytes)
	old := st.user().PasswordHash
	if st.commit(str.KonfigPasswordLabel,
		func(u *user.User) { u.PasswordHash = hashed },
		func(u *user.User) { u.PasswordHash = old }) {
		st.status = str.KonfigPwChanged
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
		st.saved(st.c.e.Strings().KonfigHeaderStyleLabel, headerStyleValue(st).text)
	}
	return nil
}

func editAutoSig(st *konfigState) error {
	str := st.c.e.Strings()
	label := str.KonfigAutoSigLabel
	if strings.TrimSpace(st.user().AutoSignature) != "" {
		choice, err := st.readChoice(str.KonfigAutoSigChoice, "ED")
		if err != nil {
			return err
		}
		switch choice {
		case 'D':
			old := st.user().AutoSignature
			if st.commit(label,
				func(u *user.User) { u.AutoSignature = "" },
				func(u *user.User) { u.AutoSignature = old }) {
				st.status = str.KonfigAutoSigDeleted
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
			st.status = str.KonfigAutoSigEditorFailed
			return nil
		}
		return err
	}
	if !saved {
		st.status = str.KonfigAutoSigUnchanged
		return nil
	}
	old := st.user().AutoSignature
	if !st.commit(label,
		func(u *user.User) { u.AutoSignature = body },
		func(u *user.User) { u.AutoSignature = old }) {
		return nil
	}
	switch {
	case body == "":
		st.status = str.KonfigAutoSigCleared
	case truncated:
		st.status = fmt.Sprintf(str.KonfigAutoSigTruncated, maxAutoSigLines)
	default:
		st.status = str.KonfigAutoSigUpdated
	}
	return nil
}

// width uses the real terminal when available, so an incorrect stored width
// cannot hide the setting needed to recover it. Wide screens retain the art
// and the original 80-column form.
func (st *konfigState) width() int {
	w := st.physicalWidth
	if w <= 0 {
		w = st.c.termWidth
	}
	if w <= 0 || w > 80 {
		w = 80
	}
	return w
}

// relayout rebuilds item and heading positions for the current terminal width.
func (st *konfigState) relayout() {
	st.items, st.headings = layoutKonfig(konfigSections(st.c.e.Strings()), st.width())
}

// ruleRow returns the separator row for the active layout.
func (st *konfigState) ruleRow() int {
	if st.width() < 80 {
		return 18
	}
	return konfigRuleRow
}
// helpRow returns the help row immediately below the separator.
func (st *konfigState) helpRow() int { return st.ruleRow() + 1 }
// editRow returns the input and status row below the help text.
func (st *konfigState) editRow() int { return st.ruleRow() + 2 }
// legendRow returns the exit-key legend row for the active layout.
func (st *konfigState) legendRow() int {
	if st.width() < 80 {
		return 21
	}
	return konfigLegendRow
}
// columnWidth returns the available item width for the active layout.
func (st *konfigState) columnWidth() int {
	if st.width() < 80 {
		return st.width() - 2
	}
	return konfigColWidth
}
// lineWidth returns the terminal width excluding the side margins.
func (st *konfigState) lineWidth() int { return st.width() - 2 }

// readKey keeps the shared session input handler and redraws the active
// editor after a PTY resize without losing its selection or edit buffer.
func (st *konfigState) readKey(redraw func() error) (int, error) {
	for {
		key, _, event, err := editor.ReadRawKeyOrEvent(st.ih, st.resizeUpdates)
		if err != nil {
			return 0, err
		}
		if !event {
			return key, nil
		}
		width := st.reportedWidth()
		if width == st.physicalWidth {
			continue
		}
		st.physicalWidth = width
		st.relayout()
		if err := st.renderAll(); err != nil {
			return 0, err
		}
		if redraw != nil {
			if err := redraw(); err != nil {
				return 0, err
			}
		}
	}
}

// reportedWidth follows the session handler's live physical width. The PTY
// snapshot is only a fallback for standalone callers without that registry.
func (st *konfigState) reportedWidth() int {
	if value, ok := terminalPhysicalWidths.Load(st.c.terminal); ok {
		if width := int(value.(*atomic.Int32).Load()); width > 0 {
			return width
		}
	}
	if pty, _, ok := st.c.s.Pty(); ok && pty.Window.Width > 0 {
		return pty.Window.Width
	}
	return 0
}

// watchWidth signals only actual width changes. Periodic events delivered to
// the key reader would restart its idle timeout even while the caller is idle.
func (st *konfigState) watchWidth() func() {
	updates := make(chan struct{}, 1)
	stop, done := make(chan struct{}), make(chan struct{})
	previous := st.physicalWidth
	st.resizeUpdates = updates
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				width := st.reportedWidth()
				if width == previous {
					continue
				}
				previous = width
				// The key reader uses the latest shared width, so one pending signal
				// also covers changes while a field hands off to another screen.
				select {
				case updates <- struct{}{}:
				default:
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}
