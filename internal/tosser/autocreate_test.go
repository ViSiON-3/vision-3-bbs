package tosser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// tossOne writes a one-message packet for area to the inbound dir and tosses it.
func tossOne(t *testing.T, tsr *Tosser, env *testEnv, name, area, msgID string) TossResult {
	t.Helper()
	return tossOnePw(t, tsr, env, name, area, msgID, "")
}

// tossOnePw is tossOne with the packet header carrying password.
func tossOnePw(t *testing.T, tsr *Tosser, env *testEnv, name, area, msgID, password string) TossResult {
	t.Helper()
	pkt := makePktSimple(t, area, "Sender", "All", "Subj", "body\r", msgID)
	if password != "" {
		hdr, msgs, err := ftn.ReadPacket(bytes.NewReader(pkt))
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
		hdr.Password = [8]byte{}
		copy(hdr.Password[:], password)
		var buf bytes.Buffer
		if err := ftn.WritePacket(&buf, hdr, msgs); err != nil {
			t.Fatalf("WritePacket: %v", err)
		}
		pkt = buf.Bytes()
	}
	if err := os.WriteFile(filepath.Join(env.inboundDir, name), pkt, 0644); err != nil {
		t.Fatal(err)
	}
	return tsr.ProcessInbound()
}

// setupAutoCreateEnv is setupTestEnv with auto-create on and packets written
// to a secure inbound, so packets from the hub may create areas.
func setupAutoCreateEnv(t *testing.T) *testEnv {
	t.Helper()
	env := setupTestEnv(t)
	env.netCfg.AutoCreate.Enabled = true
	useSecureInbound(t, env)
	return env
}

// useSecureInbound gives env a secure inbound apart from the unsecured one
// and has the test write its packets there.
func useSecureInbound(t *testing.T, env *testEnv) {
	t.Helper()
	secure := filepath.Join(env.dataDir, "ftn", "secure_in")
	if err := os.MkdirAll(secure, 0755); err != nil {
		t.Fatal(err)
	}
	env.globalCfg.SecureInboundPath = secure
	env.inboundDir = secure
}

// With auto_create on, mail in an echo the BBS doesn't carry creates an area
// for it (named the way the wizard names areas) and lands there, and the new
// area is saved to message_areas.json. A second message for the echo uses the
// same area.
func TestTossAutoCreatesUnknownArea(t *testing.T) {
	env := setupAutoCreateEnv(t)
	env.netCfg.AutoCreate.ConferenceID = 7
	maxMsgs := 200
	env.netCfg.AutoCreate.MaxMessages = &maxMsgs
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if r := tossOne(t, tsr, env, "new00001.pkt", "NEW_ECHO", "21:4/100 AC000001"); r.MessagesImported != 1 || len(r.Errors) != 0 {
		t.Fatalf("imported %d, errors %v; want 1 and none", r.MessagesImported, r.Errors)
	}

	area, ok := env.msgMgr.GetAreaByTag("testnet_new_echo")
	if !ok {
		t.Fatal("area testnet_new_echo was not created")
	}
	if area.EchoTag != "NEW_ECHO" || area.Network != "testnet" || area.AreaType != "echomail" {
		t.Errorf("echo tag %q, network %q, type %q; want NEW_ECHO, testnet, echomail", area.EchoTag, area.Network, area.AreaType)
	}
	if area.ACSRead != "s10" || area.ACSWrite != "s20" {
		t.Errorf("ACS %q/%q, want the s10/s20 defaults", area.ACSRead, area.ACSWrite)
	}
	if area.ConferenceID != 7 || area.MaxMessages != 200 {
		t.Errorf("conference %d, max messages %d; want 7 and 200", area.ConferenceID, area.MaxMessages)
	}
	if area.MaxAge != 365 || !area.AutoJoin || area.RealNameOnly {
		t.Errorf("max age %d, auto join %v, real names %v; want the Mystic defaults 365, true, false",
			area.MaxAge, area.AutoJoin, area.RealNameOnly)
	}
	if area.OriginAddr != env.netCfg.OwnAddress {
		t.Errorf("origin address %q, want %q", area.OriginAddr, env.netCfg.OwnAddress)
	}

	if r := tossOne(t, tsr, env, "new00002.pkt", "NEW_ECHO", "21:4/100 AC000002"); r.MessagesImported != 1 || len(r.Errors) != 0 {
		t.Fatalf("second toss: imported %d, errors %v", r.MessagesImported, r.Errors)
	}
	base, err := env.msgMgr.GetBase(area.ID)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer base.Close()
	if n, _ := base.GetMessageCount(); n != 2 {
		t.Errorf("new area holds %d messages, want 2", n)
	}

	areas, _, err := message.ReadAreasFile(filepath.Join(env.configDir, "message_areas.json"))
	if err != nil {
		t.Fatalf("ReadAreasFile: %v", err)
	}
	var saved int
	for _, a := range areas {
		if a.Tag == "testnet_new_echo" {
			saved++
		}
	}
	if saved != 1 {
		t.Errorf("message_areas.json holds %d testnet_new_echo areas, want 1", saved)
	}
}

// With no conference configured, a new area joins the conference of the
// network's other echomail areas, whichever spelling of the type they use.
func TestAutoCreateInfersConference(t *testing.T) {
	for _, areaType := range []string{"echomail", "echo"} {
		t.Run(areaType, func(t *testing.T) {
			env := setupAutoCreateEnv(t)
			existing, _ := env.msgMgr.GetAreaByTag("FSX_TEST")
			upd := *existing
			upd.ConferenceID = 3
			upd.AreaType = areaType
			if err := env.msgMgr.UpdateAreaByID(upd.ID, upd); err != nil {
				t.Fatalf("UpdateAreaByID: %v", err)
			}
			tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			tossOne(t, tsr, env, "conf0001.pkt", "OTHER", "21:4/100 AC000003")
			area, ok := env.msgMgr.GetAreaByTag("testnet_other")
			if !ok {
				t.Fatal("area was not created")
			}
			if area.ConferenceID != 3 {
				t.Errorf("conference %d, want 3 from FSX_TEST", area.ConferenceID)
			}
		})
	}
}

// A packet must prove it is from the hub before its mail can create areas:
// by arriving in the secure inbound, or by carrying the link's packet
// password. The origin address alone is not enough, nor is a network whose
// link addresses don't parse (which tosses every packet, so as not to stall).
func TestAutoCreateRequiresTrustedPacket(t *testing.T) {
	for _, tc := range []struct {
		name       string
		secure     bool
		linkPw     string
		pktPw      string
		badLink    bool
		sameDir    bool
		wantCreate bool
	}{
		{name: "secure inbound", secure: true, wantCreate: true},
		{name: "unsecured, no password", secure: false},
		{name: "unsecured, password matches", linkPw: "SeCrEt", pktPw: "secret", wantCreate: true},
		{name: "unsecured, wrong password", linkPw: "secret", pktPw: "guess"},
		{name: "secure, wrong password", secure: true, linkPw: "secret", pktPw: "guess"},
		{name: "secure, no valid link", secure: true, badLink: true},
		{name: "secure inbound is the unsecured one", sameDir: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTestEnv(t)
			env.netCfg.AutoCreate.Enabled = true
			if tc.secure {
				useSecureInbound(t, env)
			}
			if tc.sameDir {
				env.globalCfg.SecureInboundPath = env.globalCfg.InboundPath
			}
			env.netCfg.Links = []config.FTNLinkConfig{{Address: "21:4/158", PacketPassword: tc.linkPw}}
			if tc.badLink {
				env.netCfg.Links[0].Address = "not-an-address"
			}
			tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			tossOnePw(t, tsr, env, "trust001.pkt", "NEW_ECHO", "21:4/100 AC000005", tc.pktPw)
			_, created := env.msgMgr.GetAreaByTag("testnet_new_echo")
			if created != tc.wantCreate {
				t.Errorf("area created = %v, want %v", created, tc.wantCreate)
			}
		})
	}
}

// Auto-create off, or a tag that isn't a valid echo tag, still sends the
// message to the bad area and creates nothing.
func TestAutoCreateFallsBackToBad(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		area    string
	}{
		{"disabled", false, "NEW_ECHO"},
		{"invalid tag", true, "../ETC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupAutoCreateEnv(t)
			env.netCfg.AutoCreate.Enabled = tc.enabled
			if _, err := env.msgMgr.AddArea(message.MessageArea{Tag: "BAD", Name: "Bad", AreaType: "local"}); err != nil {
				t.Fatalf("AddArea: %v", err)
			}
			env.globalCfg.BadAreaTag = "BAD"
			tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			before := len(env.msgMgr.ListAreas())
			if r := tossOne(t, tsr, env, "bad00001.pkt", tc.area, "21:4/100 AC000004"); r.MessagesImported != 1 {
				t.Fatalf("imported %d, errors %v; want 1 (to BAD)", r.MessagesImported, r.Errors)
			}
			if after := len(env.msgMgr.ListAreas()); after != before {
				t.Errorf("%d areas after toss, want %d", after, before)
			}
			for _, a := range env.msgMgr.ListAreas() {
				if strings.HasPrefix(a.Tag, "testnet_") {
					t.Errorf("unexpected area %q created", a.Tag)
				}
			}
		})
	}
}
