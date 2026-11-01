package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// #847: a subject stored as raw CP437 bytes (é is 0x82, ─ is 0xC4) shows as
// those characters, not '?', on a CP437 session, and as their UTF-8 form on a
// UTF-8 session.
func TestRawCP437SubjectDisplays(t *testing.T) {
	msg := &message.DisplayMessage{
		Subject:  "Caf\x82 \xc4\xc4 news",
		From:     "Ren\x82",
		To:       "All",
		DateTime: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	subs := buildMsgSubstitutions(msg, "GENERAL", 1, 1, false, 0, "", "", nil, 0, nil, 1, nil, nil)
	if got, want := subs['T'], "Café ── news"; got != want {
		t.Errorf("UTF-8 subject = %q, want %q", got, want)
	}
	if got, want := subs['F'], "René"; got != want {
		t.Errorf("UTF-8 from = %q, want %q", got, want)
	}
	cp := convertSubsToCP437(subs)
	if got, want := cp['T'], "Caf\x82 \xc4\xc4 news"; got != want {
		t.Errorf("CP437 subject = %q, want %q", got, want)
	}

	row := messageListRow(MessageListEntry{MsgNum: 1, Subject: msg.Subject, From: msg.From, To: msg.To}, ansi.OutputModeCP437)
	if !containsBytes(row, "Caf\x82 \xc4\xc4 news") || !containsBytes(row, "Ren\x82") {
		t.Errorf("CP437 list row = %q, want the subject and author bytes kept", row)
	}
}

// toCP437Safe keeps a byte that is already CP437 rather than turning it into
// '?': it cannot be UTF-8, and it is the character the sender meant.
func TestToCP437SafeKeepsRawCP437Bytes(t *testing.T) {
	if got, want := toCP437Safe("Caf\x82 é"), "Caf\x82 \x82"; got != want {
		t.Errorf("toCP437Safe = %q, want %q", got, want)
	}
}

func containsBytes(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
