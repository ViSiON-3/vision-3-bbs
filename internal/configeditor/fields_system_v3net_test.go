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
