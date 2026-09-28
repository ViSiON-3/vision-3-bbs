package menu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// netmailFixture is a BBS on the shipped configs plus a NETMAIL area for the
// FTN network "testnet", fed by the real tosser with the BBS's accounts as its
// recipient resolver, as v3mail toss runs it. The accounts are Hermit (user
// #1, the sysop, real name "Sam Sysop"), Bob ("Bob Builder") and Carol
// ("Carol Singer"); only Hermit is at the sysop level.
type netmailFixture struct {
	*privacyFixture
	tosser  *tosser.Tosser
	inbound string
	packets int
}

func newNetmailFixture(t *testing.T) *netmailFixture {
	t.Helper()
	root := t.TempDir()
	cfgDir, dataDir := filepath.Join(root, "configs"), filepath.Join(root, "data")
	inbound := filepath.Join(dataDir, "ftn", "in")
	for _, d := range []string{cfgDir, dataDir, inbound,
		filepath.Join(dataDir, "ftn", "temp_in"), filepath.Join(dataDir, "ftn", "temp_out"),
		filepath.Join(dataDir, "ftn", "out")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeShippedConfigs(t, cfgDir)

	areasPath := filepath.Join(cfgDir, "message_areas.json")
	raw, err := os.ReadFile(areasPath)
	if err != nil {
		t.Fatal(err)
	}
	var areas []map[string]any
	if err := json.Unmarshal(raw, &areas); err != nil {
		t.Fatal(err)
	}
	areas = append(areas, map[string]any{
		"id": 90, "position": 90, "tag": "NETMAIL", "name": "Netmail",
		"acs_read": "s10", "acs_write": "s20", "conference_id": 1,
		"base_path": "msgbases/netmail", "area_type": "netmail",
		"echo_tag": "NETMAIL", "network": "testnet",
	})
	if raw, err = json.Marshal(areas); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(areasPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	strs, err := config.LoadStrings(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	menuSet, err := filepath.Abs(filepath.Join("..", "..", "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(dataDir, cfgDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	area, ok := mm.GetAreaByTag("NETMAIL")
	if !ok {
		t.Fatal("NETMAIL area missing")
	}

	users := []*user.User{
		{ID: 1, Handle: "Hermit", RealName: "Sam Sysop", AccessLevel: 255, Validated: true},
		{ID: 2, Handle: "Bob", RealName: "Bob Builder", AccessLevel: 30, Validated: true},
		{ID: 3, Handle: "Carol", RealName: "Carol Singer", AccessLevel: 30, Validated: true},
	}
	for _, u := range users {
		u.CurrentMsgConferenceID = area.ConferenceID
		u.CurrentMessageAreaID = area.ID
		u.CurrentMessageAreaTag = area.Tag
		u.TaggedMessageAreaTags = []string{area.Tag}
		u.MsgHdr = 2
	}
	seed, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "users.json"), seed, 0o644); err != nil {
		t.Fatal(err)
	}
	um, err := user.NewUserManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	dupes, err := tosser.NewDupeDB(filepath.Join(dataDir, "ftn", "dupes.json"), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tss, err := tosser.New("testnet", config.FTNNetworkConfig{
		InternalTosserEnabled: true,
		OwnAddress:            "21:4/158.1",
		Links:                 []config.FTNLinkConfig{{Address: "21:4/158", Name: "Test Hub"}},
	}, config.FTNConfig{
		InboundPath:       inbound,
		TempPath:          filepath.Join(dataDir, "ftn", "temp_in"),
		OutboundPath:      filepath.Join(dataDir, "ftn", "temp_out"),
		BinkdOutboundPath: filepath.Join(dataDir, "ftn", "out"),
	}, dupes, mm)
	if err != nil {
		t.Fatal(err)
	}
	tss.SetRecipientResolver(um)

	e := &MenuExecutor{MenuSetPath: menuSet, RootConfigPath: cfgDir, MessageMgr: mm}
	e.SetStrings(strs)
	e.SetServerConfig(config.ServerConfig{SysOpLevel: 255, CoSysOpLevel: 250, DataDir: dataDir})
	pf := &privacyFixture{e: e, um: um, privID: area.ID}
	pf.sysop, _ = um.GetUserByID(1)
	pf.bob, _ = um.GetUserByID(2)
	pf.carol, _ = um.GetUserByID(3)
	return &netmailFixture{privacyFixture: pf, tosser: tss, inbound: inbound}
}

// toss delivers one netmail from a remote node to this system (21:4/158.1),
// addressed to to, through the tosser.
func (f *netmailFixture) toss(t *testing.T, to, subject, body string) {
	t.Helper()
	hdr := ftn.NewPacketHeader(21, 4, 158, 0, 21, 4, 158, 1, "")
	f.packets++
	pm := &ftn.PackedMessage{
		MsgType: 2, OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158,
		Attr: 0x0001, DateTime: "21 Feb 26  12:00:00",
		To: to, From: "Remote User", Subject: subject,
		Body: ftn.FormatPackedMessageBody(&ftn.ParsedBody{
			Text:    body + "\r",
			Kludges: []string{fmt.Sprintf("MSGID: 21:4/100 %08x", f.packets), "TOPT 1"},
		}),
	}
	var buf bytes.Buffer
	if err := ftn.WritePacket(&buf, hdr, []*ftn.PackedMessage{pm}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.inbound, fmt.Sprintf("%08x.pkt", f.packets)), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := f.tosser.ProcessInbound(); res.MessagesImported != 1 {
		t.Fatalf("tossed %d messages, want 1 (errors: %v)", res.MessagesImported, res.Errors)
	}
}

// TestTossedNetmailReachesItsRecipient pins the #467 regression for FTN
// netmail: mail addressed to a local user's real name, or to "Sysop", is
// stored addressed to that user's handle, so the recipient reads it and
// nobody else does (not even the sysop, since it was delivered).
func TestTossedNetmailReachesItsRecipient(t *testing.T) {
	f := newNetmailFixture(t)
	f.toss(t, "Bob Builder", "For Bob", "NET-BOB-BODY")
	f.toss(t, "Sysop", "For the sysop", "NET-SYSOP-BODY")

	base, err := f.e.MessageMgr.GetBase(f.privID)
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range map[int]string{1: "Bob", 2: "Hermit"} {
		m, err := base.ReadMessage(n)
		if err != nil {
			t.Fatal(err)
		}
		if m.To != want {
			t.Errorf("netmail #%d stored To %q, want the handle %q", n, m.To, want)
		}
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		u          *user.User
		see, avoid []string
	}{
		{"recipient", f.bob, []string{"NET-BOB-BODY"}, []string{"NET-SYSOP-BODY"}},
		{"sysop", f.sysop, []string{"NET-SYSOP-BODY"}, []string{"NET-BOB-BODY"}},
		{"bystander", f.carol, nil, []string{"NET-BOB-BODY", "NET-SYSOP-BODY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := f.run(t, runReadMsgs, tc.u, "NNNQ")
			for _, s := range tc.see {
				if !strings.Contains(out, s) {
					t.Errorf("%s did not see %q:\n%s", tc.u.Handle, s, out)
				}
			}
			for _, s := range tc.avoid {
				if strings.Contains(out, s) {
					t.Errorf("%s saw %q, mail for someone else", tc.u.Handle, s)
				}
			}
		})
	}
}

// TestUndeliverableNetmailIsReadBySysop pins that netmail whose To resolves to
// no account (so it stays addressed to no one's handle) can be read by a
// sysop, in the reader and the list, and by no other user.
func TestUndeliverableNetmailIsReadBySysop(t *testing.T) {
	f := newNetmailFixture(t)
	f.toss(t, "Nobody Known", "Lost letter", "NET-LOST-BODY")

	if out := f.run(t, runReadMsgs, f.sysop, "NNNQ"); !strings.Contains(out, "NET-LOST-BODY") {
		t.Errorf("sysop could not read undeliverable netmail:\n%s", out)
	}
	if out := f.run(t, runListMsgs, f.sysop, ""); !strings.Contains(out, "Lost letter") {
		t.Errorf("undeliverable netmail missing from the sysop's list:\n%s", out)
	}
	for _, u := range []*user.User{f.bob, f.carol} {
		for name, fn := range map[string]RunnableFunc{"READMSGS": runReadMsgs, "LISTMSGS": runListMsgs} {
			out := f.run(t, fn, u, "NNNQ")
			if strings.Contains(out, "NET-LOST-BODY") || strings.Contains(out, "Lost letter") {
				t.Errorf("%s showed undeliverable netmail to %s", name, u.Handle)
			}
		}
	}
}

// TestWithPrivacyUndeliverableNeedsADirectory pins that the sysop allowance
// never widens access without an account directory that knows the reader:
// with none, or with one that does not know the sysop's handle, private mail
// between others stays hidden.
func TestWithPrivacyUndeliverableNeedsADirectory(t *testing.T) {
	sysop := &user.User{Handle: "Hermit", AccessLevel: 255}
	lost := &message.DisplayMessage{IsPrivate: true, From: "Remote", To: "Nobody Known"}
	known := user.NewUserMgrForTest(&user.User{ID: 1, Handle: "Hermit"})
	var typedNil *user.UserMgr

	if !withPrivacy(privateMailReader{user: sysop, sysOpLevel: 255, handles: known}, nil)(lost) {
		t.Error("sysop with a directory could not read undeliverable mail")
	}
	for name, r := range map[string]privateMailReader{
		"no directory":       {user: sysop, sysOpLevel: 255},
		"typed-nil manager":  {user: sysop, sysOpLevel: 255, handles: typedNil},
		"directory lacks me": {user: sysop, sysOpLevel: 255, handles: user.NewUserMgrForTest()},
		"below sysop level":  {user: &user.User{Handle: "Hermit", AccessLevel: 254}, sysOpLevel: 255, handles: known},
	} {
		if withPrivacy(r, nil)(lost) {
			t.Errorf("%s: undeliverable mail shown", name)
		}
	}
}
