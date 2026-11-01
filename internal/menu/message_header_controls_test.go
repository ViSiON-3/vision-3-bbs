package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// hostileHeaders are header fields from another board, each holding one kind
// of control sequence: a raw escape, a CSI, an OSC and the 8-bit CSI. Each is
// short enough that no header style cuts it off, and ends in a mark that shows
// the message was drawn.
var hostileHeaders = []struct{ text, mark string }{
	{"\x1bcMARKESC", "MARKESC"},
	{"\x1b[9;9;9HMARKCSI", "MARKCSI"},
	{"\x1b]0;TITLE\x07MARKOSC", "MARKOSC"},
	{"\u009b8;8;8HMARKCONE", "MARKCONE"},
}

// controlSequences are what hostileHeaders would send a terminal. The reader
// and its screens send other escapes of their own, so these are matched whole.
var controlSequences = []string{"\x1bc", "\x1b[9;9;9H", "\x1b]0;", "\x07", "\u009b", "\x9b8;8;8H"}

// assertNoHeaderControls fails if a hostile sequence reached the caller.
func assertNoHeaderControls(t *testing.T, label, raw string) {
	t.Helper()
	for _, seq := range controlSequences {
		if strings.Contains(raw, seq) {
			t.Errorf("%s: the caller received %q", label, seq)
		}
	}
}

// importV3NetSubjects serves hostileHeaders' subjects as a hub would and polls
// them through a real leaf and JAM adapter into GENERAL.
func importV3NetSubjects(t *testing.T, env *menuEnv) {
	t.Helper()
	msgs := make([]protocol.Message, 0, len(hostileHeaders))
	for i, h := range hostileHeaders {
		uuid := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		msgs = append(msgs, protocol.Message{
			V3Net: protocol.ProtocolVersion, Network: "testnet", AreaTag: "gen.general",
			MsgUUID: uuid, ThreadUUID: uuid, OriginNode: "remote.example.net", OriginBoard: "Remote",
			From: "Remote Guy", To: "All", Subject: h.text, DateUTC: "2026-03-16T04:20:00Z",
			Body: "body " + h.mark, Kludges: map[string]any{},
		})
	}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info") {
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet"})
			return
		}
		_ = json.NewEncoder(w).Encode(protocol.MessagePage{Messages: msgs})
	}))
	t.Cleanup(hub.Close)

	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "leaf.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	ix, err := dedup.Open(filepath.Join(dir, "dedup.sqlite"))
	if err != nil {
		t.Fatalf("open dedup: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	l := leaf.New(leaf.Config{
		HubURL: hub.URL, Network: "testnet", Keystore: ks, DedupIndex: ix,
		JAMWriter: v3net.NewJAMAdapter(env.e.MessageMgr, generalAreaID),
	})
	n, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if n != len(hostileHeaders) {
		t.Fatalf("imported %d messages, want %d", n, len(hostileHeaders))
	}
}

// A V3Net subject holding control sequences is imported through the real leaf
// and read in every shipped header style, on both kinds of terminal (#842).
func TestV3NetSubjectControlsDoNotReachCaller(t *testing.T) {
	env := newMsgEnv(t)
	importV3NetSubjects(t, env)

	styles, err := filepath.Glob(filepath.Join(env.e.MenuSetPath, "templates", "message_headers", "MSGHDR.*.ans"))
	if err != nil || len(styles) == 0 {
		t.Fatalf("no header styles found: %v", err)
	}
	for _, path := range styles {
		style, err := strconv.Atoi(strings.Split(filepath.Base(path), ".")[1])
		if err != nil {
			t.Fatalf("header style file %s: %v", path, err)
		}
		caller := *env.caller
		caller.MsgHdr = style
		for name, mode := range map[string]ansi.OutputMode{"utf8": ansi.OutputModeUTF8, "cp437": ansi.OutputModeCP437} {
			env.outputMode = mode
			env.markRead(generalAreaID, "Caller", 0)
			r := env.runCmd("READMSGS", &caller, "", "NNNQ")
			if r.err != nil {
				t.Fatalf("READMSGS: %v", r.err)
			}
			for _, h := range hostileHeaders {
				if !r.has(h.mark) {
					t.Errorf("style %d %s: message %s not drawn", style, name, h.mark)
				}
			}
			assertNoHeaderControls(t, fmt.Sprintf("style %d %s", style, name), r.raw)
		}
	}
}

// storeRawHeaders writes messages directly to the JAM base, as a message stored
// before fields were cleaned on the way in.
func storeRawHeaders(t *testing.T, env *menuEnv) {
	t.Helper()
	base, err := env.e.MessageMgr.GetBase(generalAreaID)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer func() { _ = base.Close() }()
	for _, h := range hostileHeaders {
		msg := jam.NewMessage()
		msg.From, msg.To, msg.Subject = h.text, h.text, h.text
		msg.Text = "body " + h.mark
		if _, err := base.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}
}

// Messages already in the base are safe on every screen that lists or shows
// their subject, sender and recipient.
func TestStoredHeaderControlsDoNotReachCaller(t *testing.T) {
	env := newMsgEnv(t)
	storeRawHeaders(t, env)

	for _, tc := range []struct{ name, cmd, args, keys string }{
		{"reader", "READMSGS", "", "NNNQ"},
		{"newscan", "NEWSCAN", "current", "\rNNNQ"},
		{"list", "LISTMSGS", "", "QQ"},
		{"list opened", "LISTMSGS", "", "\rQQ"},
		{"reply editor", "READMSGS", "", "R\x01YQ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			sub.markRead(generalAreaID, "Caller", 0)
			r := sub.runCmd(tc.cmd, sub.caller, tc.args, tc.keys)
			if r.err != nil {
				t.Fatalf("%s: %v", tc.cmd, r.err)
			}
			if !r.has("MARKESC") {
				t.Fatalf("the first message was not drawn; output:\n%s", r.text())
			}
			assertNoHeaderControls(t, tc.name, r.raw)
		})
	}
}

// A reply to a stored message takes its subject and recipient from the message,
// and neither carries the control characters into the new one.
func TestReplyDoesNotCopyHeaderControls(t *testing.T) {
	env := newMsgEnv(t)
	storeRawHeaders(t, env)

	r := env.runCmd("READMSGS", env.caller, "", "RThanks\x1aQ")
	if !r.has("Reply posted successfully!") {
		t.Fatalf("reply not confirmed; output:\n%s", r.text())
	}
	assertNoHeaderControls(t, "reply", r.raw)
	reply := env.mustMsg(generalAreaID, len(hostileHeaders)+1)
	if reply.Subject != "Re:  cMARKESC" || reply.To != " cMARKESC" {
		t.Errorf("reply subject/to = %q/%q", reply.Subject, reply.To)
	}
}

// A caller on a UTF-8 terminal can type U+009B at the subject prompt: ESC ends
// the prompt and it drops other C0 bytes, but it takes any complete character.
func TestTypedSubjectDropsControlCharacter(t *testing.T) {
	env := newMsgEnv(t)

	r := env.runCmd("COMPOSEMSG", env.caller, "GENERAL", "\u009b2JFirst post\r\rHello, board.\x1a")
	if !r.has("Message Posted!") {
		t.Fatalf("post not confirmed; output:\n%s", r.text())
	}
	base, err := env.e.MessageMgr.GetBase(generalAreaID)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer func() { _ = base.Close() }()
	stored, err := base.ReadMessage(1)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if stored.Subject != " 2JFirst post" {
		t.Errorf("stored subject = %q", stored.Subject)
	}
}

// The list's plain-text cells also show body excerpts, which nothing else cleans.
func TestListCellDropsC1(t *testing.T) {
	if got := listCell("a\u009b2Jb", 6, ansi.OutputModeUTF8); got != "a2Jb  " {
		t.Errorf("listCell = %q", got)
	}
}
