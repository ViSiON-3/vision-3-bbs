package menu

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

var (
	shippedStringsOnce sync.Once
	shippedStrings     config.StringsConfig
	shippedStringsErr  error
)

// konfigTestStrings is the shipped strings.json, loaded the way the BBS loads
// it, so the form draws exactly what a fresh install shows.
func konfigTestStrings(t *testing.T) config.StringsConfig {
	t.Helper()
	shippedStringsOnce.Do(func() {
		shippedStrings, shippedStringsErr = config.LoadStrings("../../templates/configs")
	})
	if shippedStringsErr != nil {
		t.Fatalf("loading shipped strings: %v", shippedStringsErr)
	}
	return shippedStrings
}

// konfigTestExecutor is an executor carrying the shipped strings.
func konfigTestExecutor(t *testing.T) *MenuExecutor {
	t.Helper()
	e := &MenuExecutor{}
	e.SetStrings(konfigTestStrings(t))
	return e
}

// konfigStockExecutor is konfigTestExecutor with the stock menu set, for
// tests that draw KONFIG.ANS.
func konfigStockExecutor(t *testing.T) *MenuExecutor {
	t.Helper()
	e := &MenuExecutor{MenuSetPath: "../../menus/v3"}
	e.SetStrings(konfigTestStrings(t))
	return e
}

// konfigFields returns the json key of every StringsConfig field the form owns.
func konfigFields() map[string]string {
	fields := map[string]string{} // Go field name -> json key
	rt := reflect.TypeOf(config.StringsConfig{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Type.Kind() == reflect.String && strings.HasPrefix(key, "konfig") {
			fields[f.Name] = key
		}
	}
	return fields
}

// shout upper-cases the letters of s, leaving pipe codes (|07, |B1) and fmt
// verbs (%s, %d) as they are so the string still works.
func shout(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		switch {
		case r[i] == '|' && i+2 < len(r):
			b.WriteString(string(r[i : i+3]))
			i += 2
		case r[i] == '%' && i+1 < len(r):
			j := i + 1
			for j < len(r) && !unicode.IsLetter(r[j]) && r[j] != '%' {
				j++
			}
			if j < len(r) {
				b.WriteString(string(r[i : j+1]))
				i = j
			} else {
				b.WriteString(string(r[i:]))
				i = len(r)
			}
		default:
			b.WriteRune(unicode.ToUpper(r[i]))
		}
	}
	return b.String()
}

// shoutedKonfigStrings is the shipped strings with every konfig* value
// upper-cased.
func shoutedKonfigStrings(t *testing.T) config.StringsConfig {
	t.Helper()
	cfg := konfigTestStrings(t)
	v := reflect.ValueOf(&cfg).Elem()
	for name := range konfigFields() {
		f := v.FieldByName(name)
		f.SetString(shout(f.String()))
	}
	return cfg
}

// teeSession records every byte the form writes, so text that is drawn and
// then overwritten (a help line, a status message, the column box) is still
// checked.
type teeSession struct {
	*testterm.Session
	out *bytes.Buffer
}

func (s *teeSession) Write(p []byte) (int, error) {
	s.out.Write(p)
	return s.Session.Write(p)
}

// runKonfigCapture drives USERCONFIG with the given strings and returns
// everything it wrote.
func runKonfigCapture(t *testing.T, um *user.UserMgr, u *user.User, keys string, strs config.StringsConfig) (*user.User, string) {
	t.Helper()
	screen := testterm.New(80, 24)
	sess := &teeSession{Session: testterm.NewSession(screen, keys), out: &bytes.Buffer{}}
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	e := &MenuExecutor{}
	e.SetStrings(strs)
	c := &cmdCtx{
		e:           e,
		s:           sess,
		terminal:    term.NewTerminal(sess, ""),
		userManager: um,
		currentUser: u,
		nodeNumber:  1,
		outputMode:  ansi.OutputModeUTF8,
		termWidth:   80,
		termHeight:  24,
	}
	got, _, err := runUserKonfig(c, "")
	if err != nil {
		t.Fatalf("runUserKonfig: %v", err)
	}
	return got, sess.out.String()
}

var (
	escapeSeq     = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|[78])`)
	lowercaseWord = regexp.MustCompile(`[a-z][a-z']+`)
)

// TestKonfigTextComesFromStrings is the guard for #429: with every konfig*
// string upper-cased, nothing the form draws may contain a lower-case word.
// One that does is English hard-coded in Go, which a sysop cannot reword.
func TestKonfigTextComesFromStrings(t *testing.T) {
	strs := shoutedKonfigStrings(t)
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Upper-case user data, so the only lower case left is the form's own.
	n := 0
	fresh := func() *user.User {
		t.Helper()
		n++
		handle := "TESTER" + strings.Repeat("X", n)
		u, err := um.AddUser("password", handle, "REAL NAME", "LOC")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}

	// Each run starts from a new account, so one scenario's saved values
	// cannot change what the next one shows.
	var scenarios []string
	for i := 0; i < 12; i++ {
		scenarios = append(scenarios, strings.Repeat(keyDown, i)+"q") // every label, value and help line
	}
	scenarios = append(scenarios,
		"a"+keyClear+"20\rq",  // out-of-range number, with its range hint
		"a"+keyClear+"100\rq", // saved number
		"cq",                  // encoding: the session's own
		"ccq",                 // encoding: the other one, mismatch warning
		"cccq",                // encoding: back to Auto
		"dq",                  // hot keys
		"kq",                  // listing mode
		"g"+keyClear+"\rq",    // real name required
		"g"+keyClear+"AB\rq",  // real name too short
		"g"+keyClear+"ABCDE\rq",
		"g"+keyClear+"AB CD\rq", // real name updated
		"h"+keyClear+"\rq",      // location cleared
		"jWRONG\rq",             // incorrect current password
		"jpassword\rAB\rq",      // new password too short
		"jpassword\rNEWPASS\rOTHER\rq",
		"jpassword\rNEWPASS\rNEWPASS\rq", // password changed
		"lnq"+keyEsc+"q",                 // column box: a switch, then leave
		"lnsdlueq"+keyEsc+"q",            // try to switch every column off
	)

	var found []string
	seen := map[string]bool{}
	for _, keys := range scenarios {
		_, out := runKonfigCapture(t, um, fresh(), keys, strs)
		text := escapeSeq.ReplaceAllString(out, "")
		for _, w := range lowercaseWord.FindAllString(text, -1) {
			if !seen[w] {
				seen[w] = true
				found = append(found, w)
			}
		}
	}
	// The auto-signature screens hand off to the message editor, which has
	// its own strings; only its prompt and outcomes belong to the form.
	u := fresh()
	u.AutoSignature = "A SIG"
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	_, out := runKonfigCapture(t, um, u, "f"+keyEsc+"q", strs)
	for _, w := range lowercaseWord.FindAllString(escapeSeq.ReplaceAllString(out, ""), -1) {
		if !seen[w] {
			seen[w] = true
			found = append(found, w)
		}
	}
	_, out = runKonfigCapture(t, um, u, "fdq", strs)
	for _, w := range lowercaseWord.FindAllString(escapeSeq.ReplaceAllString(out, ""), -1) {
		if !seen[w] {
			seen[w] = true
			found = append(found, w)
		}
	}

	if len(found) > 0 {
		sort.Strings(found)
		t.Errorf("text not taken from konfig* strings: %s", strings.Join(found, " "))
	}
}

// TestKonfigStringFieldsAreUsed checks the other direction: every konfig*
// string is read by the form, so none sits in the string editor doing nothing.
func TestKonfigStringFieldsAreUsed(t *testing.T) {
	var src strings.Builder
	for _, f := range []string{"user_konfig.go", "user_konfig_render.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(data)
	}
	fields := konfigFields()
	if len(fields) == 0 {
		t.Fatal("StringsConfig has no konfig* fields")
	}
	for name, key := range fields {
		if !regexp.MustCompile(`\.` + name + `\b`).MatchString(src.String()) {
			t.Errorf("%s (%s) is never read by the form", key, name)
		}
	}
}

// Sysop text can be any length; the form must keep its shape regardless.
func TestKonfigLongStringsKeepLayout(t *testing.T) {
	strs := konfigTestStrings(t)
	long := strings.Repeat("W", 120)
	v := reflect.ValueOf(&strs).Elem()
	for name := range konfigFields() {
		f := v.FieldByName(name)
		if !strings.Contains(f.String(), "%") {
			f.SetString(long)
		}
	}
	um, u := newUserConfigTestUser(t)
	_, out := runKonfigCapture(t, um, u, "lq"+keyEsc+"q", strs)

	screen := testterm.New(80, 24)
	_, _ = screen.Write([]byte(out))
	for row := 1; row <= 24; row++ {
		if n := len([]rune(screen.Row(row))); n > 80 {
			t.Errorf("row %d is %d columns wide", row, n)
		}
	}
	// The value column must still start where it always does.
	for row := 1; row <= 24; row++ {
		line := screen.Row(row)
		if strings.HasPrefix(strings.TrimSpace(line), "[A]") {
			if !strings.Contains(line, "80") {
				t.Errorf("Screen Width row lost its value: %q", line)
			}
		}
	}
}

// A configured label longer than the edit row must not break the field
// editor: the label is cut so the box keeps room to type in.
func TestKonfigLongLabelEditing(t *testing.T) {
	strs := konfigTestStrings(t)
	long := strings.Repeat("W", 120)
	strs.KonfigScreenWidthLabel = long
	strs.KonfigRealNameLabel = long
	strs.KonfigPwCurrent = long
	um, u := newUserConfigTestUser(t)
	got, out := runKonfigCapture(t, um, u, "a"+keyClear+"100\r"+"g"+keyEsc+"jx"+keyEsc+"q", strs)
	if got.ScreenWidth != 100 {
		t.Errorf("ScreenWidth = %d, want 100: the field could not be edited", got.ScreenWidth)
	}
	screen := testterm.New(80, 24)
	_, _ = screen.Write([]byte(out))
	if n := len([]rune(screen.Row(konfigEditRow))); n > 80 {
		t.Errorf("edit row is %d columns wide", n)
	}
}

// Fixed-width cells are measured in terminal cells: a double-width glyph
// takes two, so rune counting would let a title or label overrun its cell.
func TestKonfigWideTextFitsCells(t *testing.T) {
	wide := strings.Repeat("界", 60)
	st := &konfigState{c: &cmdCtx{e: konfigTestExecutor(t), outputMode: ansi.OutputModeUTF8}}
	for _, n := range []int{konfigLabelWidth, konfigValueWidth, 14} {
		if got := runewidth.StringWidth(st.fit(wide, n)); got != n {
			t.Errorf("fit(wide, %d) is %d cells", n, got)
		}
		if got := runewidth.StringWidth(st.fit("ab", n)); got != n {
			t.Errorf("fit(ab, %d) is %d cells", n, got)
		}
	}
	if got := runewidth.StringWidth(stripEscapes(st.heading(wide))); got != konfigColWidth {
		t.Errorf("heading is %d cells, want %d", got, konfigColWidth)
	}
	if got := runewidth.StringWidth(stripEscapes(st.line("|07" + wide))); got > konfigLineWidth {
		t.Errorf("line is %d cells, want at most %d", got, konfigLineWidth)
	}
	// A CP437 session has no double-width glyphs: each character is one cell.
	st.c.outputMode = ansi.OutputModeCP437
	if got := len([]rune(st.fit(wide, konfigLabelWidth))); got != konfigLabelWidth {
		t.Errorf("CP437 fit is %d characters, want %d", got, konfigLabelWidth)
	}
}
