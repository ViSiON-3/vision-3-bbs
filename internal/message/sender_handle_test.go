package message

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// TestRealNameNetmailVisibleToSender pins the fix for netmail signed with a
// real name: the sender's handle is stored with it, so the private message
// stays visible to the account that wrote it and to no other.
func TestRealNameNetmailVisibleToSender(t *testing.T) {
	mm := newNetmailTestManager(t)
	n, err := mm.Post(1, "Pat Example", "Bob@21:1/100", "Hi", "Hello", PostOptions{SenderHandle: "pat"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mm.GetMessage(1, n)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.IsPrivate {
		t.Fatal("netmail should be private")
	}
	if !msg.VisibleTo("pat") {
		t.Error("sender cannot see their own netmail")
	}
	if msg.VisibleTo("Pat Example") || msg.VisibleTo("someone") {
		t.Error("netmail visible to an account that is not its sender or recipient")
	}
}

// TestSenderHandleOmittedWhenSameAsFrom checks a post signed with the
// handle stores no extra subfield.
func TestSenderHandleOmittedWhenSameAsFrom(t *testing.T) {
	mm := newNetmailTestManager(t)
	n, err := mm.Post(1, "Pat", "Bob@21:1/100", "Hi", "Hello", PostOptions{SenderHandle: "pat"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := mm.openBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	hdr, err := b.ReadMessageHeader(n)
	if err != nil {
		t.Fatal(err)
	}
	if sf := hdr.GetSubfieldByType(jam.SfldV3SenderHandle); sf != nil {
		t.Errorf("sender handle stored although it matches From: %q", sf.Buffer)
	}
}
