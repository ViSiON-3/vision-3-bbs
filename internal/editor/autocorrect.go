package editor

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Autocorrect fixes common typos as a word is finished. When a space, Tab
// or Enter is typed, the word before the cursor (or before the sentence
// punctuation right after it: "teh," or "(teh)") is looked up in a list of
// known misspellings and replaced. Punctuation alone does not end a word,
// so "teh.com", "teh_var" and "teh@example.com" are left alone.
//
//   - Only listed typos are fixed: the built-in list plus the board's own
//     configs/autocorrect.txt, which wins. There is no dictionary, so
//     handles, jargon and part numbers are never "corrected".
//   - Case follows the word: teh becomes the, Teh becomes The. ALL-CAPS words
//     (acronyms) are left alone, as are words touching digits or pipe codes,
//     and quoted lines.
//   - Backspace right after a fix puts the original word back.
//   - Ctrl-O turns it on or off for the caller.
//   - Each fix is shown on the prompt row for a moment ("teh » the"), and the
//     fixed word is drawn in the theme's autocorrectHighlightColor for as
//     long as that notice is up.
//
// Whether it is offered at all is decided by the caller of the editor (the
// message area's autocorrect setting or the board default); see
// EditorContext.Autocorrect.
//
// The board list has one fix per line, "wrong right" separated by spaces or
// tabs (right may be several words); "wrong -" turns a built-in fix off; #
// starts a comment. A line with no fix is logged and skipped.

//go:embed autocorrect_typos.txt
var builtinTypos string

// autocorrectNotice is how long a fix stays on the prompt row, and its word
// highlighted, before the next key puts the footer and the text colour back.
const autocorrectNotice = 2500 * time.Millisecond

// defaultAutocorrectColor is the highlight for a fixed word when the theme
// sets none: bright white.
const defaultAutocorrectColor = 15

// textSGR is the pen the editor draws message text with. Every write that
// colours the screen (header, footer, prompts, notices) resets to it after.
const textSGR = "\x1b[0m"

// wordBefore is the word right before the cursor: letters with at most one
// inner apostrophe, not glued to a digit, pipe code, path, address or handle,
// then any sentence punctuation typed after it (the second group).
var wordBefore = regexp.MustCompile(`(?:^|[^A-Za-z0-9'_|@./\\-])([A-Za-z]+(?:'[A-Za-z]+)?)([.,;:!?)\]}"]*)$`)

// quoteLine matches a quoted line in a reply, which starts like " DI> ".
var quoteLine = regexp.MustCompile(`^\s*[A-Za-z0-9_]{0,3}>`)

// acFix is one fix: the word as typed, what it became, the punctuation
// between it and the key that ended it, and what that key typed (after, the
// punctuation included), kept so the next Backspace can undo it. enter is
// set when the key was Enter, which typed nothing on the line but broke it.
type acFix struct {
	word, fixed string
	tail, after string
	enter       bool
}

// wordMark is a word drawn in a highlight colour: the buffer line it is on,
// its first column (1-based, in characters), the word, and the SGR sequence
// it is drawn with. after is how many characters follow the word up to the
// cursor once the key that ended it is in. The buffer is not changed; see
// Screen.MarkWord.
type wordMark struct {
	line, col int
	word, sgr string
	after     int
}

// highlightSGR is the escape sequence for DOS colour code: the editor's
// text pen with that foreground, and its background unless it is black, so a
// terminal with a background of its own keeps it behind the word.
func highlightSGR(code int) string {
	return textSGR + colorCodeToAnsi(code, false)
}

// parseTypos adds the fixes in text (from source, for the log) to list, or
// removes them for "wrong -". The misspelling and its fix are separated by
// any run of spaces or tabs; a line with no fix is logged and skipped.
func parseTypos(source, text string, list map[string]string) {
	for n, raw := range strings.Split(text, "\n") {
		fields := strings.Fields(raw)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) == 1 {
			slog.Warn("autocorrect: skipping entry with no fix", "file", source, "line", n+1, "entry", fields[0])
			continue
		}
		wrong, right := strings.ToLower(fields[0]), strings.Join(fields[1:], " ")
		if right == "-" {
			delete(list, wrong)
		} else {
			list[wrong] = right
		}
	}
}

// loadTypos returns the built-in list with the board's list in configDir
// applied over it. A missing board list is not an error.
func loadTypos(configDir string) map[string]string {
	list := make(map[string]string)
	parseTypos("built-in", builtinTypos, list)
	if configDir != "" {
		path := filepath.Join(configDir, "autocorrect.txt")
		if data, err := os.ReadFile(path); err == nil {
			parseTypos(path, string(data), list)
		}
	}
	return list
}

// acKeyText is what a key that ends a word types on the line, and whether
// it ends one at all: a space, Tab (its spaces) or Enter (nothing; it breaks
// the line). Punctuation does not: it waits for one of these, so the word
// is only fixed once it is clear it was not part of "teh.com" or "teh_var".
func acKeyText(key int) (string, bool) {
	switch key {
	case ' ':
		return " ", true
	case KeyTab:
		return strings.Repeat(" ", tabSpaces), true
	case KeyEnter:
		return "", true
	}
	return "", false
}

// matchCase gives fix the word's leading capital: teh -> the, Teh -> The. A
// fix with capitals of its own (I'm) keeps them.
func matchCase(word, fix string) string {
	first, rest := []rune(word), []rune(fix)
	if len(first) == 0 || len(rest) == 0 || !unicode.IsUpper(first[0]) {
		return fix
	}
	return string(unicode.ToUpper(rest[0])) + string(rest[1:])
}

// findFix returns the fix for the word ending at the end of before, or nil.
func findFix(list map[string]string, before string) *acFix {
	if quoteLine.MatchString(before) {
		return nil
	}
	m := wordBefore.FindStringSubmatch(before)
	if m == nil {
		return nil
	}
	word := m[1]
	if len(word) > 1 && word == strings.ToUpper(word) {
		return nil // acronym
	}
	right, ok := list[strings.ToLower(word)]
	if !ok {
		return nil
	}
	fixed := matchCase(word, right)
	if fixed == word {
		return nil
	}
	return &acFix{word: word, fixed: fixed, tail: m[2]}
}

// EnableAutocorrect turns autocorrect on for this editor with the built-in
// list and configDir's autocorrect.txt. on is the caller's saved choice;
// onToggle, if set, is called with the new state when Ctrl-O changes it, so
// the caller's record can keep it.
func (e *FSEditor) EnableAutocorrect(configDir string, on bool, onToggle func(on bool)) {
	e.typos = loadTypos(configDir)
	e.acOn = on
	e.acToggled = onToggle
	if e.acSGR == "" {
		e.acSGR = highlightSGR(defaultAutocorrectColor)
	}
	if e.acText == (acText{}) {
		e.acText = defaultACText
	}
}

// acText is the text of autocorrect's prompt-row notices (strings.json
// editorAutocorrect*). fixed takes the word as typed and the fix, kept the
// word put back.
type acText struct {
	fixed, kept, on, off string
}

// defaultACText is used for any notice strings.json leaves blank.
var defaultACText = acText{
	fixed: "|08%s » %s",
	kept:  "|08Kept %s",
	on:    "|08Autocorrect on",
	off:   "|08Autocorrect off",
}

// SetAutocorrectStrings sets the notices' text; a blank one keeps the
// default.
func (e *FSEditor) SetAutocorrectStrings(fixed, kept, on, off string) {
	pick := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return v
	}
	e.acText = acText{
		fixed: pick(fixed, defaultACText.fixed),
		kept:  pick(kept, defaultACText.kept),
		on:    pick(on, defaultACText.on),
		off:   pick(off, defaultACText.off),
	}
}

// SetAutocorrectColor sets the DOS colour code a fixed word is highlighted
// in (theme.json autocorrectHighlightColor).
func (e *FSEditor) SetAutocorrectColor(code int) {
	e.acSGR = highlightSGR(code)
}

// textBeforeCursor is the current line up to the cursor.
func (e *FSEditor) textBeforeCursor() string {
	line := []rune(e.buffer.GetLine(e.currentLine))
	col := e.currentCol - 1
	if col > len(line) {
		col = len(line)
	}
	return string(line[:col])
}

// applyAutocorrect replaces the word before the cursor (and any sentence
// punctuation after it) if it is a known typo, when key ends a word. It goes
// through the editor's own backspace and typing so wrapping and redraw
// behave exactly as if the caller had typed the fix. It returns the fix, or
// nil when there was none; the caller applies key and then keeps the fix in
// acUndo.
func (e *FSEditor) applyAutocorrect(key int) *acFix {
	keyText, ends := acKeyText(key)
	if !ends || e.typos == nil || !e.acOn || !e.insertMode || e.buffer.QuotePrefix(e.currentLine) != "" {
		return nil
	}
	fix := findFix(e.typos, e.textBeforeCursor())
	if fix == nil {
		return nil
	}
	fix.after, fix.enter = fix.tail+keyText, key == KeyEnter
	e.unmarkFix() // one highlight at a time
	for range []rune(fix.word + fix.tail) {
		e.handleBackspace()
	}
	for _, r := range fix.fixed + fix.tail {
		e.insertCharacter(r)
	}
	tail := len([]rune(fix.tail))
	e.acPending = &wordMark{line: e.currentLine, col: e.currentCol - tail - len([]rune(fix.fixed)),
		word: fix.fixed, sgr: e.acSGR, after: len([]rune(fix.after))}
	e.showNotice(fmt.Sprintf(e.acText.fixed, fix.word, fix.fixed))
	return fix
}

// placeFixMark highlights the word the last fix produced, once the key that
// ended it has been applied. That key may have wrapped the word onto the
// next line; if the word cannot be found where it was put or just before the
// cursor, it is left unhighlighted. Run calls it before each screen refresh.
func (e *FSEditor) placeFixMark() {
	p := e.acPending
	e.acPending = nil
	if p == nil {
		return
	}
	if !e.buffer.wordAt(p.line, p.col, p.word) {
		// Wrapped with the key that ended it: the word now ends just before
		// the punctuation and the key's spaces, at the cursor.
		col := e.currentCol - p.after - len([]rune(p.word))
		if !e.buffer.wordAt(e.currentLine, col, p.word) {
			return
		}
		p.line, p.col = e.currentLine, col
	}
	e.screen.MarkWord(p)
}

// unmarkFix puts a highlighted fix back in the text colour (on the next
// refresh) and forgets it.
func (e *FSEditor) unmarkFix() {
	e.acPending = nil
	e.screen.UnmarkWord()
}

// wordAt reports whether line holds word starting at column col (1-based).
func (b *MessageBuffer) wordAt(line, col int, word string) bool {
	if line < 1 || line > b.GetLineCount() || col < 1 {
		return false
	}
	runes := []rune(b.GetLine(line))
	w := []rune(word)
	end := col - 1 + len(w)
	return end <= len(runes) && string(runes[col-1:end]) == word
}

// MarkWord draws m.word in m.sgr at its place in the text, from the next
// refresh until the line is redrawn without it: when UnmarkWord is called,
// when the line no longer holds the word there, or when the whole view is
// redrawn (scrolling, resize, Ctrl-L), which forgets the mark. Only the
// screen is changed.
func (s *Screen) MarkWord(m *wordMark) {
	s.UnmarkWord()
	s.mark = m
	delete(s.physicalLines, m.line) // draw it on the next refresh
}

// UnmarkWord forgets the highlighted word and has its line redrawn plain on
// the next refresh. The redraw writes the line as it now is, so a word that
// has since moved or changed is never written over other text.
func (s *Screen) UnmarkWord() {
	if s.mark == nil {
		return
	}
	delete(s.physicalLines, s.mark.line)
	s.mark = nil
}

// markedLine is content as RefreshLine draws it for lineNum: with the marked
// word in its colour when the line still holds it, or plain (dropping the
// mark) when it does not.
func (s *Screen) markedLine(lineNum int, content string) string {
	m := s.mark
	if m == nil || m.line != lineNum {
		return content
	}
	runes := []rune(content)
	w := []rune(m.word)
	end := m.col - 1 + len(w)
	if m.col < 1 || end > len(runes) || string(runes[m.col-1:end]) != m.word {
		s.mark = nil
		return content
	}
	return string(runes[:m.col-1]) + m.sgr + m.word + textSGR + string(runes[end:])
}

// undoAutocorrect puts back the word u replaced, when the text before the
// cursor still ends with the fix and what the key that ended it typed, and
// reports whether it did. A fix made by Enter is undone at the start of the
// new line: the break is joined, the word put back and the line broken
// again, so the caller keeps the line break.
func (e *FSEditor) undoAutocorrect(u *acFix) bool {
	if u == nil {
		return false
	}
	if u.enter {
		if e.currentCol != 1 || e.currentLine < 2 || !strings.HasSuffix(e.buffer.GetLine(e.currentLine-1), u.fixed+u.after) {
			return false
		}
		e.handleBackspace() // back to the end of the fixed line
	}
	if !strings.HasSuffix(e.textBeforeCursor(), u.fixed+u.after) {
		return false
	}
	for range []rune(u.fixed + u.after) {
		e.handleBackspace()
	}
	e.unmarkFix() // the original word goes back plain
	for _, r := range u.word + u.after {
		e.insertCharacter(r)
	}
	if u.enter {
		e.handleReturn()
	}
	e.showNotice(fmt.Sprintf(e.acText.kept, u.word))
	return true
}

// toggleAutocorrect switches autocorrect on or off and tells the caller.
func (e *FSEditor) toggleAutocorrect() {
	if e.typos == nil {
		return
	}
	e.acOn = !e.acOn
	if e.acToggled != nil {
		e.acToggled(e.acOn)
	}
	if e.acOn {
		e.showNotice(e.acText.on)
	} else {
		e.showNotice(e.acText.off)
	}
}

// showNotice writes text on the prompt row. clearNotice puts the footer
// back, and the fixed word's colour, once it has been up long enough.
func (e *FSEditor) showNotice(text string) {
	e.screen.GoXY(1, e.screen.PromptRow())
	e.screen.ClearEOL()
	e.screen.WriteDirectProcessed(text)
	e.screen.WriteDirect(textSGR)
	e.noticeAt = e.now()
}

// clearNotice restores the prompt row a notice has been on for at least
// autocorrectNotice, and unmarks the fixed word; it is called before each
// key.
func (e *FSEditor) clearNotice() {
	if e.noticeAt.IsZero() || e.now().Sub(e.noticeAt) < autocorrectNotice {
		return
	}
	e.noticeAt = time.Time{}
	e.unmarkFix()
	e.restorePromptRow()
}
