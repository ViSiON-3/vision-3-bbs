package v3net

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

type fakeNetwork struct {
	bindings map[int]AreaBinding
	sent     []protocol.Message
	fail     error
	onSend   func(protocol.Message) // sees every attempt, failed or not
}

func (f *fakeNetwork) AreaBindings() map[int]AreaBinding { return f.bindings }
func (f *fakeNetwork) NodeID() string                    { return "a1b2c3d4e5f6a7b8" }
func (f *fakeNetwork) SendMessage(_ string, msg protocol.Message) error {
	if f.onSend != nil {
		f.onSend(msg)
	}
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, msg)
	return nil
}

func newOutboundTest(t *testing.T) (*message.MessageManager, *fakeNetwork, *Outbound) {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"fel.general","name":"General","base_path":"fel_general","area_type":"v3net","network":"felonynet"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	net := &fakeNetwork{bindings: map[int]AreaBinding{1: {Network: "felonynet", Origin: "Test Origin"}}}
	out := NewOutbound(mm, net, func() string { return "TestBBS" },
		func(b AreaBinding) string { return b.Origin }, 0)
	return mm, net, out
}

// A post the BBS made (body transformed with the V3Net trailer) and one made
// by another program (no trailer) both go out once, with the trailer removed
// from the wire body, and are marked sent.
func TestOutbound_SendsPendingOnce(t *testing.T) {
	mm, net, out := newOutboundTest(t)
	mm.BodyTransform = func(_ int, body string) string {
		return AppendV3NetOrigin(body, DefaultTearline(), "Test Origin", "a1b2c3d4e5f6a7b8")
	}
	if _, err := mm.AddMessage(1, "Bob", "All", "From the BBS", "line one\nline two", ""); err != nil {
		t.Fatal(err)
	}
	mm.BodyTransform = nil
	if _, err := mm.AddMessage(1, "Sysop", "All", "From helper", "posted by helper", ""); err != nil {
		t.Fatal(err)
	}

	if n := out.SweepNow(); n != 2 {
		t.Fatalf("first sweep sent %d, want 2", n)
	}
	if got := net.sent[0]; got.Body != "line one\nline two" || got.Subject != "From the BBS" ||
		got.Network != "felonynet" || got.AreaTag != "fel.general" || got.Origin != "Test Origin" {
		t.Errorf("first wire message = %+v", got)
	}
	if got := net.sent[1].Body; got != "posted by helper" {
		t.Errorf("second wire body = %q", got)
	}
	if n := out.SweepNow(); n != 0 {
		t.Errorf("second sweep sent %d, want 0", n)
	}
	for num := 1; num <= 2; num++ {
		dm, err := mm.GetMessage(1, num)
		if err != nil {
			t.Fatal(err)
		}
		if dm.Attributes&0x10 == 0 { // jam.MsgSent
			t.Errorf("message %d not marked sent", num)
		}
	}
}

// A send that fails, including for want of a leaf, leaves the post pending
// for the next sweep.
func TestOutbound_RetriesFailedSend(t *testing.T) {
	mm, net, out := newOutboundTest(t)
	if _, err := mm.AddMessage(1, "Bob", "All", "s", "hi", ""); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{errors.New("hub down"), ErrNoLeaf} {
		net.fail = failure
		if n := out.SweepNow(); n != 0 {
			t.Fatalf("%v: sent %d, want 0", failure, n)
		}
	}
	net.fail = nil
	if n := out.SweepNow(); n != 1 || len(net.sent) != 1 {
		t.Fatalf("after recovery: sent %d (%d on wire), want 1", n, len(net.sent))
	}
}

// Every attempt at a post carries the same valid UUID, so a hub that stored
// a post whose response was lost drops the retry as a duplicate.
func TestOutbound_RetryKeepsUUID(t *testing.T) {
	mm, net, out := newOutboundTest(t)
	if _, err := mm.AddMessage(1, "Bob", "All", "s", "hi", ""); err != nil {
		t.Fatal(err)
	}
	var attempts []string
	net.fail = errors.New("response lost")
	net.onSend = func(m protocol.Message) { attempts = append(attempts, m.MsgUUID) }
	out.SweepNow()
	net.fail = nil
	out.SweepNow()
	if len(attempts) != 2 || attempts[0] != attempts[1] {
		t.Fatalf("attempt UUIDs = %v, want two identical", attempts)
	}
	if err := net.sent[0].Validate(); err != nil {
		t.Errorf("wire message invalid: %v", err)
	}
	if net.sent[0].ThreadUUID != net.sent[0].MsgUUID {
		t.Errorf("ThreadUUID %q != MsgUUID %q", net.sent[0].ThreadUUID, net.sent[0].MsgUUID)
	}
}

// Imported messages and unbound areas are never sent.
func TestOutbound_SkipsImportsAndUnbound(t *testing.T) {
	mm, net, out := newOutboundTest(t)
	w := NewJAMAdapter(mm, 1)
	if _, err := w.WriteMessage(protocol.Message{MsgUUID: "u-1", From: "Remote", To: "All", Subject: "s",
		Body: "from another node", DateUTC: "2026-10-24T12:00:00Z", Tearline: "--- X", Origin: "Elsewhere", OriginNode: "ffff"}); err != nil {
		t.Fatal(err)
	}
	if n := out.SweepNow(); n != 0 {
		t.Errorf("import sent: %d", n)
	}
	if _, err := mm.AddMessage(1, "Bob", "All", "s", "hi", ""); err != nil {
		t.Fatal(err)
	}
	net.bindings = map[int]AreaBinding{}
	if n := out.SweepNow(); n != 0 {
		t.Errorf("unbound area sent: %d", n)
	}
}

func TestStripV3NetOrigin(t *testing.T) {
	cases := map[string]string{
		"hi\n--- ViSiON/3 v1/Linux\n * Origin: Board (a1b2)\n": "hi",
		"hi\n * Origin: Board\n":                               "hi",
		"hi\n\n":                                               "hi",
		"a --- b\nc":                                           "a --- b\nc",
		"--- not a trailer\nmore":                              "--- not a trailer\nmore",
	}
	for in, want := range cases {
		if got := StripV3NetOrigin(in); got != want {
			t.Errorf("StripV3NetOrigin(%q) = %q, want %q", in, got, want)
		}
	}
	if !strings.HasPrefix(DefaultTearline(), "--- ") {
		t.Fatal("tearline format changed; StripV3NetOrigin expects \"--- \"")
	}
}
