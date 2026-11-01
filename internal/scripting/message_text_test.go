package scripting

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// #855: v3.message.get gives scripts the stored body as body, and alongside
// it text, the body with nothing a terminal would act on, for a script to
// print.
func TestMessageGetOffersPlainText(t *testing.T) {
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	areaID, err := mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"})
	if err != nil {
		t.Fatal(err)
	}
	const raw = "Hi\x1b[2J\x1b[1;31mthere\x1b[0m\x1b]0;title\x07\r\nline two\x07 caf\x82"
	if _, err := mm.AddMessage(areaID, "Alice", "All", "Hello", raw, ""); err != nil {
		t.Fatal(err)
	}

	sess := newInterruptibleSession("")
	eng := NewEngine(context.Background(), &SessionContext{
		Session: sess, OutputMode: ansi.OutputModeUTF8, ScreenWidth: 80, ScreenHeight: 24,
		UserHandle: "Bob",
	}, ScriptConfig{}, &Providers{MessageMgr: mm})
	t.Cleanup(func() {
		sess.closeInterrupt()
		eng.Close()
	})
	get := func(field string) string {
		t.Helper()
		v, err := eng.vm.RunString(`v3.message.get(` + strconv.Itoa(areaID) + `, 1).` + field)
		if err != nil {
			t.Fatalf("script: %v", err)
		}
		return v.String()
	}
	body := get("body")
	if body == get("text") {
		t.Fatalf("text is the raw body: %q", body)
	}
	if got, want := get("text"), "Hithere\nline two café"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

// A malformed escape holding raw bytes must not leave a control character in
// text once its pieces are taken out: C2 and 9B either side of a dropped 01
// would otherwise join into U+009B, CSI.
func TestMessageTextHasNoRecombinedControls(t *testing.T) {
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	areaID, err := mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mm.AddMessage(areaID, "Alice", "All", "Hello", "START\x1b[\xc2\x01\x9b6n END", ""); err != nil {
		t.Fatal(err)
	}
	sess := newInterruptibleSession("")
	eng := NewEngine(context.Background(), &SessionContext{
		Session: sess, OutputMode: ansi.OutputModeUTF8, ScreenWidth: 80, ScreenHeight: 24,
		UserHandle: "Bob",
	}, ScriptConfig{}, &Providers{MessageMgr: mm})
	t.Cleanup(func() {
		sess.closeInterrupt()
		eng.Close()
	})
	v, err := eng.vm.RunString(`v3.message.get(` + strconv.Itoa(areaID) + `, 1).text`)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	if got := v.String(); strings.ContainsRune(got, 0x9B) || !strings.HasPrefix(got, "START") || !strings.HasSuffix(got, " END") {
		t.Errorf("text = %q", got)
	}
}
