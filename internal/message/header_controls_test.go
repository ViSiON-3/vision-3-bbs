package message

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

func jamMessage(from, to, subject string) *jam.Message {
	msg := jam.NewMessage()
	msg.From, msg.To, msg.Subject = from, to, subject
	msg.Text = "body"
	return msg
}

// Whatever a post's fields hold, the base and the post hook get them clean.
func TestAddMessageCleansHeaderFields(t *testing.T) {
	mm := newReplyTestManager(t)
	var hookSubject string
	mm.OnMessagePosted = func(_ *MessageArea, _ int, _, _, subject, _ string) { hookSubject = subject }

	if _, err := mm.AddMessage(1, "Fr\x1bcom", "T\x1b[2Jo", "Sub\u009bject\r\nX: y", "body\x1b[1m", ""); err != nil {
		t.Fatal(err)
	}
	base, err := mm.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	msg, err := base.ReadMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.From != "Fr com" || msg.To != "T [2Jo" || msg.Subject != "Sub ject  X: y" {
		t.Errorf("stored from/to/subject = %q/%q/%q", msg.From, msg.To, msg.Subject)
	}
	if hookSubject != "Sub ject  X: y" {
		t.Errorf("hook subject = %q", hookSubject)
	}
	if msg.Text != "body\x1b[1m" {
		t.Errorf("body changed: %q", msg.Text)
	}
}

// A message stored with control characters reads back clean, keeps a raw CP437
// subject's characters, and still counts its replies.
func TestGetMessageCleansStoredHeaderFields(t *testing.T) {
	mm := newReplyTestManager(t)
	base, err := mm.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"Bad\x1b[2JOne", "Caf\x82 \x9b5"} {
		msg := jamMessage("Fr\x1bcom", "T\x07o", subject)
		msg.OrigAddr, msg.DestAddr = "1:2/3\x1bc", "4:5/6\x07"
		if _, err := base.WriteMessage(msg); err != nil {
			t.Fatal(err)
		}
	}
	_ = base.Close()

	dm, err := mm.GetMessage(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if dm.From != "Fr com" || dm.To != "T o" || dm.Subject != "Bad [2JOne" {
		t.Errorf("from/to/subject = %q/%q/%q", dm.From, dm.To, dm.Subject)
	}
	if dm.OrigAddr != "1:2/3 c" || dm.DestAddr != "4:5/6 " {
		t.Errorf("addresses = %q/%q", dm.OrigAddr, dm.DestAddr)
	}
	raw, err := mm.GetMessage(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Subject != "Caf\x82 \x9b5" {
		t.Errorf("raw CP437 subject = %q", raw.Subject)
	}
	if !SubjectsMatchThread("Bad\x1b[2JOne", "Re: "+dm.Subject) {
		t.Error("a stored subject no longer matches its cleaned reply")
	}
}
