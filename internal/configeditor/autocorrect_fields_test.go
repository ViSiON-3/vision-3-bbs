package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// The message area's Autocorrect picker has three settings: Default (the key
// left out, following the board), On and Off.
func TestMsgAreaAutocorrectField(t *testing.T) {
	m := configuredModel()
	m.recordType = "msgarea"
	m.recordEditIdx = 0
	a := &m.configs.MsgAreas[0]
	a.Autocorrect = nil

	f := fieldByLabel(t, m.fieldsMsgArea(), "Autocorrect")
	if f.Type != ftLookup {
		t.Fatalf("type = %v, want a lookup", f.Type)
	}
	if got := f.Get(); got != autocorrectAreaDefault {
		t.Errorf("unset reads %q, want %q", got, autocorrectAreaDefault)
	}
	items := f.LookupItems()
	if len(items) != 3 || items[0].Value != "" {
		t.Fatalf("items = %+v", items)
	}
	for _, tc := range []struct {
		value, display string
		want           *bool
	}{
		{"on", autocorrectAreaOn, acBool(true)},
		{"off", autocorrectAreaOff, acBool(false)},
		{"", autocorrectAreaDefault, nil},
	} {
		if err := f.Set(tc.value); err != nil {
			t.Fatal(err)
		}
		if (a.Autocorrect == nil) != (tc.want == nil) || (a.Autocorrect != nil && *a.Autocorrect != *tc.want) {
			t.Errorf("Set(%q) stored %v, want %v", tc.value, a.Autocorrect, tc.want)
		}
		if got := f.Get(); got != tc.display {
			t.Errorf("after Set(%q), Get = %q, want %q", tc.value, got, tc.display)
		}
	}
}

// Network fields sit below Autocorrect, not on its row.
func TestMsgAreaAutocorrectRowIsFree(t *testing.T) {
	m := configuredModel()
	m.recordType = "msgarea"
	m.recordEditIdx = 0
	for _, typ := range []string{"local", "echomail", "netmail", "v3net", "qwknet"} {
		m.configs.MsgAreas[0].AreaType = typ
		rows := map[int]string{}
		for _, f := range m.fieldsMsgArea() {
			if prev, dup := rows[f.Row]; dup {
				t.Errorf("%s: %q and %q share row %d", typ, prev, f.Label, f.Row)
			}
			rows[f.Row] = f.Label
		}
	}
}

// The board default is a Y/N on Default Settings, off unless set.
func TestBoardAutocorrectField(t *testing.T) {
	var cfg config.ServerConfig
	f := fieldByLabel(t, sysFieldsDefaults(&cfg), "Autocorrect")
	if got := f.Get(); got != "N" {
		t.Fatalf("default = %q, want N", got)
	}
	if err := f.Set("Y"); err != nil || !cfg.EditorAutocorrect || f.Get() != "Y" {
		t.Fatalf("set Y: on=%v err=%v", cfg.EditorAutocorrect, err)
	}
}

// The highlight colour is edited with the other prompt colours.
func TestThemeAutocorrectColorField(t *testing.T) {
	th := config.ThemeConfig{AutocorrectHighlightColor: 15}
	f := fieldByLabel(t, themeFieldsPrompts(&th), "Autocorrect Fix")
	if got := f.Get(); got != "15" {
		t.Fatalf("Get = %q, want 15", got)
	}
	if err := f.Set("14"); err != nil || th.AutocorrectHighlightColor != 14 {
		t.Fatalf("Set(14): color=%d err=%v", th.AutocorrectHighlightColor, err)
	}
}

func acBool(v bool) *bool { return &v }
