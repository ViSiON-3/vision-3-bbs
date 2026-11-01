package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// hostileBodySeqs are what a message from another board can carry that a
// terminal would act on or answer, rather than draw. Each string holding a
// payload holds the word PAYLOAD, which must not show either.
var hostileBodySeqs = []struct{ name, seq string }{
	// Strings.
	{"OSC title, BEL", "\x1b]0;PAYLOAD\x07"},
	{"OSC title, ST", "\x1b]2;PAYLOAD\x1b\\"},
	{"OSC 52 clipboard", "\x1b]52;c;PAYLOAD\x07"},
	{"OSC 8 hyperlink", "\x1b]8;;http://PAYLOAD\x1b\\"},
	{"DCS", "\x1bPqPAYLOAD\x1b\\"},
	{"DCS query", "\x1bP$qmPAYLOAD\x1b\\"},
	{"APC", "\x1b_PAYLOAD\x1b\\"},
	{"PM", "\x1b^PAYLOAD\x1b\\"},
	{"SOS", "\x1bXPAYLOAD\x1b\\"},
	// 8-bit forms, in the UTF-8 encoding a modern terminal decodes.
	{"U+009B erase", "\u009b2J"},
	{"U+009B DSR", "\u009b6n"},
	{"U+009D OSC", "\u009d0;PAYLOAD\u009c"},
	{"U+0090 DCS", "\u0090PAYLOAD\u009c"},
	{"U+009F APC", "\u009fPAYLOAD\u009c"},
	{"U+0084 IND", "\u0084"},
	{"U+0085 NEL", "\u0085"},
	{"U+008D RI", "\u008d"},
	// Sequences the terminal answers into the caller's input.
	{"DSR cursor report", "\x1b[6n"},
	{"DSR status", "\x1b[5n"},
	{"DA primary", "\x1b[c"},
	{"DA with parameter", "\x1b[0c"},
	{"DA secondary", "\x1b[>c"},
	{"DECRQM", "\x1b[?25$p"},
	{"window report", "\x1b[18t"},
	{"title report", "\x1b[21t"},
	{"ENQ answerback", "\x05"},
	// Resets, modes and other state left behind after the message.
	{"RIS reset", "\x1bc"},
	{"alternate screen", "\x1b[?1049h"},
	{"hide cursor", "\x1b[?25l"},
	{"mouse reporting", "\x1b[?1000h"},
	{"bracketed paste", "\x1b[?2004h"},
	{"insert mode", "\x1b[4h"},
	{"modifyOtherKeys", "\x1b[>4;2m"},
	{"conformance level", "\x1b[61;1\"p"},
	{"auto-wrap off", "\x1b[?7l"},
	{"printer", "\x1b[5i"},
	{"window resize", "\x1b[8;1;1t"},
	{"scroll region", "\x1b[1;5r"},
	{"scroll up", "\x1b[5S"},
	{"insert line", "\x1b[5L"},
	{"delete line", "\x1b[5M"},
	{"delete char", "\x1b[5P"},
	{"charset", "\x1b(0"},
	{"screen fill", "\x1b#8"},
	{"keypad", "\x1b="},
	{"index", "\x1bD"},
	{"reverse index", "\x1bM"},
	// C0.
	{"BEL", "\x07"},
	{"BS", "\x08"},
	{"VT", "\x0b"},
	{"FF", "\x0c"},
	{"SO", "\x0e"},
	{"SI", "\x0f"},
	{"CAN", "\x18"},
	{"SUB", "\x1a"},
	{"DEL", "\x7f"},
}

// testSGR matches colour sequences, the one thing the art renderer may emit
// between two cells that have nothing between them in the message.
var testSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// bodyPaths are the two ways a body reaches the caller, chosen by whether it
// looks like art. The prefix is what sends it down that path.
var bodyPaths = []struct{ name, prefix string }{
	{"text", ""},
	{"art", "\x1b[5C"},
}

// storeRawBody writes a message directly to the JAM base, as one that arrived
// from another system, and returns its number.
func storeRawBody(t *testing.T, env *menuEnv, areaID int, body string) int {
	t.Helper()
	base, err := env.e.MessageMgr.GetBase(areaID)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer func() { _ = base.Close() }()
	msg := jam.NewMessage()
	msg.From, msg.To, msg.Subject = "Remote Guy", "All", "Hostile"
	msg.Text = body
	n, err := base.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	return n
}

// between returns what the caller received between two marks that have nothing
// but a hostile sequence between them in the message.
func between(raw, open, closing string) (string, bool) {
	_, rest, ok := strings.Cut(raw, open)
	if !ok {
		return "", false
	}
	mid, _, ok := strings.Cut(rest, closing)
	return mid, ok
}

// Each hostile sequence is read through the real reader, down both body paths
// and on both kinds of terminal. What it sends is compared between two marks, so
// the reader's own escapes elsewhere on screen don't count (#846).
func TestBodyControlsDoNotReachCaller(t *testing.T) {
	env := newMsgEnv(t)
	for _, path := range bodyPaths {
		for _, mode := range []struct {
			name string
			mode ansi.OutputMode
		}{{"utf8", ansi.OutputModeUTF8}, {"cp437", ansi.OutputModeCP437}} {
			for _, h := range hostileBodySeqs {
				t.Run(path.name+"/"+mode.name+"/"+h.name, func(t *testing.T) {
					sub := env.sub(t)
					sub.outputMode = mode.mode
					n := storeRawBody(t, sub, generalAreaID, path.prefix+"OPENMARK"+h.seq+"CLOSEMARK\n")
					sub.markRead(generalAreaID, "Caller", n-1)
					r := sub.runCmd("READMSGS", sub.caller, "", "Q")
					if r.err != nil {
						t.Fatalf("READMSGS: %v", r.err)
					}
					got, ok := between(r.raw, "OPENMARK", "CLOSEMARK")
					if !ok {
						t.Fatalf("the marks were not drawn; output:\n%q", r.raw)
					}
					if got = testSGR.ReplaceAllString(got, ""); got != "" {
						t.Errorf("the caller received %q", got)
					}
					if strings.Contains(r.raw, "PAYLOAD") {
						t.Error("the payload was shown")
					}
				})
			}
		}
	}
}

// A string with no terminator hides only its own line. A terminal would go on
// swallowing what followed, and a filter that did the same would lose the rest
// of the message.
func TestUnterminatedBodyStringKeepsTheRest(t *testing.T) {
	env := newMsgEnv(t)
	for _, intro := range []string{"\x1b]0;", "\x1bP", "\x1b_", "\x1b^", "\x1bX", "\u009d", "\u0090"} {
		for _, path := range bodyPaths {
			t.Run(fmt.Sprintf("%q/%s", intro, path.name), func(t *testing.T) {
				sub := env.sub(t)
				n := storeRawBody(t, sub, generalAreaID, path.prefix+"FIRSTLINE"+intro+"PAYLOAD\nSECONDLINE\n")
				sub.markRead(generalAreaID, "Caller", n-1)
				r := sub.runCmd("READMSGS", sub.caller, "", "Q")
				if !r.has("FIRSTLINE", "SECONDLINE") {
					t.Errorf("text around the string was lost; output:\n%s", r.text())
				}
				if strings.Contains(r.raw, "PAYLOAD") {
					t.Error("the payload was shown")
				}
				if strings.Contains(r.raw, intro) {
					t.Error("the string's introducer reached the caller")
				}
			})
		}
	}
}

// The other ways into the same reader, and the screens that quote a body's first
// line, are safe too.
func TestBodyControlsOtherScreens(t *testing.T) {
	const body = "OPENMARK\x1b]0;PAYLOAD\x07\u009b6n\x1b[6n\x07\x05CLOSEMARK\n"
	sequences := []string{"\x1b]0;", "PAYLOAD", "\u009b", "\x1b[6n", "\x07", "\x05"}

	for _, tc := range []struct{ name, cmd, args, keys string }{
		{"newscan", "NEWSCAN", "current", "\rQ"},
		{"list opened", "LISTMSGS", "", "\rQQ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMsgEnv(t)
			storeRawBody(t, env, generalAreaID, body)
			r := env.runCmd(tc.cmd, env.caller, tc.args, tc.keys)
			if r.err != nil {
				t.Fatalf("%s: %v", tc.cmd, r.err)
			}
			if !r.has("OPENMARK") {
				t.Fatalf("the message was not drawn; output:\n%s", r.text())
			}
			for _, seq := range sequences {
				if strings.Contains(r.raw, seq) {
					t.Errorf("the caller received %q", seq)
				}
			}
		})
	}

	t.Run("private mail", func(t *testing.T) {
		env := newMsgEnv(t)
		if _, err := env.e.MessageMgr.AddPrivateMessage(privmailAreaID, "Sysop", "Caller", "Hostile", body, ""); err != nil {
			t.Fatalf("AddPrivateMessage: %v", err)
		}
		r := env.runCmd("READPRIVMAIL", env.caller, "", "Q")
		if !r.has("OPENMARK") {
			t.Fatalf("the message was not drawn; output:\n%s", r.text())
		}
		for _, seq := range sequences {
			if strings.Contains(r.raw, seq) {
				t.Errorf("the caller received %q", seq)
			}
		}
	})
}

// A kludge in a body goes to the sysop's K view, and U+009B is the one form of
// CSI that view let through.
func TestKludgeViewDropsC1(t *testing.T) {
	env := newMsgEnv(t)
	n := storeRawBody(t, env, generalAreaID, "\x01PID: OPENMARK\u009b6n\u009dCLOSEMARK\rbody\r")
	env.markRead(generalAreaID, "Sysop", n-1)
	r := env.runCmd("READMSGS", env.sysop, "", "KqQ")
	if !r.has("OPENMARK") {
		t.Fatalf("the kludge was not drawn; output:\n%s", r.text())
	}
	if strings.Contains(r.raw, "\u009b") || strings.Contains(r.raw, "\u009d") {
		t.Error("the caller received a C1 control")
	}
}

// Colour, cursor movement and erasing in a plain message still reach the caller.
func TestBodyKeepsDisplaySequences(t *testing.T) {
	for _, seq := range []string{
		"\x1b[1;31m", "\x1b[0m", "\x1b[38;5;12m", "\x1b[38:5:12m", "\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D",
		"\x1b[K", "\x1b[2K", "\x1b[J", "\x1b[2J",
	} {
		t.Run(fmt.Sprintf("%q", seq), func(t *testing.T) {
			env := newMsgEnv(t)
			storeRawBody(t, env, generalAreaID, "OPENMARK"+seq+"CLOSEMARK\n")
			r := env.runCmd("READMSGS", env.caller, "", "Q")
			if got, ok := between(r.raw, "OPENMARK", "CLOSEMARK"); !ok || got != seq {
				t.Errorf("between the marks: %q, want %q", got, seq)
			}
		})
	}
}

// Raw CP437 is characters, not controls: 0x80-0x9F are letters and symbols, and
// 0x9B is the cent sign, the same byte that is CSI on an 8-bit terminal.
func TestRawCP437BodySurvives(t *testing.T) {
	high := "\x80\x81\x8d\x90\x9b\x9c\x9d\x9e\x9f"
	const decoded = "ÇüìÉ¢£¥₧ƒ"
	for _, path := range bodyPaths {
		body := path.prefix + "\x1b[1;36m\xdb\xb0" + high + "\xb2\x1b[0m\n"
		t.Run(path.name+"/utf8", func(t *testing.T) {
			env := newMsgEnv(t)
			storeRawBody(t, env, generalAreaID, body)
			r := env.runCmd("READMSGS", env.caller, "", "Q")
			if !strings.Contains(testSGR.ReplaceAllString(r.raw, ""), "█░"+decoded+"▓") {
				t.Errorf("the art changed; output:\n%q", r.raw)
			}
		})
		t.Run(path.name+"/cp437", func(t *testing.T) {
			env := newMsgEnv(t)
			env.outputMode = ansi.OutputModeCP437
			storeRawBody(t, env, generalAreaID, body)
			r := env.runCmd("READMSGS", env.caller, "", "Q")
			if !strings.Contains(testSGR.ReplaceAllString(r.raw, ""), "\xdb\xb0"+high+"\xb2") {
				t.Errorf("the art changed; output:\n%q", r.raw)
			}
		})
	}
}

// Shipped art, which uses the same sequences art posted to a board does, renders
// exactly as the renderer draws it unfiltered, whichever charset it is read in.
func TestShippedArtBodyUnchanged(t *testing.T) {
	env := newMsgEnv(t)
	files, err := filepath.Glob(filepath.Join(env.e.MenuSetPath, "templates", "message_headers", "MSGHDR.*.ans"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no shipped art found: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		art, _, _ := strings.Cut(string(data), "\x1a") // SAUCE is not part of a message
		if !detectAnsiArtInMessage(art) {
			art = "\x1b[H" + art
		}
		want := RenderANSIArtToLines(bodyAsUTF8(art, ""), ansi.ArtWidth, 500)
		got := messageBodyLines(bodyForDisplay(art, ""), "", 80, ansi.OutputModeUTF8)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: the art changed", filepath.Base(f))
		}
	}
}

// Art that turns wrapping off still gets its effect: the row is cut at the margin
// as it was before the filter, not wrapped onto the next.
func TestArtKeepsAutoWrapForRenderer(t *testing.T) {
	art := "\x1b[?7l\x1b[1;1H" + strings.Repeat("x", 90) + "\r\nnext\r\n"
	got := messageBodyLines(bodyForDisplay(art, ""), "", 80, ansi.OutputModeUTF8)
	if len(got) < 2 || !strings.Contains(got[1], "next") {
		t.Errorf("lines = %q", got)
	}
	// A plain body has no renderer to give it to.
	if got := bodyForDisplay("text\x1b[?7lmore", ""); got != "textmore" {
		t.Errorf("plain body = %q", got)
	}
}

// A string's payload is not the line of the body that stands for the message in
// the list and the thread pane.
func TestExcerptSkipsStringPayload(t *testing.T) {
	if got := excerpt("\x1b]0;PAYLOAD\x07\nfirst real line\n"); got != "first real line" {
		t.Errorf("excerpt = %q", got)
	}
}

// The charset is settled before the filter: a CP437 body's 0x9B stays a cent sign
// even beside the bytes that spell U+009B in UTF-8, while a UTF-8 body's U+009B
// is a control.
func TestBodyForDisplayChecksCharsetFirst(t *testing.T) {
	if got := bodyForDisplay("5\x9b \xdb\xc2\x9b2J", "CP437"); got != "5¢ █┬¢2J" {
		t.Errorf("CP437 body = %q", got)
	}
	if got := bodyForDisplay("caf\xc3\xa9\xc2\x9b2J", "UTF-8"); got != "café" {
		t.Errorf("UTF-8 body = %q", got)
	}
}
