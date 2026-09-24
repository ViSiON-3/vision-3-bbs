package qwknet

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

type env struct {
	dir    string
	msgMgr *message.MessageManager
	paths  config.QWKNetConfig
	cfg    config.QWKNetworkConfig
	dupes  *tosser.DupeDB
}

// newEnv builds a BBS with two DOVE-Net style areas (2001 general, 2006
// programming) on network "dovenet", plus an unrelated local area.
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "configs")
	dataDir := filepath.Join(dir, "data")
	for _, d := range []string{configDir, filepath.Join(dataDir, "msgbases", "dove_gen"),
		filepath.Join(dataDir, "msgbases", "dove_prog"), filepath.Join(dataDir, "msgbases", "local")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	areas := []message.MessageArea{
		{ID: 1, Tag: "DOVE_GEN", Name: "DOVE-Net General", BasePath: "msgbases/dove_gen", AreaType: "qwknet", Network: "dovenet", QWKConference: 2001},
		{ID: 2, Tag: "DOVE_PROG", Name: "DOVE-Net Programming", BasePath: "msgbases/dove_prog", AreaType: "qwknet", Network: "dovenet", QWKConference: 2006},
		{ID: 3, Tag: "LOCAL", Name: "Local", BasePath: "msgbases/local", AreaType: "local"},
	}
	data, _ := json.Marshal(areas)
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := config.QWKNetConfig{}
	paths.ResolvePaths(dir)
	dupes, err := OpenDupeDB(paths.DupeDBPath)
	if err != nil {
		t.Fatal(err)
	}
	return &env{dir: dir, msgMgr: mm, paths: paths, dupes: dupes,
		cfg: config.QWKNetworkConfig{Enabled: true, Name: "DOVE-Net", HubID: "VERT", Host: "127.0.0.1", Password: "pw", Tagline: "Test Node"}}
}

func (e *env) node(t *testing.T) *Node {
	t.Helper()
	n, err := New("dovenet", e.cfg, e.paths, "VISION3", e.msgMgr, e.dupes)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// hubPacket builds a QWK packet the way a hub would send it.
func hubPacket(t *testing.T, msgs ...qwk.PacketMessage) []byte {
	t.Helper()
	pw := qwk.NewPacketWriter("VERT", "Vertrauen", "Digital Man")
	pw.AddConference(2001, "DOVE-Net General")
	pw.AddConference(2006, "Programming")
	for _, m := range msgs {
		pw.AddMessage(m)
	}
	var buf bytes.Buffer
	if err := pw.WritePacket(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readREP(t *testing.T, path string) []qwk.NetMessage {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	st, _ := f.Stat()
	p, err := qwk.ReadREPPacket(f, st.Size(), "VERT")
	if err != nil {
		t.Fatal(err)
	}
	hdrs, _ := qwk.ReadArchiveHeaders(f, st.Size())
	id, msgs, perr := qwk.ParseMSGPayload(p.Payload, hdrs)
	if perr != nil || id != "VERT" {
		t.Fatalf("REP payload: id=%q err=%v", id, perr)
	}
	return msgs
}

func TestScan_ExportsLocalPostsOnce(t *testing.T) {
	e := newEnv(t)
	if _, err := e.msgMgr.AddMessage(1, "Robbie", "All", "Hello DOVE-Net", "First post\nsecond line", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.msgMgr.AddMessage(3, "Robbie", "All", "Local only", "stays home", ""); err != nil {
		t.Fatal(err)
	}
	n := e.node(t)
	res := n.Scan()
	if len(res.Errors) != 0 || res.Exported != 1 || res.REPPath == "" {
		t.Fatalf("scan: %+v", res)
	}
	msgs := readREP(t, res.REPPath)
	if len(msgs) != 1 {
		t.Fatalf("REP holds %d messages", len(msgs))
	}
	m := msgs[0]
	if m.Conference != 2001 || m.From != "Robbie" || m.Subject != "Hello DOVE-Net" {
		t.Errorf("message = %+v", m)
	}
	if !strings.HasPrefix(m.Body, "First post\nsecond line") || !strings.Contains(m.Body, "\xfe ViSiON/3 \xfe Test Node") {
		t.Errorf("body = %q", m.Body)
	}
	if m.MessageID != "<1.dove_gen@vision3>" {
		t.Errorf("Message-ID = %q", m.MessageID)
	}
	if m.SenderNetAddr != "VISION3" {
		t.Errorf("SenderNetAddr = %q", m.SenderNetAddr)
	}

	// Nothing new: the REP stays as it is and nothing is exported twice.
	again := n.Scan()
	if again.Exported != 0 || again.Pending != 1 || again.REPPath != res.REPPath {
		t.Fatalf("second scan: %+v", again)
	}
}

func TestScan_AppendsToPendingREP(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	if _, err := e.msgMgr.AddMessage(1, "A", "All", "one", "1", ""); err != nil {
		t.Fatal(err)
	}
	n.Scan()
	if _, err := e.msgMgr.AddMessage(2, "B", "All", "two", "2", ""); err != nil {
		t.Fatal(err)
	}
	res := n.Scan()
	if res.Exported != 1 || res.Pending != 1 {
		t.Fatalf("scan: %+v", res)
	}
	msgs := readREP(t, res.REPPath)
	if len(msgs) != 2 || msgs[0].Subject != "one" || msgs[1].Subject != "two" || msgs[1].Conference != 2006 {
		t.Fatalf("merged REP: %+v", msgs)
	}
	if strings.Count(msgs[0].Body, "---") != 1 {
		t.Errorf("re-packed message gained a second tearline: %q", msgs[0].Body)
	}
}

func TestScan_ReplyCarriesParentMessageID(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	parent, err := e.msgMgr.AddMessage(1, "A", "All", "parent", "p", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.msgMgr.AddReply(1, "B", "A", "Re: parent", "r", "", parent); err != nil {
		t.Fatal(err)
	}
	res := n.Scan()
	msgs := readREP(t, res.REPPath)
	if len(msgs) != 2 || msgs[1].ReplyID != msgs[0].MessageID || msgs[1].ReplyID == "" {
		t.Fatalf("reply threading lost: %+v", msgs)
	}
}

func TestToss_ImportsRoutesDedupsAndNeverReexports(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pkt := hubPacket(t,
		qwk.PacketMessage{Conference: 2001, Number: 500, From: "Digital Man", To: "All", Subject: "Welcome", DateTime: when, Body: "Hi all\n---\n \xfe Synchronet \xfe Vertrauen"},
		qwk.PacketMessage{Conference: 2006, Number: 501, From: "Coder", To: "Robbie", Subject: "Re: Go", DateTime: when, Body: "Nice"},
		qwk.PacketMessage{Conference: 2099, Number: 502, From: "Nobody", To: "All", Subject: "Unmapped", DateTime: when, Body: "x"},
	)
	path := filepath.Join(e.paths.InboundPath, "VERT.QWK")
	if err := os.WriteFile(path, pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	res := n.Toss()
	if len(res.Errors) != 0 || res.Packets != 1 || res.Imported != 2 || res.Unmapped != 1 {
		t.Fatalf("toss: %+v", res)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("tossed packet not removed")
	}
	dm, err := e.msgMgr.GetMessage(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if dm.From != "Digital Man" || dm.Subject != "Welcome" || !strings.HasPrefix(dm.Body, "Hi all") || !dm.DateTime.Equal(when) {
		t.Errorf("imported message = %+v", dm)
	}
	if dm.MsgID == "" {
		t.Error("imported message lost its Message-ID")
	}

	// The same packet again: every message is a duplicate.
	if err := os.WriteFile(path, pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	res = n.Toss()
	if res.Imported != 0 || res.Duplicates != 2 {
		t.Fatalf("re-toss: %+v", res)
	}
	if c, _ := e.msgMgr.GetMessageCountForArea(1); c != 1 {
		t.Errorf("area 1 has %d messages, want 1", c)
	}

	// Imported mail must not go back to the hub.
	scan := n.Scan()
	if scan.Exported != 0 || scan.REPPath != "" {
		t.Fatalf("imported messages were re-exported: %+v", scan)
	}
}

// Replies thread to their parent whether it arrived in an earlier packet or
// earlier in the same one.
func TestToss_ThreadsRepliesAcrossAndWithinPackets(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	toss := func(msgs ...qwk.PacketMessage) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), hubPacket(t, msgs...), 0o644); err != nil {
			t.Fatal(err)
		}
		if res := n.Toss(); len(res.Errors) != 0 {
			t.Fatalf("toss: %+v", res)
		}
	}
	msg := func(num int, body string) qwk.PacketMessage {
		return qwk.PacketMessage{Conference: 2001, Number: num, From: "A", To: "All", Subject: "Thread", DateTime: when, Body: body}
	}
	// The packet writer's HEADERS.DAT names each message <number.conf@vert>.
	toss(msg(1, "root"))
	toss(
		msg(2, "@REPLY: <1.2001@vert>\nreply to an earlier packet"),
		msg(3, "@REPLY: <2.2001@vert>\nreply within this packet"),
	)

	base, err := e.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	for num, want := range map[int]uint32{2: 1, 3: 2} {
		hdr, err := base.ReadMessageHeader(num)
		if err != nil {
			t.Fatal(err)
		}
		if hdr.ReplyTo != want {
			t.Errorf("message %d ReplyTo = %d, want %d", num, hdr.ReplyTo, want)
		}
	}
}

// @REPLYTO is kept on the imported message and stripped from its text.
func TestToss_KeepsReplyTo(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2001, Number: 1, From: "List Bot", To: "All", Subject: "Digest",
		DateTime: time.Now(), Body: "@REPLYTO: Moderator\nToday's digest"})
	if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := n.Toss(); len(res.Errors) != 0 || res.Imported != 1 {
		t.Fatalf("toss: %+v", res)
	}
	base, err := e.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	msg, err := base.ReadMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range msg.Kludges {
		if strings.TrimPrefix(k, "\x01") == "QWKREPLYTO: Moderator" {
			found = true
		}
	}
	if !found {
		t.Errorf("kludges = %q, want QWKREPLYTO: Moderator", msg.Kludges)
	}
	if strings.Contains(msg.Text, "@REPLYTO") {
		t.Errorf("kludge left in the text: %q", msg.Text)
	}
}

// A second bad packet must not overwrite the first one set aside: the next
// download reuses the plain name, so both land on <HUB>.QWK in turn.
func TestToss_SetAsideKeepsEveryBadPacket(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	path := filepath.Join(e.paths.InboundPath, "VERT.QWK")
	for i, body := range []string{"first garbage", "second garbage"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if res := n.Toss(); len(res.Errors) == 0 {
			t.Fatalf("toss %d of garbage reported no error", i)
		}
	}
	bad, _ := filepath.Glob(filepath.Join(e.paths.InboundPath, "VERT.QWK*.bad"))
	if len(bad) != 2 {
		t.Fatalf(".bad files = %v, want two", bad)
	}
	seen := map[string]bool{}
	for _, b := range bad {
		data, _ := os.ReadFile(b)
		seen[string(data)] = true
	}
	if !seen["first garbage"] || !seen["second garbage"] {
		t.Errorf("a set-aside packet was overwritten: %v", seen)
	}
}

// A message already in the base is a duplicate even when the dupe database
// never recorded it, as after a crash between the write and the save.
func TestToss_MessageAlreadyInBaseIsDuplicateWithoutDupeDB(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2001, Number: 7, From: "A", To: "All", Subject: "Once", DateTime: time.Now(), Body: "only once"})
	path := filepath.Join(e.paths.InboundPath, "VERT.QWK")
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(path, pkt, 0o644); err != nil {
			t.Fatal(err)
		}
		n.dupes = nil // as if every save had been lost
		res := n.Toss()
		if len(res.Errors) != 0 {
			t.Fatalf("toss %d: %+v", i, res)
		}
		if i == 1 && (res.Imported != 0 || res.Duplicates != 1) {
			t.Errorf("re-toss: %+v, want the message counted as a duplicate", res)
		}
	}
	if c, _ := e.msgMgr.GetMessageCountForArea(1); c != 1 {
		t.Errorf("area holds %d messages, want 1", c)
	}
}

// Tosses run one at a time across processes: while another holds the toss
// lock, a toss waits, then gives up without touching the inbound packets.
func TestToss_WaitsForAnotherToss(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	held, err := filelock.Acquire(filepath.Join(e.paths.InboundPath, "toss"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	prev := tossLockTimeout
	tossLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { tossLockTimeout = prev })

	path := filepath.Join(e.paths.InboundPath, "VERT.QWK")
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2001, Number: 1, From: "A", To: "All", Subject: "S", DateTime: time.Now(), Body: "b"})
	if err := os.WriteFile(path, pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	res := n.Toss()
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "another toss") || res.Packets != 0 {
		t.Fatalf("toss under a held lock: %+v", res)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("packet was touched while another toss held the lock")
	}
}

// Scan takes the REP lock: while another process holds it, a scan waits,
// then gives up without marking anything exported.
func TestScan_WaitsForREPLock(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	if _, err := e.msgMgr.AddMessage(1, "Robbie", "All", "queued", "wait for the lock", ""); err != nil {
		t.Fatal(err)
	}
	held, err := filelock.Acquire(n.repPath(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	prev := repLockTimeout
	repLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { repLockTimeout = prev })

	if res := n.Scan(); res.Exported != 0 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "busy") {
		t.Fatalf("scan under a held lock: %+v", res)
	}
	held.Release()
	if res := n.Scan(); res.Exported != 1 || len(res.Errors) != 0 {
		t.Fatalf("scan after release: %+v (the first scan must not have marked the message)", res)
	}
}

// A hub packet past the download cap is abandoned and its partial file
// removed, before anything can fill the inbound disk.
func TestPoll_DownloadOverCapIsAbandoned(t *testing.T) {
	e := newEnv(t)
	prev := maxPacketDownload
	maxPacketDownload = 1024
	t.Cleanup(func() { maxPacketDownload = prev })
	srv := newFakeFTP(t, "VISION3", "pw", map[string][]byte{"VERT.QWK": bytes.Repeat([]byte("x"), 64<<10)})
	host, port, _ := strings.Cut(srv.addr(), ":")
	e.cfg.Host = host
	e.cfg.Port = atoi(port)
	n := e.node(t)

	res := n.Poll(context.Background())
	if res.Downloaded || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "size limit") {
		t.Fatalf("poll: %+v", res)
	}
	left, _ := os.ReadDir(e.paths.InboundPath)
	for _, f := range left {
		if strings.Contains(f.Name(), "QWK") {
			t.Errorf("oversized download left %s behind", f.Name())
		}
	}
}

// Toss purges dupe entries past the retention window.
func TestToss_PurgesExpiredDupes(t *testing.T) {
	e := newEnv(t)
	old := time.Now().Add(-dupeRetention - 24*time.Hour).Unix()
	fresh := time.Now().Unix()
	data := fmt.Sprintf(`{"entries":{"<old@vert>":%d,"<fresh@vert>":%d}}`, old, fresh)
	if err := os.WriteFile(e.paths.DupeDBPath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	dupes, err := OpenDupeDB(e.paths.DupeDBPath)
	if err != nil {
		t.Fatal(err)
	}
	e.dupes = dupes
	e.node(t).Toss()
	after, err := OpenDupeDB(e.paths.DupeDBPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.IsDupe("<old@vert>") || !after.IsDupe("<fresh@vert>") {
		t.Errorf("after toss: old=%v fresh=%v, want old purged and fresh kept", after.IsDupe("<old@vert>"), after.IsDupe("<fresh@vert>"))
	}
}

func TestToss_DropsLoopedAndForeignPackets(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	// A message that already passed through this node, per its @VIA route.
	pw := qwk.NewPacketWriter("VERT", "Vertrauen", "DM")
	pw.AddConference(2001, "General")
	pw.AddMessage(qwk.PacketMessage{Conference: 2001, Number: 1, From: "X", To: "All", Subject: "loop", DateTime: time.Now(), Body: "@VIA: VERT/VISION3\nlooped"})
	var buf bytes.Buffer
	if err := pw.WritePacket(&buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res := n.Toss()
	if res.Skipped != 1 || res.Imported != 0 || len(res.Errors) != 0 {
		t.Fatalf("looped message: %+v", res)
	}

	// A packet from some other hub is set aside untouched.
	other := qwk.NewPacketWriter("OTHER", "Other", "O")
	buf.Reset()
	if err := other.WritePacket(&buf); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(e.paths.InboundPath, "VERT-2.QWK")
	if err := os.WriteFile(foreign, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res = n.Toss()
	if len(res.Errors) != 1 {
		t.Fatalf("foreign packet: %+v", res)
	}
	if _, err := os.Stat(foreign + ".bad"); err != nil {
		t.Error("foreign packet not set aside as .bad")
	}
}

func TestPoll_EndToEnd(t *testing.T) {
	e := newEnv(t)
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2006, Number: 9, From: "Hub User", To: "All", Subject: "From hub", DateTime: time.Now(), Body: "hub says hi"})
	srv := newFakeFTP(t, "VISION3", "pw", map[string][]byte{"VERT.QWK": pkt})
	host, port, _ := strings.Cut(srv.addr(), ":")
	e.cfg.Host = host
	e.cfg.Port = atoi(port)
	n := e.node(t)

	if _, err := e.msgMgr.AddMessage(1, "Robbie", "All", "outbound", "to the hub", ""); err != nil {
		t.Fatal(err)
	}
	res := n.Poll(context.Background())
	if len(res.Errors) != 0 || !res.Uploaded || !res.Downloaded || res.Scan.Exported != 1 || res.Toss.Imported != 1 {
		t.Fatalf("poll: %+v", res)
	}
	rep := srv.upload("VERT.REP")
	p, err := qwk.ReadREPPacket(bytes.NewReader(rep), int64(len(rep)), "VERT")
	if err != nil || len(p.Messages) != 1 || p.Messages[0].Subject != "outbound" {
		t.Fatalf("uploaded REP: %+v err=%v", p, err)
	}
	if _, err := os.Stat(n.repPath()); !os.IsNotExist(err) {
		t.Error("REP left behind after upload")
	}
	if c, _ := e.msgMgr.GetMessageCountForArea(2); c != 1 {
		t.Errorf("hub message not tossed into area 2 (count %d)", c)
	}

	// Second poll: the hub has nothing, which it answers with 550.
	srv.remove("VERT.QWK")
	res = n.Poll(context.Background())
	if len(res.Errors) != 0 || res.Uploaded || res.Downloaded || res.Toss.Imported != 0 {
		t.Fatalf("quiet poll: %+v", res)
	}
}

func TestPoll_HubDownStillPacksREP(t *testing.T) {
	e := newEnv(t)
	e.cfg.Host = "127.0.0.1"
	e.cfg.Port = 1 // nothing listens here
	e.cfg.TimeoutSeconds = 1
	n := e.node(t)
	if _, err := e.msgMgr.AddMessage(1, "Robbie", "All", "queued", "wait for hub", ""); err != nil {
		t.Fatal(err)
	}
	res := n.Poll(context.Background())
	if len(res.Errors) == 0 || res.Scan.Exported != 1 {
		t.Fatalf("poll with hub down: %+v", res)
	}
	if _, err := os.Stat(n.repPath()); err != nil {
		t.Error("REP should wait in outbound for the next poll")
	}
}

func TestPoll_RefusedUploadStillDownloads(t *testing.T) {
	e := newEnv(t)
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2006, Number: 9, From: "Hub User", To: "All", Subject: "From hub", DateTime: time.Now(), Body: "hub says hi"})
	srv := newFakeFTP(t, "VISION3", "pw", map[string][]byte{"VERT.QWK": pkt})
	srv.setStorCode(553)
	host, port, _ := strings.Cut(srv.addr(), ":")
	e.cfg.Host = host
	e.cfg.Port = atoi(port)
	n := e.node(t)

	if _, err := e.msgMgr.AddMessage(1, "Robbie", "All", "outbound", "to the hub", ""); err != nil {
		t.Fatal(err)
	}
	res := n.Poll(context.Background())
	if res.Uploaded || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "upload REP") {
		t.Fatalf("want exactly the upload error: %+v", res)
	}
	if !res.Downloaded || res.Toss.Imported != 1 {
		t.Fatalf("refused upload stopped the download: %+v", res)
	}
	if _, err := os.Stat(n.repPath()); err != nil {
		t.Error("REP should wait in outbound for the next poll")
	}
	if parts, _ := filepath.Glob(filepath.Join(e.paths.InboundPath, "*.part")); len(parts) != 0 {
		t.Errorf("partial download left behind: %v", parts)
	}
}

// An uploaded REP that cannot be deleted must not be read back as pending
// and uploaded again. A read-only outbound directory blocks both removal and
// the rename aside, leaving the emptying fallback.
func TestPoll_UndeletableUploadedREPIsNotResent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs unix directory permissions enforced")
	}
	e := newEnv(t)
	srv := newFakeFTP(t, "VISION3", "pw", map[string][]byte{})
	host, port, _ := strings.Cut(srv.addr(), ":")
	e.cfg.Host = host
	e.cfg.Port = atoi(port)
	n := e.node(t)

	if _, err := e.msgMgr.AddMessage(1, "Robbie", "All", "once only", "send me once", ""); err != nil {
		t.Fatal(err)
	}
	if res := n.Scan(); res.Exported != 1 {
		t.Fatalf("scan: %+v", res)
	}
	if err := os.Chmod(e.paths.OutboundPath, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.paths.OutboundPath, 0o755) })

	res := n.Poll(context.Background())
	if !res.Uploaded || len(res.Errors) != 0 {
		t.Fatalf("poll: %+v", res)
	}
	if st, err := os.Stat(n.repPath()); err != nil || st.Size() != 0 {
		t.Fatalf("uploaded REP should be emptied in place: %v %v", st, err)
	}
	if again := n.Scan(); again.Pending != 0 || len(again.Errors) != 0 {
		t.Errorf("delivered messages read back as pending: %+v", again)
	}
	if res := n.Poll(context.Background()); res.Uploaded {
		t.Error("emptied REP was uploaded again")
	}
}

func TestFetchConferences(t *testing.T) {
	e := newEnv(t)
	pkt := hubPacket(t)
	srv := newFakeFTP(t, "VISION3", "pw", map[string][]byte{"VERT.QWK": pkt})
	host, port, _ := strings.Cut(srv.addr(), ":")
	e.cfg.Host = host
	e.cfg.Port = atoi(port)
	n := e.node(t)
	confs, err := n.FetchConferences(context.Background())
	if err != nil || len(confs) != 2 || confs[0].Number != 2001 || confs[1].Name != "Programming" {
		t.Fatalf("confs=%+v err=%v", confs, err)
	}
	// The packet is kept for tossing, and answers the next call offline.
	if len(n.inboundPackets()) != 1 {
		t.Error("downloaded packet not kept in inbound")
	}
	srv.remove("VERT.QWK")
	if again, err := n.FetchConferences(context.Background()); err != nil || len(again) != 2 {
		t.Fatalf("offline conference read: %+v err=%v", again, err)
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestInboundPackets_ExactNamesAndOrder(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	for _, name := range []string{"VERT-1700000002.QWK", "VERTX.QWK", "vert.qwk", "VERT-1700000001.QWK", "VERT-abc.QWK", "OTHER.QWK", "VERT.QWK.123456.part"} {
		if err := os.WriteFile(filepath.Join(e.paths.InboundPath, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := n.inboundPackets()
	want := []string{"vert.qwk", "VERT-1700000001.QWK", "VERT-1700000002.QWK"}
	if len(got) != len(want) {
		t.Fatalf("inboundPackets = %v", got)
	}
	for i := range want {
		if filepath.Base(got[i]) != want[i] {
			t.Errorf("order[%d] = %s want %s", i, filepath.Base(got[i]), want[i])
		}
	}
}

func TestToss_RejectsPacketWithoutHubID(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	// MESSAGES.DAT only, no CONTROL.DAT: must not be imported blind.
	pw := qwk.NewPacketWriter("VERT", "V", "S")
	var full bytes.Buffer
	if err := pw.WritePacket(&full); err != nil {
		t.Fatal(err)
	}
	stripped := rezipWithout(t, full.Bytes(), "CONTROL.DAT")
	path := filepath.Join(e.paths.InboundPath, "VERT.QWK")
	if err := os.WriteFile(path, stripped, 0o644); err != nil {
		t.Fatal(err)
	}
	res := n.Toss()
	if len(res.Errors) != 1 || res.Imported != 0 {
		t.Fatalf("toss: %+v", res)
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Error("packet without hub ID not set aside")
	}
}

func TestToss_BadAreaCatchesUnmappedConferences(t *testing.T) {
	e := newEnv(t)
	e.paths.BadAreaTag = "LOCAL"
	n := e.node(t)
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2099, Number: 1, From: "X", To: "All", Subject: "stray", DateTime: time.Now(), Body: "x"})
	if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	res := n.Toss()
	if len(res.Errors) != 0 || res.Unmapped != 1 || res.Imported != 1 {
		t.Fatalf("toss: %+v", res)
	}
	if c, _ := e.msgMgr.GetMessageCountForArea(3); c != 1 {
		t.Errorf("bad area holds %d messages, want 1", c)
	}
}

func TestScan_AdvancesPointerPastImportedMail(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	pkt := hubPacket(t,
		qwk.PacketMessage{Conference: 2001, Number: 1, From: "A", To: "All", Subject: "one", DateTime: time.Now(), Body: "1"},
		qwk.PacketMessage{Conference: 2001, Number: 2, From: "B", To: "All", Subject: "two", DateTime: time.Now(), Body: "2"},
	)
	if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := n.Toss(); res.Imported != 2 {
		t.Fatalf("toss: %+v", res)
	}
	n.Scan()
	base, err := e.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	hwm := highWater(base)
	_ = base.Close()
	if hwm != 2 {
		t.Errorf("export pointer = %d, want 2 (imported mail skipped without rescanning)", hwm)
	}
}

func TestScan_SkipsMessagesWithQWKViaEvenIfUnmarked(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	// Simulate an import whose DateProcessed mark failed: the message has the
	// QWKVIA kludge but DateProcessed is zero.
	base, err := e.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	jm := jam.NewMessage()
	jm.From, jm.To, jm.Subject, jm.Text = "Hub User", "All", "from hub", "body"
	jm.Kludges = []string{"QWKVIA: VERT"}
	if _, err := base.WriteMessageExt(jm, jam.MsgTypeEchomailMsg, "", ""); err != nil {
		t.Fatal(err)
	}
	_ = base.Close()
	res := n.Scan()
	if res.Exported != 0 || res.REPPath != "" {
		t.Fatalf("hub mail re-exported: %+v", res)
	}
}

// rezipWithout copies a zip archive, leaving out one entry.
func rezipWithout(t *testing.T, archive []byte, drop string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, drop) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestToss_TruncatedPacketIsSetAside(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	pkt := hubPacket(t,
		qwk.PacketMessage{Conference: 2001, Number: 1, From: "A", To: "All", Subject: "whole", DateTime: time.Now(), Body: "ok"},
		qwk.PacketMessage{Conference: 2001, Number: 2, From: "B", To: "All", Subject: "cut", DateTime: time.Now(), Body: strings.Repeat("x", 400)},
	)
	// Rewrite MESSAGES.DAT with its last block missing.
	zr, err := zip.NewReader(bytes.NewReader(pkt), int64(len(pkt)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		if strings.EqualFold(f.Name, "MESSAGES.DAT") {
			data = data[:len(data)-qwk.BlockSize]
		}
		w, _ := zw.Create(f.Name)
		_, _ = w.Write(data)
	}
	_ = zw.Close()
	path := filepath.Join(e.paths.InboundPath, "VERT.QWK")
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res := n.Toss()
	if res.Imported != 1 || len(res.Errors) != 1 {
		t.Fatalf("toss: %+v", res)
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Error("truncated packet was not set aside")
	}
}
