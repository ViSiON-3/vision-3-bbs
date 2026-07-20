package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
)

// newRecordModel builds a Model seeded with one record of every editable type.
func newRecordModel() *Model {
	return &Model{
		configs: &allConfigs{
			Conferences: []conference.Conference{
				{ID: 1, Position: 1, Tag: "LOCAL", Name: "Local Conferences"},
			},
			MsgAreas: []message.MessageArea{
				{ID: 1, Position: 1, Tag: "GENERAL", Name: "General", AreaType: "local",
					ACSRead: "s10", ACSWrite: "s10", BasePath: "msgbases/general", ConferenceID: 1},
			},
			FileAreas: []file.FileArea{
				{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils", ConferenceID: 1},
			},
			Protocols: []transfer.ProtocolConfig{
				{Key: "Z", Name: "Zmodem", SendCmd: "sz", SendArgs: []string{"-b"}, RecvCmd: "rz"},
			},
			Archivers: archiver.Config{Archivers: []archiver.Archiver{
				{ID: "zip", Name: "ZIP Archive", Extension: ".zip", Magic: "504B0304"},
			}},
			LoginSeq: []config.LoginItem{
				{Command: "DISPLAY", Data: "WELCOME.ANS"},
			},
			Events: config.EventsConfig{Events: []config.EventConfig{
				{ID: "nightly", Name: "Nightly", Command: "true", Enabled: true},
			}},
		},
		recordEditIdx: 0,
	}
}

// TestRecordFieldGetSetIdempotence sweeps every record screen and verifies
// that for each editable non-lookup field, Set(Get()) succeeds and Get is
// stable afterwards (the editor must be able to re-save what it displays).
func TestRecordFieldGetSetIdempotence(t *testing.T) {
	screens := []string{
		"msgarea", "filearea", "conference", "protocol", "archiver", "login", "event",
	}
	for _, screen := range screens {
		t.Run(screen, func(t *testing.T) {
			m := newRecordModel()
			m.recordType = screen
			fields := m.buildRecordFields()
			if len(fields) == 0 {
				t.Fatalf("no fields for screen %s", screen)
			}
			for _, f := range fields {
				if f.Get == nil {
					t.Errorf("field %q has no Get", f.Label)
					continue
				}
				v1 := f.Get()
				if f.Set == nil || f.Type == ftLookup || f.Type == ftDisplay {
					continue
				}
				if err := f.Set(v1); err != nil {
					t.Errorf("field %q: Set(Get()) = %v", f.Label, err)
					continue
				}
				if v2 := f.Get(); v2 != v1 {
					t.Errorf("field %q: Get after Set = %q, want %q", f.Label, v2, v1)
				}
			}
		})
	}
}

// TestSysFieldGetSetIdempotence sweeps all system-config sub-screens.
func TestSysFieldGetSetIdempotence(t *testing.T) {
	m := newRecordModel()
	m.configs.Server = config.ServerConfig{
		BoardName: "Test BBS", SysOpName: "sysop", QWKID: "TEST",
	}
	for _, it := range append(systemConfigMenuItems(), securityMenuItems()...) {
		fields := it.Build(m)
		if len(fields) == 0 {
			t.Errorf("no fields for sys screen %q", it.Label)
			continue
		}
		for _, f := range fields {
			if f.Get == nil {
				t.Errorf("screen %q field %q has no Get", it.Label, f.Label)
				continue
			}
			v1 := f.Get()
			if f.Set == nil || f.Type == ftLookup || f.Type == ftDisplay {
				continue
			}
			if err := f.Set(v1); err != nil {
				t.Errorf("screen %q field %q: Set(Get()) = %v", it.Label, f.Label, err)
				continue
			}
			if v2 := f.Get(); v2 != v1 {
				t.Errorf("screen %q field %q: Get after Set = %q, want %q", it.Label, f.Label, v2, v1)
			}
		}
	}
}

func findField(t *testing.T, fields []fieldDef, label string) *fieldDef {
	t.Helper()
	for i := range fields {
		if fields[i].Label == label {
			return &fields[i]
		}
	}
	t.Fatalf("field %q not found", label)
	return nil
}

func TestFieldsMsgAreaConditionalNetworkFields(t *testing.T) {
	tests := []struct {
		areaType   string
		wantLabels []string
	}{
		{"local", nil},
		{"echomail", []string{"Network", "Echo Tag", "Origin Addr", "Sponsor"}},
		{"netmail", []string{"Network"}},
		{"v3net", []string{"Network", "Echo Tag"}},
	}
	base := len(newRecordModel().fieldsMsgArea()) // local variant
	for _, tt := range tests {
		t.Run(tt.areaType, func(t *testing.T) {
			m := newRecordModel()
			m.configs.MsgAreas[0].AreaType = tt.areaType
			fields := m.fieldsMsgArea()
			if got, want := len(fields), base+len(tt.wantLabels); got != want {
				t.Fatalf("field count = %d, want %d", got, want)
			}
			for _, label := range tt.wantLabels {
				findField(t, fields, label)
			}
		})
	}
}

func TestFieldsMsgAreaValidation(t *testing.T) {
	m := newRecordModel()
	m.recordType = "msgarea"
	fields := m.buildRecordFields()

	maxMsgs := findField(t, fields, "Max Messages")
	if err := maxMsgs.Set("not-a-number"); err == nil {
		t.Error("Max Messages should reject non-numeric input")
	}
	if err := maxMsgs.Set("500"); err != nil {
		t.Fatalf("Set(500): %v", err)
	}
	if m.configs.MsgAreas[0].MaxMessages != 500 {
		t.Errorf("MaxMessages = %d, want 500", m.configs.MsgAreas[0].MaxMessages)
	}

	autoJoin := findField(t, fields, "Auto Join")
	if err := autoJoin.Set("Y"); err != nil {
		t.Fatalf("Set(Y): %v", err)
	}
	if !m.configs.MsgAreas[0].AutoJoin {
		t.Error("AutoJoin should be true after Set(Y)")
	}

	// Conference lookup: Get shows display name, Set takes the ID value.
	conf := findField(t, fields, "Conference")
	if got := conf.Get(); got != "Local Conferences (ID: 1)" {
		t.Errorf("Conference Get = %q", got)
	}
	if err := conf.Set("0"); err != nil {
		t.Fatalf("Set(0): %v", err)
	}
	if got := conf.Get(); got != "Ungrouped (ID: 0)" {
		t.Errorf("Conference Get after Set(0) = %q", got)
	}
	if err := conf.Set("bogus"); err == nil {
		t.Error("Conference Set should reject non-numeric ID")
	}
	items := conf.LookupItems()
	if len(items) != 2 || items[0].Value != "0" || items[1].Value != "1" {
		t.Errorf("lookup items = %+v", items)
	}
}

func TestFieldsEventNameSetSanitizesID(t *testing.T) {
	m := newRecordModel()
	m.recordType = "event"
	fields := m.buildRecordFields()

	name := findField(t, fields, "Name")
	if err := name.Set("New Cool Event!"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	e := m.configs.Events.Events[0]
	if e.Name != "New Cool Event!" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.ID != "new_cool_event" {
		t.Errorf("ID = %q, want new_cool_event", e.ID)
	}
	if err := name.Set("!!!"); err == nil {
		t.Error("Set with no alphanumerics should error")
	}
}

func TestFieldsProtocolArgsRoundTrip(t *testing.T) {
	m := newRecordModel()
	m.recordType = "protocol"
	fields := m.buildRecordFields()

	sendArgs := findField(t, fields, "Send Args")
	if got := sendArgs.Get(); got != `["-b"]` {
		t.Errorf("Send Args Get = %q, want [\"-b\"]", got)
	}
	if err := sendArgs.Set(`["-b","--tcp"]`); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := m.configs.Protocols[0].SendArgs; len(got) != 2 || got[1] != "--tcp" {
		t.Errorf("SendArgs = %#v", got)
	}
	if err := sendArgs.Set(`["broken`); err == nil {
		t.Error("Set with malformed JSON should error")
	}
}

func TestBuildRecordFields_UnknownAndOutOfRange(t *testing.T) {
	m := newRecordModel()
	m.recordType = "nonsense"
	if fields := m.buildRecordFields(); fields != nil {
		t.Errorf("unknown record type should yield nil fields, got %d", len(fields))
	}
	m.recordType = "msgarea"
	m.recordEditIdx = 42
	if fields := m.buildRecordFields(); fields != nil {
		t.Error("out-of-range index should yield nil fields")
	}
}
