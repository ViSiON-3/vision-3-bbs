package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// readdressMail is the mail seedReaddressBase writes, in message order.
var readdressMail = []struct {
	to      string
	private bool
	wantTo  string // after readdress
}{
	{"Bob Builder", true, "Bob"},   // a unique real name
	{"Sysop", true, "Hermit"},      // the FidoNet sysop convention: user #1
	{"Bob", true, "Bob"},           // already a handle
	{"Pat Twin", true, "Pat Twin"}, // a real name two accounts share
	{"Stranger", true, "Stranger"}, // no such account
	{"Bob Builder", false, "Bob Builder"},
}

// seedReaddressBase writes a data directory holding users.json and a base of
// readdressMail, as mail imported before recipients were resolved left it.
// It returns the data directory and the base path.
func seedReaddressBase(t *testing.T) (string, string) {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "msgbases"), 0o755); err != nil {
		t.Fatal(err)
	}
	users, err := json.Marshal([]*user.User{
		{ID: 1, Handle: "Hermit", RealName: "Sam Sysop", AccessLevel: 255},
		{ID: 2, Handle: "Bob", RealName: "Bob Builder", AccessLevel: 30},
		{ID: 3, Handle: "PatA", RealName: "Pat Twin", AccessLevel: 30},
		{ID: 4, Handle: "PatB", RealName: "Pat Twin", AccessLevel: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "users.json"), users, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataDir, "msgbases", "netmail")
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range readdressMail {
		msg := jam.NewMessage()
		msg.From, msg.To, msg.Subject = "Remote User", m.to, "Subject"
		msg.Text = "Body of message " + string(rune('A'+i))
		if m.private {
			msg.Header = &jam.MessageHeader{Attribute: jam.MsgLocal | jam.MsgTypeNet | jam.MsgPrivate}
		}
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	return dataDir, path
}

// baseFiles returns the base's header and index files, to tell whether a
// run wrote anything.
func baseFiles(t *testing.T, path string) []byte {
	t.Helper()
	var all []byte
	for _, ext := range []string{".jhr", ".jdx", ".jdt"} {
		b, err := os.ReadFile(path + ext)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, b...)
	}
	return all
}

// checkBase verifies every message's To (want picks the expected one), that
// text, sender and privacy survived, and that the index's To CRCs agree:
// CountMessagesToUser is what the "Yours" and new-mail counts use.
func checkBase(t *testing.T, path string, want func(i int) string, counts map[string]int) {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	for i, m := range readdressMail {
		msg, err := b.ReadMessage(i + 1)
		if err != nil {
			t.Fatalf("message %d: %v", i+1, err)
		}
		if msg.To != want(i) {
			t.Errorf("message %d To = %q, want %q", i+1, msg.To, want(i))
		}
		if msg.Text != "Body of message "+string(rune('A'+i)) || msg.From != "Remote User" || msg.IsPrivate() != m.private || msg.IsDeleted() {
			t.Errorf("message %d changed beyond its To: %+v", i+1, msg)
		}
	}
	if n := b.GetActiveMessageCount(); n != len(readdressMail) {
		t.Errorf("active messages = %d, want %d", n, len(readdressMail))
	}
	for name, want := range counts {
		if got, err := b.CountMessagesToUser(name); err != nil || got != want {
			t.Errorf("CountMessagesToUser(%q) = %d, %v; want %d", name, got, err, want)
		}
	}
}

// TestReaddress pins 'v3mail readdress': a dry run reports what it would do
// and writes nothing; a real run rewrites real-name and "Sysop" To fields of
// private mail to the handle, header and index alike, leaves ambiguous and
// unknown names and public mail alone, and running it again changes nothing.
func TestReaddress(t *testing.T) {
	dataDir, path := seedReaddressBase(t)
	original := func(i int) string { return readdressMail[i].to }
	readdressed := func(i int) string { return readdressMail[i].wantTo }
	before := baseFiles(t, path)

	code, out, _ := runV3mail(t, t.TempDir(), "readdress", "--data", dataDir, "--dry-run", path)
	if code != 0 {
		t.Fatalf("dry run exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "2 would be readdressed (dry run, nothing written), 1 left undeliverable, 1 ambiguous") {
		t.Errorf("dry run summary:\n%s", out)
	}
	if !bytes.Equal(before, baseFiles(t, path)) {
		t.Fatal("dry run modified the base")
	}
	checkBase(t, path, original, map[string]int{"Bob": 1, "Hermit": 0})

	code, out, _ = runV3mail(t, t.TempDir(), "readdress", "--data", dataDir, path)
	if code != 0 {
		t.Fatalf("readdress exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Readdress complete: 2 readdressed, 1 left undeliverable, 1 ambiguous") {
		t.Errorf("readdress summary:\n%s", out)
	}
	checkBase(t, path, readdressed, map[string]int{"Bob": 2, "Hermit": 1, "Sysop": 0, "Bob Builder": 1})

	after := baseFiles(t, path)
	code, out, _ = runV3mail(t, t.TempDir(), "readdress", "--data", dataDir, path)
	if code != 0 || !strings.Contains(out, "Readdress complete: 0 readdressed, 1 left undeliverable, 1 ambiguous") {
		t.Errorf("second run exited %d:\n%s", code, out)
	}
	if !bytes.Equal(after, baseFiles(t, path)) {
		t.Error("second run modified the base")
	}

	// Packing drops the retired copies of the rewritten headers.
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Pack(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	checkBase(t, path, readdressed, map[string]int{"Bob": 2, "Hermit": 1})
}

// TestReaddressNeedsUsersFile pins that readdress refuses to run without the
// users file rather than creating one (with a default sysop) in what may be
// the wrong data directory.
func TestReaddressNeedsUsersFile(t *testing.T) {
	dataDir, path := seedReaddressBase(t)
	if err := os.Remove(filepath.Join(dataDir, "users.json")); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runV3mail(t, t.TempDir(), "readdress", "--data", dataDir, path); code != 1 {
		t.Errorf("exit %d without users.json, want 1:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "users.json")); !os.IsNotExist(err) {
		t.Error("readdress created users.json")
	}
}
