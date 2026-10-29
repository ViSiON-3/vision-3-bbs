package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func hubApproveFields(t *testing.T, m *Model) (subs, areas fieldDef) {
	t.Helper()
	byLabel := make(map[string]fieldDef)
	for _, f := range m.sysFieldsNetwork(&config.ServerConfig{}) {
		byLabel[f.Label] = f
	}
	subs, ok := byLabel["Auto Approve"]
	if !ok {
		t.Fatal("missing 'Auto Approve' field")
	}
	areas, ok = byLabel["Auto Approve Areas"]
	if !ok {
		t.Fatal("missing 'Auto Approve Areas' field")
	}
	return subs, areas
}

// An older config has no autoApproveAreas and follows autoApprove. Editing
// Auto Approve must not change how area proposals are handled.
func TestAutoApproveEditLeavesAreaApprovalOnOldConfig(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.V3Net.Hub.AutoApprove = false
	subs, areas := hubApproveFields(t, &m)

	if err := subs.Set("Y"); err != nil {
		t.Fatal(err)
	}
	if !m.configs.V3Net.Hub.AutoApprove {
		t.Error("Set(Y) did not enable subscriber auto-approve")
	}
	if got := areas.Get(); got != "N" {
		t.Errorf("Auto Approve Areas = %q after editing Auto Approve, want N", got)
	}
	if p := m.configs.V3Net.Hub.AutoApproveAreas; p == nil || *p {
		t.Errorf("AutoApproveAreas = %v, want pinned false", p)
	}
}

func TestAutoApproveEditKeepsExplicitAreaSetting(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	on := true
	m.configs.V3Net.Hub.AutoApproveAreas = &on
	subs, areas := hubApproveFields(t, &m)

	if err := subs.Set("N"); err != nil {
		t.Fatal(err)
	}
	if got := areas.Get(); got != "Y" {
		t.Errorf("Auto Approve Areas = %q, want Y", got)
	}
}

func TestSignedSubscribeSetting(t *testing.T) {
	testHubYesNoSetting(t, "Signed Subscribe", func(m *Model) bool { return m.configs.V3Net.Hub.RequireSignedSubscribe })
}

func TestSignedMessagesSetting(t *testing.T) {
	testHubYesNoSetting(t, "Signed Messages", func(m *Model) bool { return m.configs.V3Net.Hub.RequireSignedMessages })
}

// testHubYesNoSetting checks that the hub Y/N field label defaults to N and
// that Y and N round-trip into the config value stored reads.
func testHubYesNoSetting(t *testing.T, label string, stored func(*Model) bool) {
	t.Helper()
	m := Model{configs: &allConfigs{}}
	for _, f := range m.sysFieldsNetwork(&config.ServerConfig{}) {
		if f.Label != label {
			continue
		}
		if got := f.Get(); got != "N" {
			t.Fatalf("old config default=%q, want N", got)
		}
		for _, value := range []string{"Y", "N"} {
			if err := f.Set(value); err != nil {
				t.Fatal(err)
			}
			if got := f.Get(); got != value {
				t.Fatalf("got %q, want %q", got, value)
			}
			if got := stored(&m); got != (value == "Y") {
				t.Fatalf("after Set(%q) config value = %v", value, got)
			}
		}
		return
	}
	t.Fatalf("missing %s field", label)
}
