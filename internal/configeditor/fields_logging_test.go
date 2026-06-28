package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func fieldByLabel(t *testing.T, fields []fieldDef, label string) *fieldDef {
	t.Helper()
	for i := range fields {
		if fields[i].Label == label {
			return &fields[i]
		}
	}
	t.Fatalf("field %q not found", label)
	return nil
}

func TestSysFieldsLogging_GetSet(t *testing.T) {
	cfg := &config.ServerConfig{Logging: config.LoggingConfig{
		Dir: "data/logs", Level: "INFO", Cache: true,
		Type: config.LogTypeNone, MaxFiles: 5, MaxSizeKB: 1024,
	}}
	fields := sysFieldsLogging(cfg)
	if len(fields) != 6 {
		t.Fatalf("expected 6 logging fields, got %d", len(fields))
	}

	// Directory round-trips.
	dir := fieldByLabel(t, fields, "Log Directory")
	if dir.Get() != "data/logs" {
		t.Errorf("Dir Get = %q", dir.Get())
	}
	if err := dir.Set("/var/log/bbs"); err != nil || cfg.Logging.Dir != "/var/log/bbs" {
		t.Errorf("Dir Set: err=%v dir=%q", err, cfg.Logging.Dir)
	}

	// Min Level lookup round-trips and offers all four levels.
	lvl := fieldByLabel(t, fields, "Min Level")
	if lvl.Type != ftLookup {
		t.Error("Min Level should be ftLookup")
	}
	if got := len(lvl.LookupItems()); got != 4 {
		t.Errorf("expected 4 level options, got %d", got)
	}
	if err := lvl.Set("WARN"); err != nil || cfg.Logging.Level != "WARN" {
		t.Errorf("Level Set: err=%v level=%q", err, cfg.Logging.Level)
	}
	if lvl.Get() != "WARN" {
		t.Errorf("Level Get = %q, want WARN", lvl.Get())
	}

	// Cache toggles via Y/N.
	cache := fieldByLabel(t, fields, "Cache Writes")
	if cache.Get() != "Y" {
		t.Errorf("Cache Get = %q, want Y", cache.Get())
	}
	if err := cache.Set("N"); err != nil || cfg.Logging.Cache {
		t.Errorf("Cache Set N: err=%v cache=%v", err, cfg.Logging.Cache)
	}

	// Max Files validates integers.
	mf := fieldByLabel(t, fields, "Max Files")
	if err := mf.Set("3"); err != nil || cfg.Logging.MaxFiles != 3 {
		t.Errorf("MaxFiles Set: err=%v val=%d", err, cfg.Logging.MaxFiles)
	}
	if err := mf.Set("notanint"); err == nil {
		t.Error("MaxFiles Set should reject non-integer input")
	}
}

func TestSysFieldsLogging_RollingTypeLabelMapping(t *testing.T) {
	cfg := &config.ServerConfig{Logging: config.LoggingConfig{Type: config.LogTypeNone}}
	rt := fieldByLabel(t, sysFieldsLogging(cfg), "Rolling Type")

	if rt.Get() != "None" {
		t.Errorf("Type 0 Get = %q, want None", rt.Get())
	}
	cases := map[string]int{
		"Size":  config.LogTypeSize,
		"Daily": config.LogTypeDaily,
		"None":  config.LogTypeNone,
	}
	for label, want := range cases {
		if err := rt.Set(label); err != nil {
			t.Fatalf("Set(%q): %v", label, err)
		}
		if cfg.Logging.Type != want {
			t.Errorf("Set(%q) -> Type %d, want %d", label, cfg.Logging.Type, want)
		}
		if rt.Get() != label {
			t.Errorf("after Set(%q), Get = %q", label, rt.Get())
		}
	}
	// The picker offers exactly the three rolling types.
	if got := len(rt.LookupItems()); got != 3 {
		t.Errorf("expected 3 rolling-type options, got %d", got)
	}

	// An unmapped/hand-edited type falls back to "None" rather than blank.
	cfg.Logging.Type = 99
	if got := rt.Get(); got != "None" {
		t.Errorf("unmapped type Get = %q, want None fallback", got)
	}
}

func TestBuildSysFields_LoggingScreenWired(t *testing.T) {
	m := &Model{configs: &allConfigs{}}
	m.configs.Server.Logging = config.LoggingConfig{Dir: "data/logs", Level: "INFO"}

	fields := m.buildSysFields(8)
	if len(fields) != 6 {
		t.Fatalf("buildSysFields(8) returned %d fields, want 6 (Logging screen)", len(fields))
	}
	// Sanity: it's the logging screen, not some other screen's fields.
	fieldByLabel(t, fields, "Rolling Type")
}
