package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestSysFieldsNetworkBinkdFields(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.FTN.Binkd = config.BinkdServerConfig{
		Enabled: false, Port: 24554, BinaryPath: "bin/binkd", LogLevel: 4, ExportSecs: 300,
	}
	cfg := &config.ServerConfig{}
	fields := m.sysFieldsNetwork(cfg)

	byLabel := make(map[string]fieldDef)
	for _, f := range fields {
		byLabel[f.Label] = f
	}

	f, ok := byLabel["Binkd Mailer"]
	if !ok {
		t.Fatal("missing 'Binkd Mailer' field")
	}
	if got := f.Get(); got != "N" {
		t.Errorf("Binkd Mailer initial = %q, want N", got)
	}
	if err := f.Set("Y"); err != nil {
		t.Fatal(err)
	}
	if !m.configs.FTN.Binkd.Enabled {
		t.Error("Set(Y) did not enable binkd")
	}

	p, ok := byLabel["Binkd Port"]
	if !ok {
		t.Fatal("missing 'Binkd Port' field")
	}
	if got := p.Get(); got != "24554" {
		t.Errorf("Binkd Port = %q, want 24554", got)
	}
	if err := p.Set("24555"); err != nil {
		t.Fatal(err)
	}
	if m.configs.FTN.Binkd.Port != 24555 {
		t.Errorf("Port = %d, want 24555", m.configs.FTN.Binkd.Port)
	}

	for _, label := range []string{"Binkd Binary", "Binkd Log Lvl", "Export Secs"} {
		if _, ok := byLabel[label]; !ok {
			t.Errorf("missing %q field", label)
		}
	}
}
