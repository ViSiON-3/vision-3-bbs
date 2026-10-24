package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// sauceRecord builds a 128-byte SAUCE record with the given title.
func sauceRecord(title string) []byte {
	rec := make([]byte, 128)
	copy(rec, "SAUCE00")
	t := make([]byte, 35)
	for i := range t {
		t[i] = ' '
	}
	copy(t, title)
	copy(rec[7:], t)
	return rec
}

// The Micronet ad that prompted this command arrived with a CR every ~78
// bytes because something wrapped it as plain text. An .ANS body must keep
// its long, escape-laden lines whole, lose only the SAUCE, and end with an
// attribute reset so the tearline is not drawn in the art's last colour.
func TestPreparePostBody_ANSKeptByteForByte(t *testing.T) {
	row := "\x1b[0;1;30m\x1b[26C\x1b[36m" + strings.Repeat("\x1b[37m\xdc\xdc\xdc\x1b[30m\xdc ", 20) + "\xb3"
	if len(row) < 200 {
		t.Fatalf("test row too short: %d bytes", len(row))
	}
	file := []byte(row + "\r\n" + row + "\r\n")
	file = append(file, 0x1A)
	file = append(file, sauceRecord("Micronet Ad")...)

	body, err := preparePostBody(file, postBodyOptions{format: postFormatANS, charset: "auto"})
	if err != nil {
		t.Fatalf("preparePostBody: %v", err)
	}
	want := row + "\n" + row + "\x1b[0m"
	if body.text != want {
		t.Errorf("body changed:\n got %q\nwant %q", body.text, want)
	}
	if body.lines != 2 {
		t.Errorf("lines = %d, want 2", body.lines)
	}
	if body.sauceTitle != "Micronet Ad" {
		t.Errorf("sauceTitle = %q, want %q", body.sauceTitle, "Micronet Ad")
	}
}

// CP437 art that happens to be valid UTF-8 must not be "converted" when the
// format is ans: ▄│ (DC B3) is a valid UTF-8 pair.
func TestPreparePostBody_ANSNeverTranscoded(t *testing.T) {
	art := []byte("\x1b[32m\xdc\xb3\xdc\xb3")
	body, err := preparePostBody(art, postBodyOptions{format: postFormatANS, charset: "utf8"})
	if err != nil {
		t.Fatalf("preparePostBody: %v", err)
	}
	if body.text != string(art)+"\x1b[0m" {
		t.Errorf("got %q", body.text)
	}
}

func TestPreparePostBody_LineEndingsNormalise(t *testing.T) {
	want := "one\ntwo\n\nfour"
	for name, in := range map[string]string{
		"crlf": "one\r\ntwo\r\n\r\nfour\r\n",
		"lf":   "one\ntwo\n\nfour\n",
		"cr":   "one\rtwo\r\rfour\r",
	} {
		body, err := preparePostBody([]byte(in), postBodyOptions{format: postFormatTXT, charset: "auto"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if body.text != want {
			t.Errorf("%s: got %q, want %q", name, body.text, want)
		}
	}
}

func TestPreparePostBody_UTF8ToCP437(t *testing.T) {
	body, err := preparePostBody([]byte("┌──┐ café\n"), postBodyOptions{format: postFormatTXT, charset: "auto"})
	if err != nil {
		t.Fatalf("preparePostBody: %v", err)
	}
	if want := "\xda\xc4\xc4\xbf caf\x82"; body.text != want {
		t.Errorf("got %q, want %q", body.text, want)
	}

	if _, err := preparePostBody([]byte("ok\nsnow ☃\n"), postBodyOptions{format: postFormatTXT, charset: "auto"}); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("unmappable rune: err = %v, want a line 2 error", err)
	}

	// Invalid UTF-8 cannot be UTF-8, so auto keeps it as CP437.
	body, err = preparePostBody([]byte("\xc9\xcd\xbb"), postBodyOptions{format: postFormatASC, charset: "auto"})
	if err != nil || body.text != "\xc9\xcd\xbb" {
		t.Errorf("cp437 asc: got %q, %v", body.text, err)
	}
}

func TestPreparePostBody_Rejects(t *testing.T) {
	cases := map[string]struct {
		in   string
		opts postBodyOptions
	}{
		"nul":       {"a\x00b", postBodyOptions{format: postFormatTXT}},
		"empty":     {"\r\n \r\n", postBodyOptions{format: postFormatTXT}},
		"wrap ans":  {"x", postBodyOptions{format: postFormatANS, wrap: 70}},
		"bad chars": {"x", postBodyOptions{format: postFormatTXT, charset: "latin1"}},
	}
	for name, c := range cases {
		if _, err := preparePostBody([]byte(c.in), c.opts); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestPreparePostBody_Wrap(t *testing.T) {
	in := "the quick brown fox jumps over the lazy dog\n\nshort"
	body, err := preparePostBody([]byte(in), postBodyOptions{format: postFormatTXT, wrap: 15})
	if err != nil {
		t.Fatalf("preparePostBody: %v", err)
	}
	want := "the quick brown\nfox jumps over\nthe lazy dog\n\nshort"
	if body.text != want {
		t.Errorf("got %q, want %q", body.text, want)
	}
	if got := wrapPlainLines([]string{"abcdefghij"}, 4); strings.Join(got, "|") != "abcd|efgh|ij" {
		t.Errorf("long word: %q", got)
	}
}

func TestPostBodyWarnings(t *testing.T) {
	lines := []string{"hello", "---", "\x1b[1m * Origin: fake (1:2/3)", "SEEN-BY: 1/2", "\x1b[2J"}
	w := strings.Join(postBodyWarnings(lines), "\n")
	for _, want := range []string{"line 2 looks like a tearline", "line 3 looks like an origin", "line 4 starts with SEEN-BY", "clears the screen"} {
		if !strings.Contains(w, want) {
			t.Errorf("warnings missing %q:\n%s", want, w)
		}
	}
	if w := postBodyWarnings([]string{"--- not a dash rule? yes it is", "fine"}); len(w) != 1 {
		t.Errorf("got %d warnings, want 1", len(w))
	}
}

func TestPostFormatFor(t *testing.T) {
	for file, want := range map[string]string{"AD.ANS": "ans", "x.asc": "asc", "x.txt": "txt", "-": "txt", "README": "txt"} {
		if got, err := postFormatFor(file, ""); err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", file, got, err, want)
		}
	}
	if got, _ := postFormatFor("x.txt", "ANS"); got != "ans" {
		t.Errorf("override: got %q", got)
	}
	if _, err := postFormatFor("x", "rip"); err == nil {
		t.Error("unknown format: no error")
	}
}

// postTestDirs writes a config with local, echomail, netmail, V3Net and
// PRIVMAIL areas, and a users file with Bob and the deleted user Gone.
func postTestDirs(t *testing.T) (configDir, dataDir string) {
	t.Helper()
	tmp := t.TempDir()
	configDir = filepath.Join(tmp, "configs")
	dataDir = filepath.Join(tmp, "data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[
	 {"id":1,"tag":"GENERAL","name":"General","base_path":"msgbases/general","area_type":"local"},
	 {"id":2,"tag":"FD_OTHERNETS","name":"Other Nets","base_path":"msgbases/othernets","area_type":"echomail",
	  "echo_tag":"OTHERNETS","origin_addr":"1:2/3","network":"fidonet"},
	 {"id":3,"tag":"FD_NETMAIL","name":"Netmail","base_path":"msgbases/netmail","area_type":"netmail",
	  "origin_addr":"1:2/3","network":"fidonet"},
	 {"id":4,"tag":"V3_GEN","name":"V3","base_path":"msgbases/v3","area_type":"v3net","network":"vnet"},
	 {"id":5,"tag":"PRIVMAIL","name":"Private Mail","base_path":"msgbases/privmail","area_type":"local"}]`
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("message_areas.json", areas)
	write("config.json", `{"boardName":"Test BBS","sysOpName":"Test Sysop"}`)
	usersDir := filepath.Join(dataDir, "users")
	if err := os.MkdirAll(usersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	users := `[{"id":1,"handle":"Bob"},{"id":2,"handle":"Gone","deletedUser":true}]`
	if err := os.WriteFile(filepath.Join(usersDir, "users.json"), []byte(users), 0o600); err != nil {
		t.Fatal(err)
	}
	return configDir, dataDir
}

// readPosted reads message num of area back from the JAM base as stored.
func readPosted(t *testing.T, configDir, dataDir string, areaID, num int) *jam.Message {
	t.Helper()
	mm, err := message.NewMessageManager(dataDir, configDir, "Test BBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mm.Close() })
	b, err := mm.GetBase(areaID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	msg, err := b.ReadMessage(num)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return msg
}

func TestRunPost_EchomailANS(t *testing.T) {
	configDir, dataDir := postTestDirs(t)
	row := "\x1b[37m" + strings.Repeat("\xdc\x1b[30m\xdc\x1b[37m ", 30)
	file := filepath.Join(t.TempDir(), "ad.ans")
	if err := os.WriteFile(file, append([]byte(row+"\r\n"), append([]byte{0x1A}, sauceRecord("Our Net")...)...), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runPost([]string{"--config", configDir, "--data", dataDir, "--area", "fd_othernets", "--file", file}, nil)
	if code != 0 {
		t.Fatalf("runPost exit %d", code)
	}

	msg := readPosted(t, configDir, dataDir, 2, 1)
	if msg.Subject != "Our Net" || msg.From != "Test Sysop" || msg.To != "All" {
		t.Errorf("header = %q / %q / %q", msg.Subject, msg.From, msg.To)
	}
	if !strings.HasPrefix(msg.Text, row+"\x1b[0m\r--- ") {
		t.Errorf("body not stored whole before the tearline:\n%q", msg.Text)
	}
	if !strings.Contains(msg.Text, " * Origin: ") || msg.MsgID == "" {
		t.Errorf("echomail trailer or MSGID missing: msgid=%q\n%q", msg.MsgID, msg.Text)
	}
	found := false
	for _, k := range msg.Kludges {
		found = found || k == chrsCP437
	}
	if !found {
		t.Errorf("kludges %q lack %q", msg.Kludges, chrsCP437)
	}
}

func TestRunPost_Netmail(t *testing.T) {
	configDir, dataDir := postTestDirs(t)
	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("Hi Sean,\nyour ad is wrapped.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	args := []string{"--config", configDir, "--data", dataDir, "--area", "3", "--subj", "Your ad", "--file", file}
	if code := runPost(append(args, "--to", "Sean Dennis"), nil); code != 1 {
		t.Errorf("netmail without an address: exit %d, want 1", code)
	}
	if code := runPost(append(args, "--to", "Sean Dennis@1:18/200"), nil); code != 0 {
		t.Fatalf("runPost exit %d", code)
	}

	msg := readPosted(t, configDir, dataDir, 3, 1)
	if msg.To != "Sean Dennis" || msg.DestAddr != "1:18/200" || msg.OrigAddr != "1:2/3" {
		t.Errorf("to=%q dest=%q orig=%q", msg.To, msg.DestAddr, msg.OrigAddr)
	}
	if !strings.HasPrefix(msg.Text, "Hi Sean,\ryour ad is wrapped.") {
		t.Errorf("body = %q", msg.Text)
	}
}

func TestRunPost_LocalStdinAndReply(t *testing.T) {
	configDir, dataDir := postTestDirs(t)
	base := []string{"--config", configDir, "--data", dataDir, "--area", "GENERAL", "--file", "-"}

	if code := runPost(append(base, "--subj", "First"), bytes.NewBufferString("hello\r\n")); code != 0 {
		t.Fatalf("first post: exit %d", code)
	}
	if code := runPost(append(base, "--subj", "Re: First", "--reply-to", "1", "--from", "Someone"), bytes.NewBufferString("hi")); code != 0 {
		t.Fatalf("reply: exit %d", code)
	}

	msg := readPosted(t, configDir, dataDir, 1, 2)
	if msg.Text != "hi" || msg.From != "Someone" || msg.Header.ReplyTo != 1 {
		t.Errorf("reply: text=%q from=%q replyTo=%d", msg.Text, msg.From, msg.Header.ReplyTo)
	}
	if len(msg.Kludges) != 0 {
		t.Errorf("local post got kludges %q", msg.Kludges)
	}
}

func TestRunPost_Refusals(t *testing.T) {
	configDir, dataDir := postTestDirs(t)
	run := func(stdin string, extra ...string) int {
		args := append([]string{"--config", configDir, "--data", dataDir, "--file", "-"}, extra...)
		return runPost(args, bytes.NewBufferString(stdin))
	}
	if code := run("x", "--area", "V3_GEN", "--subj", "s"); code != 1 {
		t.Errorf("v3net area: exit %d, want 1", code)
	}
	if code := run("x", "--area", "NOPE", "--subj", "s"); code != 1 {
		t.Errorf("unknown area: exit %d, want 1", code)
	}
	if code := run("x", "--area", "GENERAL"); code != 2 {
		t.Errorf("no subject: exit %d, want 2", code)
	}
	if code := run("x", "--area", "FD_OTHERNETS", "--subj", strings.Repeat("s", 72)); code != 1 {
		t.Errorf("long echomail subject: exit %d, want 1", code)
	}
	if code := run("x", "--area", "FD_OTHERNETS", "--subj", "s", "--to", "Sean@1:18/200"); code != 1 {
		t.Errorf("address outside a netmail area: exit %d, want 1", code)
	}
	if code := run("x", "--area", "FD_OTHERNETS", "--subj", "Café news"); code != 1 {
		t.Errorf("non-ASCII echomail subject: exit %d, want 1", code)
	}
	if code := run("x", "--area", "GENERAL", "--subj", "Café news", "--dry-run"); code != 0 {
		t.Errorf("non-ASCII local subject: exit %d, want 0", code)
	}
	if code := run("x", "--area", "GENERAL", "--subj", "s", "--dry-run"); code != 0 {
		t.Errorf("dry run: exit %d, want 0", code)
	}
	mm, err := message.NewMessageManager(dataDir, configDir, "Test BBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mm.Close() }()
	if n, _ := mm.GetMessageCountForArea(1); n != 0 {
		t.Errorf("dry run wrote %d messages", n)
	}
}

// PRIVMAIL is private by area, as in the BBS composer, and a private local
// message is visible only to the handle in its To field, so the recipient
// must be an existing account and is stored with that account's handle.
func TestRunPost_PrivateMail(t *testing.T) {
	configDir, dataDir := postTestDirs(t)
	run := func(extra ...string) int {
		args := append([]string{"--config", configDir, "--data", dataDir, "--file", "-", "--subj", "s"}, extra...)
		return runPost(args, bytes.NewBufferString("psst"))
	}
	if code := run("--area", "PRIVMAIL", "--to", "bob"); code != 0 {
		t.Fatalf("PRIVMAIL to bob: exit %d", code)
	}
	msg := readPosted(t, configDir, dataDir, 5, 1)
	if msg.To != "Bob" || msg.Header.Attribute&jam.MsgPrivate == 0 {
		t.Errorf("to=%q attr=%#x, want Bob and private", msg.To, msg.Header.Attribute)
	}

	for name, args := range map[string][]string{
		"PRIVMAIL to All":       {"--area", "PRIVMAIL"},
		"deleted user":          {"--area", "PRIVMAIL", "--to", "Gone"},
		"unknown user":          {"--area", "GENERAL", "--private", "--to", "Nobody"},
		"private echomail post": {"--area", "FD_OTHERNETS", "--private"},
	} {
		if code := run(args...); code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
	}
}

// Wrapping splits on ASCII blanks only; CP437 C2 A0 (┬á) is not a space.
func TestWrapPlainLines_CP437NotUnicodeSpace(t *testing.T) {
	got := wrapPlainLines([]string{"aaaa\xc2\xa0bbbb cc"}, 8)
	want := []string{"aaaa\xc2\xa0bb", "bb cc"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}
