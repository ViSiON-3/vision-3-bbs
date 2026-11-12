package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// posted is a message time that falls on 11/13/26 at 3:00 am on Kiritimati
// (UTC+14) and on 11/12/26 in any zone from UTC-12 to UTC+10.
var posted = time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC)

// inKiritimati sets the board's time zone to Pacific/Kiritimati for the
// test, skipping it on a host whose own zone would show the same date.
func inKiritimati(t *testing.T) {
	t.Helper()
	loc, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Skip("no time zone database:", err)
	}
	if posted.Local().Format("01/02/06") == "11/13/26" {
		t.Skip("the host's own zone shows the board's date")
	}
	prev := acsLocation.Load()
	acsLocation.Store(loc)
	t.Cleanup(func() { acsLocation.Store(prev) })
}

func TestBoardTime(t *testing.T) {
	inKiritimati(t)
	if got := boardTime(posted).Format("01/02/06 3:04 pm"); got != "11/13/26 3:00 am" {
		t.Errorf("boardTime = %q, want 11/13/26 3:00 am", got)
	}
	if !boardTime(time.Time{}).IsZero() {
		t.Error("a zero time isn't left zero")
	}
}

// The reader's header codes, the message list, the thread pane and search
// results show a message's date in the board's zone.
func TestMessageDatesInBoardZone(t *testing.T) {
	inKiritimati(t)
	msg := &message.DisplayMessage{MsgNum: 1, From: "Sysop", To: "All", Subject: "s", DateTime: posted}

	subs := buildMsgSubstitutions(msg, "GENERAL", 1, 1, false, 0, "", "", nil, 0, nil, 1, nil, nil)
	if subs['D'] != "11/13/26" || subs['W'] != "3:00 am" {
		t.Errorf("header date %q time %q, want 11/13/26 3:00 am", subs['D'], subs['W'])
	}
	if cell := listDateCell(posted); !strings.Contains(cell, "11/13/26") {
		t.Errorf("list date = %q, want 11/13/26", cell)
	}
	row := threadRow{Num: 1, Msg: msg}
	line := string(ansi.ReplacePipeCodes([]byte(renderTreeRow(row, false, false, false, 79, threadGlyphsFor(ansi.OutputModeUTF8), ansi.OutputModeUTF8))))
	if !strings.Contains(line, "11/13/26") {
		t.Errorf("thread row = %q, want 11/13/26", line)
	}
	ts := newTestSession("")
	writeMsgSearchRow(newTestTerminal(ts), ansi.OutputModeUTF8, 1, msgSearchHit{
		confTag: "LOCAL", areaTag: "GENERAL", msgNum: 1, subject: "s", from: "Sysop", dateTime: posted,
	})
	if out := ts.output(); !strings.Contains(out, "11/13/26") {
		t.Errorf("search row = %q, want 11/13/26", out)
	}
}

// A reply's quote banner dates the quoted message in the board's zone.
func TestReplyQuoteDateInBoardZone(t *testing.T) {
	inKiritimati(t)
	env := newMsgEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.Timezone = "Pacific/Kiritimati" // set the way config.json sets it
	env.e.SetServerConfig(cfg)
	if _, err := env.e.MessageMgr.AddMessageWithDate(generalAreaID, "Sysop", "All", "hello", "hello-body", "", posted); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	strs := []byte(`{"quoteTop": "[^N on ^D at ^W]", "quoteBottom": "[end]"}`)
	if err := os.WriteFile(filepath.Join(configDir, "strings.json"), strs, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISION3_MENU_PATH", t.TempDir())
	t.Setenv("VISION3_CONFIG_PATH", configDir)

	r := env.runCmd("READMSGS", env.caller, "", "R\x11 \x11\x1aQ")
	if !r.has("Reply posted successfully!") {
		t.Fatalf("reply not confirmed; output:\n%s", r.text())
	}
	if body := env.mustMsg(generalAreaID, 2).Body; !strings.Contains(body, "on 11/13/2026 at 3:00 AM]") {
		t.Errorf("reply body = %q, want the quoted message dated in the board's zone", body)
	}
}
