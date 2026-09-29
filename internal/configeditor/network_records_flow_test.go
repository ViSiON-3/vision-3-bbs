package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// refuseRecField opens the record field label, enters val, and fails unless
// the value is refused; it then abandons the edit.
func refuseRecField(t *testing.T, m Model, label, val string) Model {
	t.Helper()
	m = press(t, gotoField(t, m, label), "enter")
	m = press(t, replaceText(t, m, val), "enter")
	if m.mode != modeRecordField || !strings.HasPrefix(m.message, "Invalid:") {
		t.Fatalf("%s=%q accepted (mode %v, msg %q)", label, val, m.mode, m.message)
	}
	return press(t, m, "esc")
}

// TestFTNGlobalFields_Persist pins the Echomail global settings screen: the
// binkd outbound path is validated and every path edit is saved.
func TestFTNGlobalFields_Persist(t *testing.T) {
	m, dir := newDiskModel(t)
	seedFTN(&m)
	m.configs.MsgAreas = []message.MessageArea{
		{ID: 1, Position: 1, Tag: "BAD", Name: "Bad Mail", AreaType: "local"},
		{ID: 2, Position: 2, Tag: "DUPE", Name: "Dupes", AreaType: "local"},
	}
	m = press(t, openRecordList(t, m, "ftn"), "G")
	wantScreen(t, m, "Dupe DB Path")
	m = refuseRecField(t, m, "Binkd Outbound", "data/ftn/out.dir")
	vals := map[string]string{
		"Dupe DB Path":   "d/dupes.json",
		"Inbound Path":   "d/in",
		"Secure Inbound": "d/sin",
		"Outbound Path":  "d/outpkt",
		"Binkd Outbound": "d/out",
		"Temp Path":      "d/tmp",
	}
	for _, label := range []string{"Dupe DB Path", "Inbound Path", "Secure Inbound", "Outbound Path", "Binkd Outbound", "Temp Path"} {
		m = setRecField(t, m, label, vals[label])
	}
	// Bad and dupe areas are picked from the configured message areas.
	m = pickRecField(t, m, "Bad Area", "BAD")
	m = pickRecField(t, m, "Dupe Area", "DUPE")
	saveAndQuit(t, m)
	f := reloadConfigs(t, dir).FTN
	got := []string{f.DupeDBPath, f.InboundPath, f.SecureInboundPath, f.OutboundPath, f.BinkdOutboundPath, f.TempPath, f.BadAreaTag, f.DupeAreaTag}
	if strings.Join(got, ",") != "d/dupes.json,d/in,d/sin,d/outpkt,d/out,d/tmp,BAD,DUPE" {
		t.Errorf("saved = %v", got)
	}
}

// TestFTNLinkFields_EditAndMove pins link editing: address, packet password
// and port are validated, the flavour comes from a picker, and changing the
// Network moves the link to the other network.
func TestFTNLinkFields_EditAndMove(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"fidonet": {OwnAddress: "1:2/3", Links: []config.FTNLinkConfig{{Address: "1:2/1", Name: "Hub"}}},
		"fsxnet":  {OwnAddress: "21:4/1"},
	}
	m = openRecordList(t, m, "ftnlink")
	wantScreen(t, m, "fidonet", "1:2/1")
	m = press(t, m, "enter")
	m = refuseRecField(t, m, "Address", "")
	m = refuseRecField(t, m, "Packet Password", "123456789")
	m = refuseRecField(t, m, "Port", "-5")
	m = setRecField(t, m, "Address", "21:1/100")
	m = setRecField(t, m, "Packet Password", "pkt")
	m = setRecField(t, m, "Session Password", "sess")
	m = setRecField(t, m, "Areafix Password", "af")
	m = setRecField(t, m, "Name", "FSX Hub")
	m = setRecField(t, m, "Hostname", "agency.bbs.nz")
	m = setRecField(t, m, "Port", "24556")
	m = pickRecField(t, m, "Flavour", "Hold")
	m = pickRecField(t, m, "Network", "fsxnet")
	if m.recordFields[0].Get() != "fsxnet" {
		t.Fatalf("after move, editing network %q", m.recordFields[0].Get())
	}
	saveAndQuit(t, m)
	nets := reloadConfigs(t, dir).FTN.Networks
	if len(nets["fidonet"].Links) != 0 || len(nets["fsxnet"].Links) != 1 {
		t.Fatalf("links: fidonet=%d fsxnet=%d", len(nets["fidonet"].Links), len(nets["fsxnet"].Links))
	}
	l := nets["fsxnet"].Links[0]
	if l.Address != "21:1/100" || l.PacketPassword != "pkt" || l.SessionPassword != "sess" || l.AreafixPassword != "af" ||
		l.Name != "FSX Hub" || l.Hostname != "agency.bbs.nz" || l.Port != 24556 || l.Flavour != "Hold" {
		t.Errorf("link = %+v", l)
	}
}

// TestFTNLinkInsert_NeedsNetwork pins that inserting a link with no echomail
// network explains what to do instead of adding one.
func TestFTNLinkInsert_NeedsNetwork(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, openRecordList(t, m, "ftnlink"), "i")
	if m.recordCount() != 0 || m.message != "Create an Echomail Network before adding a link" {
		t.Errorf("count=%d msg=%q", m.recordCount(), m.message)
	}
}

// TestQWKNetworkFields_Persist pins the QWK network record: required hub
// fields and unique hub IDs are enforced, and edits persist.
func TestQWKNetworkFields_Persist(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{
		"dovenet": {Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "pw", Enabled: true},
		"fsxqwk":  {Name: "fsx", HubID: "FSX", Host: "fsx.example", Password: "pw"},
	}
	m = press(t, openRecordList(t, m, "qwknet"), "enter")
	if m.recordFields[0].Get() != "dovenet" {
		t.Fatalf("editing %q", m.recordFields[0].Get())
	}
	m = refuseRecField(t, m, "Network Key", "")
	m = refuseRecField(t, m, "Network Key", "fsxqwk")
	m = refuseRecField(t, m, "Hub QWK-ID", "")
	m = refuseRecField(t, m, "Hub QWK-ID", "FSX")
	m = refuseRecField(t, m, "Hub Host", "")
	m = refuseRecField(t, m, "Password", "")
	m = refuseRecField(t, m, "Hub Port", "0")
	m = setRecField(t, m, "Name", "Dove")
	m = setRecField(t, m, "Hub Host", "vert.example")
	m = setRecField(t, m, "Hub Port", "2121")
	m = setRecField(t, m, "Login Name", "node1")
	m = setRecField(t, m, "Password", "newpw")
	m = setRecField(t, m, "Tagline", "Hello")
	m = setRecField(t, m, "Timeout", "120")
	m = press(t, gotoField(t, m, "Enabled"), "space")
	m = press(t, gotoField(t, m, "HEADERS.DAT"), "space")
	wantScreen(t, m, "vert.example")
	saveAndQuit(t, m)
	nc := reloadConfigs(t, dir).QWKNet.Networks["dovenet"]
	if nc.Name != "Dove" || nc.Host != "vert.example" || nc.Port != 2121 || nc.Username != "node1" ||
		nc.Password != "newpw" || nc.Tagline != "Hello" || nc.TimeoutSeconds != 120 || nc.Enabled {
		t.Errorf("network = %+v", nc)
	}
}

// TestQWKGlobalBadAreaTag pins that the QWK global Bad Area Tag must name an
// existing message area, and is stored as that area's tag.
func TestQWKGlobalBadAreaTag(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.MsgAreas = []message.MessageArea{{ID: 1, Position: 1, Tag: "BADMAIL", Name: "Bad", AreaType: "local"}}
	m.configs.QWKNet.Networks = map[string]config.QWKNetworkConfig{"dovenet": {HubID: "VERT", Host: "h", Password: "p"}}
	m = press(t, openRecordList(t, m, "qwknet"), "G")
	m = refuseRecField(t, m, "Bad Area Tag", "NOPE")
	m = setRecField(t, m, "Bad Area Tag", "badmail")
	m = setRecField(t, m, "Outbound Path", "q/out")
	m = setRecField(t, m, "Temp Path", "q/tmp")
	m = setRecField(t, m, "Dupe DB Path", "q/dupes.json")
	saveAndQuit(t, m)
	q := reloadConfigs(t, dir).QWKNet
	if q.BadAreaTag != "BADMAIL" || q.OutboundPath != "q/out" || q.TempPath != "q/tmp" || q.DupeDBPath != "q/dupes.json" {
		t.Errorf("qwknet globals = %+v", q)
	}
}
