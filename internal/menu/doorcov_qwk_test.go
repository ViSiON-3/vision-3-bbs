//go:build !windows

package menu

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
)

// doorcovQWKEnv is a BBS called "Doorcov BBS" (QWK ID DOORCOVB) whose temp
// files land in a directory of their own, returned for leak checks.
func doorcovQWKEnv(t *testing.T) (*menuEnv, string) {
	t.Helper()
	tmp := doorcovIsolateTemp(t)
	env := newMenuEnv(t)
	setServerField(env.e, func(c *config.ServerConfig) { c.BoardName = "Doorcov BBS"; c.QWKID = "" })
	return env, tmp
}

const doorcovQWKID = "DOORCOVB"

// doorcovSendProtocol installs a transfer protocol whose "send" copies the
// offered file into the returned directory under its own name and then exits
// with status, standing in for the caller's terminal receiving it.
func doorcovSendProtocol(t *testing.T, env *menuEnv, status string) string {
	t.Helper()
	dest := t.TempDir()
	script := doorcovScript(t, `cp "$2" "$1/" && exit `+status)
	env.e.SetProtocols([]transfer.ProtocolConfig{{
		Key: "T", Name: "Testmodem", SendCmd: "/bin/sh", SendArgs: []string{script, dest}, Default: true,
	}})
	return dest
}

// doorcovRecvProtocol installs a transfer protocol whose "receive" delivers
// packet into the upload directory as name; an empty name delivers nothing.
func doorcovRecvProtocol(t *testing.T, env *menuEnv, name string, packet []byte) {
	t.Helper()
	body := "exit 0"
	if name != "" {
		src := filepath.Join(t.TempDir(), "upload.rep")
		if err := os.WriteFile(src, packet, 0o644); err != nil {
			t.Fatal(err)
		}
		body = `cp "` + src + `" "./` + name + `"`
	}
	env.e.SetProtocols([]transfer.ProtocolConfig{{
		Key: "T", Name: "Testmodem", RecvCmd: "/bin/sh", RecvArgs: []string{doorcovScript(t, body)}, Default: true,
	}})
}

// doorcovREP builds a REP packet addressed to bbsID.
func doorcovREP(t *testing.T, bbsID string, msgs ...qwk.PacketMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := qwk.WriteREP(&buf, bbsID, msgs); err != nil {
		t.Fatalf("WriteREP: %v", err)
	}
	return buf.Bytes()
}

// doorcovConference is the QWK conference number the BBS has assigned areaTag.
func doorcovConference(t *testing.T, env *menuEnv, areaTag string) int {
	t.Helper()
	// Building a packet is what assigns and saves the numbers.
	svc := qwkservice.New(env.e.MessageMgr, doorcovQWKID, "Doorcov BBS", "Sysop", env.e.MessageMgr.DataPath())
	if _, err := svc.BuildPacket(qwkservice.ExportOptions{Handle: "nobody"}); err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	cm, err := qwkservice.LoadConferenceMap(filepath.Join(env.e.MessageMgr.DataPath(), "qwk_conferences.json"))
	if err != nil {
		t.Fatalf("LoadConferenceMap: %v", err)
	}
	entry, ok := cm.EntryForTag(areaTag)
	if !ok {
		t.Fatalf("no QWK conference for area %s", areaTag)
	}
	return entry.QWKNumber
}

// doorcovTempLeft lists what a handler left in the temp directory, ignoring
// the per-test directories the testing package keeps there.
func doorcovTempLeft(t *testing.T, tmp string) []string {
	t.Helper()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "Test") {
			left = append(left, e.Name())
		}
	}
	return left
}

func TestDoorcovQWKRequiresLogin(t *testing.T) {
	env, _ := doorcovQWKEnv(t)
	for _, cmd := range []string{"QWKDOWNLOAD", "QWKUPLOAD"} {
		r := env.runCmd(cmd, nil, "", "\r")
		if r.user != nil || r.next != "" || !r.has("Error: You must be logged in.") {
			t.Errorf("%s: user=%v next=%q output:\n%s", cmd, r.user, r.next, r.text())
		}
	}
}

func TestDoorcovQWKDownloadNothingNew(t *testing.T) {
	env, _ := doorcovQWKEnv(t)
	r := env.runCmd("QWKDOWNLOAD", env.sysop, "", "\r")
	if r.err != nil || r.user != env.sysop || !r.has("Building QWK packet...", "No new messages to download.") {
		t.Errorf("err=%v user=%v output:\n%s", r.err, r.user, r.text())
	}
	if r.has("Send QWK Packet") {
		t.Errorf("offered to send an empty packet:\n%s", r.text())
	}
}

// A packet that reaches the caller is named for the BBS, holds the new
// messages, and moves the caller's newscan pointer past them.
func TestDoorcovQWKDownloadSendsPacket(t *testing.T) {
	env, tmp := doorcovQWKEnv(t)
	env.generalMsgs(3)
	dest := doorcovSendProtocol(t, env, "0")

	r := doorcovRun(env, newDoorcovScripted("\r\r"), runQWKDownload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || r.next != "" {
		t.Errorf("err=%v user=%v next=%q", r.err, r.user, r.next)
	}
	if !r.has("3 message(s) packed into QWK packet.", "Send QWK Packet", "Testmodem",
		"Sending "+doorcovQWKID+".QWK via Testmodem...", "QWK packet sent successfully.") {
		t.Errorf("output:\n%s", r.text())
	}

	zr, err := zip.OpenReader(filepath.Join(dest, doorcovQWKID+".QWK"))
	if err != nil {
		t.Fatalf("packet not delivered as %s.QWK: %v", doorcovQWKID, err)
	}
	defer func() { _ = zr.Close() }()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, " "); !strings.Contains(got, "MESSAGES.DAT") || !strings.Contains(got, "CONTROL.DAT") {
		t.Errorf("packet holds %q, want MESSAGES.DAT and CONTROL.DAT", got)
	}

	if got := env.diskLastRead(generalAreaID, env.sysop.Handle); got != 3 {
		t.Errorf("last-read = %d after a successful download, want 3", got)
	}
	if left := doorcovTempLeft(t, tmp); len(left) != 0 {
		t.Errorf("packet files left in the temp directory: %v", left)
	}

	// Everything is now read, so a second download has nothing to send.
	r = doorcovRun(env, newDoorcovScripted("\r\r"), runQWKDownload, env.sysop, "")
	if !r.has("No new messages to download.") {
		t.Errorf("second download:\n%s", r.text())
	}
}

// Only tagged areas are packed when the caller has tagged any.
func TestDoorcovQWKDownloadTaggedAreasOnly(t *testing.T) {
	env, _ := doorcovQWKEnv(t)
	env.generalMsgs(2)
	env.sysop.TaggedMessageAreaTags = []string{"PRIVMAIL"}

	r := env.runCmd("QWKDOWNLOAD", env.sysop, "", "\r")
	if !r.has("No new messages to download.") {
		t.Errorf("untagged area's messages were packed:\n%s", r.text())
	}
}

// Every way of not completing the transfer leaves the newscan pointer where
// it was, so the same messages are offered next time.
func TestDoorcovQWKDownloadNotSent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    string
		protocol func(*testing.T, *menuEnv)
		want     string
		notWant  string
		logoff   bool
	}{
		{"quit at the send prompt", " q\r", nil, "2 message(s) packed", "Transfer Protocols:", false},
		{"disconnect at the send prompt", "", nil, "Send QWK Packet", "Transfer Protocols:", true},
		{"cancel at the protocol menu", "\rQ\r", nil, "Transfer Protocols:", "Sending", false},
		{"disconnect at the protocol menu", "\r", nil, "Transfer Protocols:", "Sending", true},
		{"no protocols configured", "\r", func(_ *testing.T, env *menuEnv) { env.e.SetProtocols(nil) },
			protocolSelectionErrorText(errNoTransferProtocols), "Sending", false},
		{"transfer program missing", "\r\r", func(_ *testing.T, env *menuEnv) {
			env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "T", Name: "Testmodem", SendCmd: "/nonexistent/sz", Default: true}})
		}, "Transfer program not found!", "sent successfully", false},
		{"transfer fails", "\r\r", func(t *testing.T, env *menuEnv) { doorcovSendProtocol(t, env, "1") },
			"Transfer failed.", "sent successfully", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, tmp := doorcovQWKEnv(t)
			env.generalMsgs(2)
			doorcovSendProtocol(t, env, "0")
			if tc.protocol != nil {
				tc.protocol(t, env)
			}

			r := doorcovRun(env, newDoorcovScripted(tc.input), runQWKDownload, env.sysop, "")
			if r.err != nil || !r.has(tc.want) || r.has(tc.notWant) {
				t.Errorf("err=%v, want %q and not %q in:\n%s", r.err, tc.want, tc.notWant, r.text())
			}
			if tc.logoff && (r.next != "LOGOFF" || r.user != nil) {
				t.Errorf("next=%q user=%v, want a logoff", r.next, r.user)
			}
			if !tc.logoff && (r.next != "" || r.user != env.sysop) {
				t.Errorf("next=%q user=%v, want the caller back at the menu", r.next, r.user)
			}
			if got := env.diskLastRead(generalAreaID, env.sysop.Handle); got != 0 {
				t.Errorf("last-read = %d though the packet was never delivered, want 0", got)
			}
			if left := doorcovTempLeft(t, tmp); len(left) != 0 {
				t.Errorf("packet files left in the temp directory: %v", left)
			}
		})
	}
}

// With nowhere to stage the packet the download is abandoned before any
// protocol is offered, and the newscan pointer stays put.
func TestDoorcovQWKDownloadNoTempSpace(t *testing.T) {
	env, tmp := doorcovQWKEnv(t)
	env.generalMsgs(2)
	doorcovSendProtocol(t, env, "0")
	t.Setenv("TMPDIR", filepath.Join(tmp, "missing"))

	r := doorcovRun(env, newDoorcovScripted("\r\r"), runQWKDownload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || r.next != "" || r.has("Transfer Protocols:") || !r.has(qwkPrepareFailedMsg) {
		t.Errorf("err=%v user=%v next=%q output:\n%s", r.err, r.user, r.next, r.text())
	}
	if got := env.diskLastRead(generalAreaID, env.sysop.Handle); got != 0 {
		t.Errorf("last-read = %d though nothing was sent, want 0", got)
	}
}

// If the packet cannot be given its BBSID.QWK name the caller is told, and the
// staged packet is not left behind.
func TestDoorcovQWKDownloadRenameFails(t *testing.T) {
	env, tmp := doorcovQWKEnv(t)
	env.generalMsgs(2)
	doorcovSendProtocol(t, env, "0")
	// A non-empty directory where the packet should go makes the rename fail.
	blocker := filepath.Join(tmp, doorcovQWKID+".QWK")
	if err := os.MkdirAll(filepath.Join(blocker, "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := doorcovRun(env, newDoorcovScripted("\r\r"), runQWKDownload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || r.next != "" || r.has("Transfer Protocols:") || !r.has(qwkPrepareFailedMsg) {
		t.Errorf("err=%v user=%v next=%q output:\n%s", r.err, r.user, r.next, r.text())
	}
	if got := env.diskLastRead(generalAreaID, env.sysop.Handle); got != 0 {
		t.Errorf("last-read = %d though nothing was sent, want 0", got)
	}
	if left := doorcovTempLeft(t, tmp); len(left) != 1 || left[0] != doorcovQWKID+".QWK" {
		t.Errorf("temp directory holds %v, want only the blocking directory", left)
	}
}

func TestDoorcovQWKDownloadBuildError(t *testing.T) {
	env, _ := doorcovQWKEnv(t)
	env.generalMsgs(1)
	// A conference map that cannot be parsed stops the packet being built.
	if err := os.WriteFile(filepath.Join(env.e.MessageMgr.DataPath(), "qwk_conferences.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("QWKDOWNLOAD", env.sysop, "", "\r")
	if r.err != nil || r.user != env.sysop || !r.has("Error building QWK packet.") || r.has("Send QWK Packet") {
		t.Errorf("err=%v user=%v output:\n%s", r.err, r.user, r.text())
	}
}

// An uploaded REP packet's replies are posted as the caller, signed, and
// counted; the same packet sent again posts nothing.
func TestDoorcovQWKUploadPostsReplies(t *testing.T) {
	env, tmp := doorcovQWKEnv(t)
	conf := doorcovConference(t, env, "GENERAL")
	env.sysop.AutoSignature = "-- Sam"
	rep := doorcovREP(t, doorcovQWKID,
		qwk.PacketMessage{Conference: conf, To: "All", Subject: "First reply", Body: "Hello from offline", DateTime: time.Now()},
		qwk.PacketMessage{Conference: conf, To: "Caller", Subject: "Second reply", Body: "And another", DateTime: time.Now()},
		qwk.PacketMessage{Conference: 9999, To: "All", Subject: "Nowhere", Body: "No such conference", DateTime: time.Now()},
	)
	// Readers are free with the packet's filename case.
	doorcovRecvProtocol(t, env, strings.ToLower(doorcovQWKID)+".rep", rep)

	r := doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || r.next != "" {
		t.Errorf("err=%v user=%v next=%q", r.err, r.user, r.next)
	}
	if !r.has("Send your "+doorcovQWKID+".REP file via Testmodem now.", "Total Processed: 2") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := strings.Count(r.text(), "Posting Message on (General Discussion)"); got != 2 {
		t.Errorf("announced %d posts, want 2:\n%s", got, r.text())
	}

	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL holds %d messages, want 2", n)
	}
	m := env.mustMsg(generalAreaID, 1)
	if m.From != "Sysop" || m.To != "All" || m.Subject != "First reply" {
		t.Errorf("posted %q -> %q %q, want Sysop -> All \"First reply\"", m.From, m.To, m.Subject)
	}
	if !strings.Contains(m.Body, "Hello from offline") || !strings.Contains(m.Body, "-- Sam") {
		t.Errorf("body = %q, want the reply followed by the signature", m.Body)
	}
	if got := env.mustDiskUser(env.sysop.ID).MessagesPosted; got != 2 {
		t.Errorf("saved MessagesPosted = %d, want 2", got)
	}
	if left := doorcovTempLeft(t, tmp); len(left) != 0 {
		t.Errorf("upload files left in the temp directory: %v", left)
	}

	r = doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.sysop, "")
	if !r.has("This packet was already uploaded - nothing posted.") {
		t.Errorf("duplicate upload:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("duplicate upload posted: GENERAL holds %d messages, want 2", n)
	}
	if got := env.mustDiskUser(env.sysop.ID).MessagesPosted; got != 2 {
		t.Errorf("duplicate upload counted: MessagesPosted = %d, want 2", got)
	}
}

// A caller who may not post in an area has their replies to it dropped: GENERAL
// needs level 20 to write and the caller has 10.
func TestDoorcovQWKUploadRespectsWriteACS(t *testing.T) {
	env, _ := doorcovQWKEnv(t)
	conf := doorcovConference(t, env, "GENERAL")
	doorcovRecvProtocol(t, env, "ANYNAME.REP", doorcovREP(t, doorcovQWKID,
		qwk.PacketMessage{Conference: conf, To: "All", Subject: "Sneaky", Body: "Should not post", DateTime: time.Now()}))

	r := doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.caller, "")
	if r.err != nil || !r.has("Total Processed: 0") || r.has("Posting Message on") {
		t.Errorf("err=%v output:\n%s", r.err, r.text())
	}
	if n := env.msgCount(generalAreaID); n != 0 {
		t.Errorf("GENERAL holds %d messages, want none", n)
	}
	if got := env.mustDiskUser(env.caller.ID).MessagesPosted; got != 0 {
		t.Errorf("saved MessagesPosted = %d, want 0", got)
	}
}

func TestDoorcovQWKUploadRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		packet func(t *testing.T, conf int) []byte
		want   string
	}{
		{"nothing received", "", nil, "No REP packet received."},
		{"not a REP file", doorcovQWKID + ".QWK", func(t *testing.T, conf int) []byte {
			return doorcovREP(t, doorcovQWKID, qwk.PacketMessage{Conference: conf, To: "All", Subject: "s", Body: "b", DateTime: time.Now()})
		}, "No REP packet received."},
		{"addressed to another BBS", "OTHERBBS.REP", func(t *testing.T, conf int) []byte {
			return doorcovREP(t, "OTHERBBS", qwk.PacketMessage{Conference: conf, To: "All", Subject: "s", Body: "b", DateTime: time.Now()})
		}, "This REP packet is addressed to another BBS."},
		{"not a packet at all", doorcovQWKID + ".REP", func(*testing.T, int) []byte { return []byte("not a zip") },
			"Error processing REP packet."},
		{"no messages", doorcovQWKID + ".REP", func(t *testing.T, _ int) []byte { return doorcovREP(t, doorcovQWKID) },
			"REP packet contains no messages."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, tmp := doorcovQWKEnv(t)
			var packet []byte
			if tc.packet != nil {
				packet = tc.packet(t, doorcovConference(t, env, "GENERAL"))
			}
			doorcovRecvProtocol(t, env, tc.file, packet)

			r := doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.sysop, "")
			if r.err != nil || r.user != env.sysop || r.next != "" || !r.has(tc.want) {
				t.Errorf("err=%v user=%v next=%q, want %q in:\n%s", r.err, r.user, r.next, tc.want, r.text())
			}
			if r.has("Total Processed") {
				t.Errorf("rejected packet reported as processed:\n%s", r.text())
			}
			if n := env.msgCount(generalAreaID); n != 0 {
				t.Errorf("GENERAL holds %d messages, want none", n)
			}
			if got := env.mustDiskUser(env.sysop.ID).MessagesPosted; got != 0 {
				t.Errorf("saved MessagesPosted = %d, want 0", got)
			}
			if left := doorcovTempLeft(t, tmp); len(left) != 0 {
				t.Errorf("upload files left in the temp directory: %v", left)
			}
		})
	}
}

// A receive that fails outright is treated like one that delivered nothing.
func TestDoorcovQWKUploadTransferFails(t *testing.T) {
	env, tmp := doorcovQWKEnv(t)
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "T", Name: "Testmodem", RecvCmd: "/nonexistent/rz", Default: true}})

	r := doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || !r.has("No REP packet received.") {
		t.Errorf("err=%v user=%v output:\n%s", r.err, r.user, r.text())
	}
	if left := doorcovTempLeft(t, tmp); len(left) != 0 {
		t.Errorf("upload files left in the temp directory: %v", left)
	}

	// With nowhere to receive into, the transfer is never started.
	marker := filepath.Join(t.TempDir(), "ran")
	env.e.SetProtocols([]transfer.ProtocolConfig{{Key: "T", Name: "Testmodem", RecvCmd: "touch", RecvArgs: []string{marker}, Default: true}})
	t.Setenv("TMPDIR", filepath.Join(tmp, "missing"))
	r = doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || r.has("No REP packet received.") || !r.has("Error preparing to receive the REP packet.") {
		t.Errorf("no temp space: err=%v user=%v output:\n%s", r.err, r.user, r.text())
	}
	if doorcovExists(marker) {
		t.Error("receive ran with no directory to receive into")
	}
}

// A received REP that cannot be read is reported, not silently dropped.
func TestDoorcovQWKUploadUnreadableREP(t *testing.T) {
	env, tmp := doorcovQWKEnv(t)
	// A directory named like the packet is found but cannot be read as one.
	env.e.SetProtocols([]transfer.ProtocolConfig{{
		Key: "T", Name: "Testmodem", RecvCmd: "/bin/sh", RecvArgs: []string{doorcovScript(t, "mkdir ./"+doorcovQWKID+".REP")}, Default: true,
	}})

	r := doorcovRun(env, newDoorcovScripted("\r"), runQWKUpload, env.sysop, "")
	if r.err != nil || r.user != env.sysop || r.next != "" || !r.has("Error reading REP packet.") || r.has("Total Processed") {
		t.Errorf("err=%v user=%v next=%q output:\n%s", r.err, r.user, r.next, r.text())
	}
	if left := doorcovTempLeft(t, tmp); len(left) != 0 {
		t.Errorf("upload files left in the temp directory: %v", left)
	}
}

func TestDoorcovQWKUploadProtocolPrompt(t *testing.T) {
	env, _ := doorcovQWKEnv(t)
	doorcovRecvProtocol(t, env, "", nil)

	r := env.runCmd("QWKUPLOAD", env.sysop, "", "Q\r")
	if r.user != env.sysop || r.next != "" || !r.has("Transfer Protocols:") || r.has("Send your") {
		t.Errorf("cancel: user=%v next=%q output:\n%s", r.user, r.next, r.text())
	}

	r = env.runCmd("QWKUPLOAD", env.sysop, "", "")
	if r.user != nil || r.next != "LOGOFF" {
		t.Errorf("disconnect: user=%v next=%q, want a logoff", r.user, r.next)
	}

	env.e.SetProtocols(nil)
	r = env.runCmd("QWKUPLOAD", env.sysop, "", "\r")
	if r.user != env.sysop || r.next != "" || r.has("Send your") || !r.has(protocolSelectionErrorText(errNoTransferProtocols)) {
		t.Errorf("no protocols: user=%v next=%q output:\n%s", r.user, r.next, r.text())
	}
}
