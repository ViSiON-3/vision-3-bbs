package configeditor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// newAreaSyncModel returns a Model on the fidonet record's field list, with
// two of its echomail areas, its netmail area and another network's echo.
func newAreaSyncModel(events config.EventsConfig) Model {
	m := Model{
		configs: &allConfigs{
			FTN: config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{"fidonet": {OwnAddress: "1:2/3"}}},
			MsgAreas: []message.MessageArea{
				{Tag: "FIDO_A", Network: "fidonet", AreaType: "echomail", MaxMessages: 1000},
				{Tag: "FIDO_B", Network: "FidoNet", AreaType: "echomail", MaxMessages: 1000},
				{Tag: "FIDO_NET", Network: "fidonet", AreaType: "netmail"},
				{Tag: "FSX_GEN", Network: "fsxnet", AreaType: "echomail"},
			},
			Events: events,
		},
		textInput:     textinput.New(),
		mode:          modeRecordEdit,
		recordType:    "ftn",
		recordEditIdx: 0,
	}
	m.recordFields = m.buildRecordFields()
	return m
}

// focusField moves the field cursor to label.
func focusField(t *testing.T, m *Model, label string) {
	t.Helper()
	for i, f := range m.recordFields {
		if strings.TrimSpace(f.Label) == label {
			m.editField = i
			return
		}
	}
	t.Fatalf("field %q not found", label)
}

func realNameByTag(m Model) map[string]bool {
	got := map[string]bool{}
	for _, a := range m.configs.MsgAreas {
		got[a.Tag] = a.RealNameOnly
	}
	return got
}

// TestAutoCreateRealNamesOffersExistingAreas pins #690: turning on Real
// Names under Auto-Create offers to apply it to the network's existing
// echomail areas, and Yes does so without touching netmail or other
// networks.
func TestAutoCreateRealNamesOffersExistingAreas(t *testing.T) {
	m := newAreaSyncModel(config.EventsConfig{})
	focusField(t, &m, "Real Names")
	m = press(t, m, "space")

	if m.mode != modeAreaSyncConfirm {
		t.Fatalf("mode = %v, want the area sync prompt", m.mode)
	}
	if q := m.areaSync.question(); !strings.Contains(q, "2 existing fidonet") {
		t.Errorf("question = %q, want it to name 2 fidonet areas", q)
	}

	m = press(t, m, "y")
	if m.mode != modeRecordEdit {
		t.Errorf("mode after Yes = %v, want field navigation", m.mode)
	}
	want := map[string]bool{"FIDO_A": true, "FIDO_B": true, "FIDO_NET": false, "FSX_GEN": false}
	for tag, w := range want {
		if got := realNameByTag(m)[tag]; got != w {
			t.Errorf("%s RealNameOnly = %v, want %v", tag, got, w)
		}
	}
	if !strings.Contains(m.message, "applied to 2") {
		t.Errorf("message = %q, want the applied count", m.message)
	}
}

// TestAutoCreateAreaSyncNoLeavesAreas checks that answering No (or ESC)
// keeps the Auto-Create change but leaves existing areas alone.
func TestAutoCreateAreaSyncNoLeavesAreas(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		m := newAreaSyncModel(config.EventsConfig{})
		focusField(t, &m, "Real Names")
		m = press(t, m, "space")
		m = press(t, m, key)
		if m.mode != modeRecordEdit {
			t.Errorf("%s: mode = %v, want field navigation", key, m.mode)
		}
		if !m.configs.FTN.Networks["fidonet"].AutoCreate.RealNameOnly {
			t.Errorf("%s: Auto-Create Real Names lost", key)
		}
		for tag, on := range realNameByTag(m) {
			if on {
				t.Errorf("%s: %s changed to real names", key, tag)
			}
		}
	}
}

// TestAutoCreateMaxMessagesWarnsWithoutPurgeEvent pins #691 in the editor:
// a purge limit set while no enabled event runs v3mail purge says so.
func TestAutoCreateMaxMessagesWarnsWithoutPurgeEvent(t *testing.T) {
	purge := config.EventsConfig{Events: []config.EventConfig{
		{Command: "{BBS_ROOT}/v3mail", Args: []string{"purge", "--all"}, Schedule: "15 2 * * *", Enabled: true},
	}}
	for _, tt := range []struct {
		desc   string
		events config.EventsConfig
		warn   bool
	}{
		{"no purge event", config.EventsConfig{}, true},
		{"purge event enabled", purge, false},
	} {
		m := newAreaSyncModel(tt.events)
		focusField(t, &m, "Max Messages")
		m = press(t, m, "enter")
		m = replaceText(t, m, "500")
		m = press(t, m, "enter")
		if m.mode != modeAreaSyncConfirm {
			t.Fatalf("%s: mode = %v, want the area sync prompt", tt.desc, m.mode)
		}
		m = press(t, m, "y")
		for _, a := range m.configs.MsgAreas[:2] {
			if a.MaxMessages != 500 {
				t.Errorf("%s: %s MaxMessages = %d, want 500", tt.desc, a.Tag, a.MaxMessages)
			}
		}
		if got := strings.Contains(m.message, "v3mail purge"); got != tt.warn {
			t.Errorf("%s: message = %q, want purge warning %v", tt.desc, m.message, tt.warn)
		}
	}
}

// TestAutoCreateNoPromptWhenAreasMatch checks no prompt opens when every
// existing area already has the value.
func TestAutoCreateNoPromptWhenAreasMatch(t *testing.T) {
	m := newAreaSyncModel(config.EventsConfig{})
	focusField(t, &m, "Auto Join")
	m = press(t, m, "space") // default Y -> N; areas are already N
	if m.mode != modeRecordEdit || m.areaSync != nil {
		t.Errorf("mode = %v, areaSync = %v; want no prompt", m.mode, m.areaSync)
	}
}
