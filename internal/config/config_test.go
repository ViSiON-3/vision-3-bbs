package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQWKAPIConfig_Defaults(t *testing.T) {
	var c QWKAPIConfig
	if got := c.ListenAddr(); got != ":8666" {
		t.Errorf("ListenAddr default = %q, want :8666", got)
	}
	c.Host, c.Port = "127.0.0.1", 9000
	if got := c.ListenAddr(); got != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:9000", got)
	}
	if got := (&QWKAPIConfig{}).TokenTTL(); got != 24*time.Hour {
		t.Errorf("TokenTTL default = %v, want 24h", got)
	}
	if got := (&QWKAPIConfig{TokenTTLHours: 2}).TokenTTL(); got != 2*time.Hour {
		t.Errorf("TokenTTL = %v, want 2h", got)
	}
}

func TestLoadDoors_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	doors := []DoorConfig{
		{Code: "LORD", Name: "Legend of the Red Dragon", Commands: []string{"/usr/bin/lord", "-n", "{NODE}"}},
		{Code: "BRE", Name: "Barren Realms Elite", Commands: []string{"/usr/bin/bre"}},
	}
	data, _ := json.Marshal(doors)
	os.WriteFile(filepath.Join(tmpDir, "doors.json"), data, 0644)

	result, err := LoadDoors(filepath.Join(tmpDir, "doors.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 doors, got %d", len(result))
	}
	if len(result["LORD"].Commands) == 0 || result["LORD"].Commands[0] != "/usr/bin/lord" {
		t.Errorf("expected LORD command /usr/bin/lord, got %v", result["LORD"].Commands)
	}
	// Name is a display label: case must be preserved verbatim.
	if got := result["LORD"].Name; got != "Legend of the Red Dragon" {
		t.Errorf("Name = %q, want case preserved", got)
	}
}

// Menu lookups uppercase the door code (GetDoorConfig(ToUpper(input))), so
// a mixed-case code in a hand-edited doors.json must normalize on load or
// the door is unreachable.
func TestLoadDoors_UppercasesCodes(t *testing.T) {
	tmpDir := t.TempDir()
	doors := []DoorConfig{
		{Code: "lord", Name: "Legend of the Red Dragon"},
	}
	data, _ := json.Marshal(doors)
	os.WriteFile(filepath.Join(tmpDir, "doors.json"), data, 0644)

	result, err := LoadDoors(filepath.Join(tmpDir, "doors.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d, ok := result["LORD"]
	if !ok {
		t.Fatalf("expected key LORD for code lord, got keys %v", result)
	}
	if d.Code != "LORD" {
		t.Errorf("Code = %q, want LORD (key and Code must stay in sync)", d.Code)
	}
	if d.Name != "Legend of the Red Dragon" {
		t.Errorf("Name = %q, want case preserved", d.Name)
	}
}

// The loader must enforce the same code contract as the editor
// ([A-Z0-9_-]{1,16}), or hand-edited files can create registry keys that
// DOOR:CODE commands and DOORLIST alignment don't expect.
func TestLoadDoors_RejectsInvalidCodes(t *testing.T) {
	for _, bad := range []string{"BAD CODE!", "WAYTOOLONGDOORCODE", "NÖPE"} {
		t.Run(bad, func(t *testing.T) {
			tmpDir := t.TempDir()
			data, _ := json.Marshal([]DoorConfig{{Code: bad, Name: "X"}})
			os.WriteFile(filepath.Join(tmpDir, "doors.json"), data, 0644)

			if _, err := LoadDoors(filepath.Join(tmpDir, "doors.json")); err == nil {
				t.Errorf("expected error for invalid code %q", bad)
			}
		})
	}
}

func TestNormalizeDoorCode(t *testing.T) {
	if got, err := NormalizeDoorCode("  lord-2 "); err != nil || got != "LORD-2" {
		t.Errorf("NormalizeDoorCode = %q, %v; want LORD-2", got, err)
	}
	for _, bad := range []string{"", "  ", "BAD CODE!", "WAYTOOLONGDOORCODE"} {
		if _, err := NormalizeDoorCode(bad); err == nil {
			t.Errorf("NormalizeDoorCode(%q) should error", bad)
		}
	}
}

func TestLoadDoors_RejectsEmptyCode(t *testing.T) {
	tmpDir := t.TempDir()
	doors := []DoorConfig{
		{Code: "  ", Name: "LORD", Commands: []string{"/usr/bin/lord"}},
	}
	data, _ := json.Marshal(doors)
	os.WriteFile(filepath.Join(tmpDir, "doors.json"), data, 0644)

	if _, err := LoadDoors(filepath.Join(tmpDir, "doors.json")); err == nil {
		t.Error("expected error for door with blank code")
	}
}

// Duplicate detection must be case-insensitive since codes are uppercased.
func TestLoadDoors_DuplicateCodesCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	doors := []DoorConfig{
		{Code: "Lord", Commands: []string{"/usr/bin/lord"}},
		{Code: "LORD", Commands: []string{"/usr/bin/lord2"}},
	}
	data, _ := json.Marshal(doors)
	os.WriteFile(filepath.Join(tmpDir, "doors.json"), data, 0644)

	_, err := LoadDoors(filepath.Join(tmpDir, "doors.json"))
	if err == nil {
		t.Error("expected error for case-insensitive duplicate door codes")
	}
}

func TestLoadDoors_MissingFile(t *testing.T) {
	result, err := LoadDoors("/nonexistent/doors.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map for missing file, got %d entries", len(result))
	}
}

func TestLoadDoors_DuplicateCodes(t *testing.T) {
	tmpDir := t.TempDir()
	doors := []DoorConfig{
		{Code: "LORD", Commands: []string{"/usr/bin/lord"}},
		{Code: "LORD", Commands: []string{"/usr/bin/lord2"}},
	}
	data, _ := json.Marshal(doors)
	os.WriteFile(filepath.Join(tmpDir, "doors.json"), data, 0644)

	_, err := LoadDoors(filepath.Join(tmpDir, "doors.json"))
	if err == nil {
		t.Error("expected error for duplicate door codes")
	}
}

func TestLoadDoors_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "doors.json"), []byte("not json"), 0644)

	_, err := LoadDoors(filepath.Join(tmpDir, "doors.json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadLoginSequence_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	items := []LoginItem{
		{Command: "LASTCALLS"},
		{Command: "displayfile", Data: "welcome.ans", ClearScreen: true},
	}
	data, _ := json.Marshal(items)
	os.WriteFile(filepath.Join(tmpDir, "login.json"), data, 0644)

	result, err := LoadLoginSequence(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	// Commands should be normalized to uppercase
	if result[1].Command != "DISPLAYFILE" {
		t.Errorf("expected DISPLAYFILE, got %s", result[1].Command)
	}
	if !result[1].ClearScreen {
		t.Error("expected ClearScreen to be true")
	}
}

func TestLoadLoginSequence_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := LoadLoginSequence(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return default sequence
	if len(result) != 3 {
		t.Fatalf("expected default 3-item sequence, got %d", len(result))
	}
	if result[0].Command != "LASTCALLS" {
		t.Errorf("expected LASTCALLS as first default item, got %s", result[0].Command)
	}
}

func TestLoadServerConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	// No config.json — should return defaults
	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SSHPort != 2222 {
		t.Errorf("expected default SSH port 2222, got %d", result.SSHPort)
	}
	if result.MaxNodes != 10 {
		t.Errorf("expected default MaxNodes 10, got %d", result.MaxNodes)
	}
	if result.BoardName != "ViSiON/3 BBS" {
		t.Errorf("expected default board name, got %s", result.BoardName)
	}
	if !result.WFCEnabled {
		t.Error("expected WFCEnabled to default to true")
	}
}

func TestLoadServerConfig_CustomValues(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := map[string]interface{}{
		"boardName":  "Test BBS",
		"sshPort":    3333,
		"maxNodes":   50,
		"wfcEnabled": false,
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BoardName != "Test BBS" {
		t.Errorf("expected 'Test BBS', got %s", result.BoardName)
	}
	if result.SSHPort != 3333 {
		t.Errorf("expected SSH port 3333, got %d", result.SSHPort)
	}
	if result.MaxNodes != 50 {
		t.Errorf("expected MaxNodes 50, got %d", result.MaxNodes)
	}
	if result.WFCEnabled {
		t.Error("expected WFCEnabled false when explicitly disabled in config.json")
	}
}

func TestLoadThemeConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := LoadThemeConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.YesNoHighlightColor != 112 {
		t.Errorf("expected default highlight color 112, got %d", result.YesNoHighlightColor)
	}
	if result.YesNoRegularColor != 15 {
		t.Errorf("expected default regular color 15, got %d", result.YesNoRegularColor)
	}
}

func TestLoadThemeConfig_CustomValues(t *testing.T) {
	tmpDir := t.TempDir()
	theme := map[string]interface{}{
		"yesNoHighlightColor": 200,
		"yesNoRegularColor":   7,
	}
	data, _ := json.Marshal(theme)
	os.WriteFile(filepath.Join(tmpDir, "theme.json"), data, 0644)

	result, err := LoadThemeConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.YesNoHighlightColor != 200 {
		t.Errorf("expected highlight color 200, got %d", result.YesNoHighlightColor)
	}
}

func TestLoadEventsConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := LoadEventsConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Enabled {
		t.Error("expected events disabled by default")
	}
	if result.MaxConcurrentEvents != 3 {
		t.Errorf("expected default max concurrent 3, got %d", result.MaxConcurrentEvents)
	}
}

func TestLoadEventsConfig_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := EventsConfig{
		Enabled:             true,
		MaxConcurrentEvents: 5,
		Events: []EventConfig{
			{ID: "test", Name: "Test Event", Schedule: "0 * * * *", Command: "echo", Enabled: true},
			{ID: "disabled", Name: "Disabled", Schedule: "0 0 * * *", Command: "echo", Enabled: false},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "events.json"), data, 0644)

	result, err := LoadEventsConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Enabled {
		t.Error("expected events enabled")
	}
	if result.MaxConcurrentEvents != 5 {
		t.Errorf("expected max concurrent 5, got %d", result.MaxConcurrentEvents)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result.Events))
	}
}

func TestLoadEventsConfig_ZeroMaxConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := map[string]interface{}{
		"enabled":               true,
		"max_concurrent_events": 0,
		"events":                []interface{}{},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "events.json"), data, 0644)

	result, err := LoadEventsConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should default to 3 when 0 or negative
	if result.MaxConcurrentEvents != 3 {
		t.Errorf("expected default max concurrent 3 when set to 0, got %d", result.MaxConcurrentEvents)
	}
}

func TestLoadFTNConfig_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := LoadFTNConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Networks == nil {
		t.Error("expected initialized (empty) Networks map")
	}
	if len(result.Networks) != 0 {
		t.Errorf("expected 0 networks, got %d", len(result.Networks))
	}
}

func TestLoadFTNConfig_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := FTNConfig{
		DupeDBPath:        "data/ftn/dupes.json",
		InboundPath:       "data/ftn/in",
		OutboundPath:      "data/ftn/temp_out",
		BinkdOutboundPath: "data/ftn/out",
		TempPath:          "data/ftn/temp",
		Networks: map[string]FTNNetworkConfig{
			"fsxnet": {
				InternalTosserEnabled: true,
				OwnAddress:            "21:3/110",
				Links: []FTNLinkConfig{
					{Address: "21:1/100", PacketPassword: "secret", Name: "Hub"},
				},
			},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "ftn.json"), data, 0644)

	result, err := LoadFTNConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Networks) != 1 {
		t.Fatalf("expected 1 network, got %d", len(result.Networks))
	}
	net := result.Networks["fsxnet"]
	if net.OwnAddress != "21:3/110" {
		t.Errorf("expected address 21:3/110, got %s", net.OwnAddress)
	}
	if len(net.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(net.Links))
	}
	if !net.InternalTosserEnabled {
		t.Error("expected InternalTosserEnabled true")
	}
	link := net.Links[0]
	if link.Address != "21:1/100" {
		t.Errorf("expected link address 21:1/100, got %s", link.Address)
	}
	if link.PacketPassword != "secret" {
		t.Errorf("expected PacketPassword 'secret', got %s", link.PacketPassword)
	}
	if link.Name != "Hub" {
		t.Errorf("expected link name 'Hub', got %s", link.Name)
	}
	if result.DupeDBPath != "data/ftn/dupes.json" {
		t.Errorf("expected DupeDBPath 'data/ftn/dupes.json', got %s", result.DupeDBPath)
	}
	if result.InboundPath != "data/ftn/in" {
		t.Errorf("expected InboundPath 'data/ftn/in', got %s", result.InboundPath)
	}
	if result.OutboundPath != "data/ftn/temp_out" {
		t.Errorf("expected OutboundPath 'data/ftn/temp_out', got %s", result.OutboundPath)
	}
}

// TestLoadFTNConfig_TosserEnabledMissingPaths is a regression test for issue #15.
// LoadFTNConfig must succeed even when internal_tosser_enabled is true but the
// global FTN paths are blank, so the config editor can open and let the sysop
// correct the misconfiguration. ValidateFTNConfig is the appropriate place for
// the runtime path check.
func TestLoadFTNConfig_TosserEnabledMissingPaths(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := FTNConfig{
		// Global paths intentionally blank — simulates post-first-run incomplete config.
		Networks: map[string]FTNNetworkConfig{
			"fsxnet": {InternalTosserEnabled: true, OwnAddress: "21:3/110"},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "ftn.json"), data, 0644)

	result, err := LoadFTNConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadFTNConfig must not fail with incomplete paths (issue #15): %v", err)
	}
	if !result.Networks["fsxnet"].InternalTosserEnabled {
		t.Error("expected InternalTosserEnabled to be preserved after load")
	}

	// ValidateFTNConfig should catch the missing paths at runtime.
	if err := ValidateFTNConfig(result); err == nil {
		t.Error("ValidateFTNConfig should reject config with tosser enabled but no paths set")
	}
}

func TestValidateFTNConfig(t *testing.T) {
	makeNet := func(enabled bool) map[string]FTNNetworkConfig {
		return map[string]FTNNetworkConfig{
			"fsxnet": {InternalTosserEnabled: enabled},
		}
	}

	// No tosser enabled: no paths required.
	if err := ValidateFTNConfig(FTNConfig{Networks: makeNet(false)}); err != nil {
		t.Errorf("expected no error with tosser disabled, got: %v", err)
	}

	// Tosser enabled, all paths set: should pass.
	full := FTNConfig{
		InboundPath:       "data/ftn/in",
		OutboundPath:      "data/ftn/out",
		BinkdOutboundPath: "data/ftn/binkd",
		TempPath:          "data/ftn/temp",
		Networks:          makeNet(true),
	}
	if err := ValidateFTNConfig(full); err != nil {
		t.Errorf("expected no error with all paths set, got: %v", err)
	}

	// Tosser enabled, missing inbound_path: should fail.
	missing := full
	missing.InboundPath = ""
	if err := ValidateFTNConfig(missing); err == nil {
		t.Error("expected error when inbound_path is missing")
	}

	// Tosser enabled, missing outbound_path: should fail.
	missing = full
	missing.OutboundPath = ""
	if err := ValidateFTNConfig(missing); err == nil {
		t.Error("expected error when outbound_path is missing")
	}

	// Tosser enabled, missing binkd_outbound_path: should fail.
	missing = full
	missing.BinkdOutboundPath = ""
	if err := ValidateFTNConfig(missing); err == nil {
		t.Error("expected error when binkd_outbound_path is missing")
	}

	// Tosser enabled, missing temp_path: should fail.
	missing = full
	missing.TempPath = ""
	if err := ValidateFTNConfig(missing); err == nil {
		t.Error("expected error when temp_path is missing")
	}
}

func TestFTNLinkConfig_LegacyPasswordMigration(t *testing.T) {
	// Legacy config uses "password"; new config uses "packet_password".
	// When packet_password is absent (omitted), the legacy password should be used.
	// When packet_password is explicitly empty "", legacy password must NOT override it.
	legacy := []byte(`{"address":"21:1/100","password":"legacypass","name":"Hub"}`)
	var link FTNLinkConfig
	if err := json.Unmarshal(legacy, &link); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if link.PacketPassword != "legacypass" {
		t.Errorf("expected legacy password migration, got %q", link.PacketPassword)
	}

	// Explicit empty packet_password should NOT be overridden by legacy password.
	explicit := []byte(`{"address":"21:1/100","packet_password":"","password":"legacypass","name":"Hub"}`)
	var link2 FTNLinkConfig
	if err := json.Unmarshal(explicit, &link2); err != nil {
		t.Fatalf("unmarshal explicit empty: %v", err)
	}
	if link2.PacketPassword != "" {
		t.Errorf("expected explicit empty password to be preserved, got %q", link2.PacketPassword)
	}
}

func TestLoadServerConfig_PartialOverlayPreservesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	// Only override boardName — everything else should keep defaults
	cfg := map[string]interface{}{
		"boardName": "Custom BBS",
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BoardName != "Custom BBS" {
		t.Errorf("expected 'Custom BBS', got %s", result.BoardName)
	}
	// Verify defaults are preserved for unset fields
	if result.SysOpLevel != 255 {
		t.Errorf("expected default SysOpLevel 255, got %d", result.SysOpLevel)
	}
	if result.SSHPort != 2222 {
		t.Errorf("expected default SSHPort 2222, got %d", result.SSHPort)
	}
	if result.MaxFailedLogins != 5 {
		t.Errorf("expected default MaxFailedLogins 5, got %d", result.MaxFailedLogins)
	}
	if result.LockoutMinutes != 30 {
		t.Errorf("expected default LockoutMinutes 30, got %d", result.LockoutMinutes)
	}
}

func TestLoadServerConfig_LoggingDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	// config.json with no "logging" key — back-compat: defaults must apply.
	data, _ := json.Marshal(map[string]interface{}{"boardName": "X"})
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lg := result.Logging
	if lg.Dir != DefaultLogDir {
		t.Errorf("Dir = %q, want %q", lg.Dir, DefaultLogDir)
	}
	if lg.Level != DefaultLogLevel {
		t.Errorf("Level = %q, want %q", lg.Level, DefaultLogLevel)
	}
	if !lg.Cache {
		t.Error("Cache should default to true")
	}
	if lg.Type != LogTypeNone {
		t.Errorf("Type = %d, want %d", lg.Type, LogTypeNone)
	}
	if lg.MaxFiles != DefaultLogMaxFiles || lg.MaxSizeKB != DefaultLogMaxSizeKB {
		t.Errorf("MaxFiles/MaxSizeKB = %d/%d, want %d/%d", lg.MaxFiles, lg.MaxSizeKB, DefaultLogMaxFiles, DefaultLogMaxSizeKB)
	}
}

func TestLoadServerConfig_LoggingPartialOverlay(t *testing.T) {
	tmpDir := t.TempDir()
	// Only override level; sibling logging fields keep their defaults.
	data, _ := json.Marshal(map[string]interface{}{
		"logging": map[string]interface{}{"level": "DEBUG"},
	})
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Logging.Level != "DEBUG" {
		t.Errorf("Level = %q, want DEBUG", result.Logging.Level)
	}
	if result.Logging.Dir != DefaultLogDir {
		t.Errorf("Dir = %q, want default %q (partial overlay should preserve siblings)", result.Logging.Dir, DefaultLogDir)
	}
}

func TestLoggingConfig_Normalize(t *testing.T) {
	t.Run("empty strings get defaults", func(t *testing.T) {
		c := LoggingConfig{}
		c.Normalize()
		if c.Dir != DefaultLogDir || c.Level != DefaultLogLevel {
			t.Errorf("Dir/Level = %q/%q, want %q/%q", c.Dir, c.Level, DefaultLogDir, DefaultLogLevel)
		}
	})
	t.Run("size type clamps non-positive MaxFiles and MaxSizeKB", func(t *testing.T) {
		c := LoggingConfig{Type: LogTypeSize, MaxFiles: 0, MaxSizeKB: 0}
		c.Normalize()
		if c.MaxFiles != DefaultLogMaxFiles {
			t.Errorf("MaxFiles = %d, want clamped to %d", c.MaxFiles, DefaultLogMaxFiles)
		}
		if c.MaxSizeKB != DefaultLogMaxSizeKB {
			t.Errorf("MaxSizeKB = %d, want clamped to %d", c.MaxSizeKB, DefaultLogMaxSizeKB)
		}
	})
	t.Run("daily type clamps MaxFiles but leaves MaxSizeKB", func(t *testing.T) {
		c := LoggingConfig{Type: LogTypeDaily, MaxFiles: -3, MaxSizeKB: 0}
		c.Normalize()
		if c.MaxFiles != DefaultLogMaxFiles {
			t.Errorf("MaxFiles = %d, want clamped to %d", c.MaxFiles, DefaultLogMaxFiles)
		}
		if c.MaxSizeKB != 0 {
			t.Errorf("MaxSizeKB = %d, want left at 0 (not used by daily type)", c.MaxSizeKB)
		}
	})
	t.Run("none type leaves numeric fields untouched", func(t *testing.T) {
		c := LoggingConfig{Type: LogTypeNone, MaxFiles: 0, MaxSizeKB: 0}
		c.Normalize()
		if c.MaxFiles != 0 || c.MaxSizeKB != 0 {
			t.Errorf("none type should not clamp; got MaxFiles=%d MaxSizeKB=%d", c.MaxFiles, c.MaxSizeKB)
		}
	})
	t.Run("valid values preserved", func(t *testing.T) {
		c := LoggingConfig{Dir: "/var/log/bbs", Level: "WARN", Type: LogTypeSize, MaxFiles: 10, MaxSizeKB: 512}
		c.Normalize()
		if c.Dir != "/var/log/bbs" || c.Level != "WARN" || c.MaxFiles != 10 || c.MaxSizeKB != 512 {
			t.Errorf("valid config mutated: %+v", c)
		}
	})
}

func TestLoadStrings_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := map[string]interface{}{
		"pauseString":   "Press any key...",
		"anonymousName": "Anonymous Coward",
		"defColor1":     14,
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "strings.json"), data, 0644)

	result, err := LoadStrings(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PauseString != "Press any key..." {
		t.Errorf("expected 'Press any key...', got %s", result.PauseString)
	}
	if result.AnonymousName != "Anonymous Coward" {
		t.Errorf("expected 'Anonymous Coward', got %s", result.AnonymousName)
	}
	if result.DefColor1 != 14 {
		t.Errorf("expected DefColor1 14, got %d", result.DefColor1)
	}
}

func TestLoadStrings_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := LoadStrings(tmpDir)
	if err == nil {
		t.Error("expected error for missing strings.json")
	}
}

func TestLoadStrings_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "strings.json"), []byte("{bad json"), 0644)

	_, err := LoadStrings(tmpDir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadOneLiners_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	content := "First liner\nSecond liner\nThird liner\n"
	os.WriteFile(filepath.Join(tmpDir, "oneliners.dat"), []byte(content), 0644)

	result, err := LoadOneLiners(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 oneliners, got %d", len(result))
	}
	if result[0] != "First liner" {
		t.Errorf("expected 'First liner', got %s", result[0])
	}
}

func TestLoadOneLiners_WindowsLineEndings(t *testing.T) {
	tmpDir := t.TempDir()
	content := "Line one\r\nLine two\r\nLine three\r\n"
	os.WriteFile(filepath.Join(tmpDir, "oneliners.dat"), []byte(content), 0644)

	result, err := LoadOneLiners(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 oneliners with CRLF, got %d", len(result))
	}
	if result[0] != "Line one" {
		t.Errorf("expected 'Line one', got %q", result[0])
	}
}

func TestLoadOneLiners_SkipsEmptyLines(t *testing.T) {
	tmpDir := t.TempDir()
	content := "Line one\n\n\nLine two\n  \n"
	os.WriteFile(filepath.Join(tmpDir, "oneliners.dat"), []byte(content), 0644)

	result, err := LoadOneLiners(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 oneliners (empty lines skipped), got %d: %v", len(result), result)
	}
}

func TestLoadOneLiners_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	result, err := LoadOneLiners(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 oneliners for missing file, got %d", len(result))
	}
}

func TestLoadOneLiners_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "oneliners.dat"), []byte(""), 0644)

	result, err := LoadOneLiners(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 oneliners from empty file, got %d", len(result))
	}
}

func TestLoadServerConfig_AllowNewUsersDefaultTrue(t *testing.T) {
	tmpDir := t.TempDir()
	// No config.json — AllowNewUsers should default to true
	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllowNewUsers {
		t.Error("expected AllowNewUsers to default to true")
	}
}

func TestLoadServerConfig_AllowNewUsersExplicitFalse(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := map[string]interface{}{
		"allowNewUsers": false,
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AllowNewUsers {
		t.Error("expected AllowNewUsers to be false when explicitly set")
	}
}

func TestLoadServerConfig_AllowNewUsersExplicitTrue(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := map[string]interface{}{
		"boardName":     "Test BBS",
		"allowNewUsers": true,
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllowNewUsers {
		t.Error("expected AllowNewUsers to be true when explicitly set")
	}
	if result.BoardName != "Test BBS" {
		t.Errorf("expected 'Test BBS', got %s", result.BoardName)
	}
}

func TestLoadServerConfig_AllowNewUsersPreservedInPartialOverlay(t *testing.T) {
	tmpDir := t.TempDir()
	// Only override boardName — AllowNewUsers should keep default (true)
	cfg := map[string]interface{}{
		"boardName": "Partial BBS",
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	result, err := LoadServerConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllowNewUsers {
		t.Error("expected AllowNewUsers to remain true when not specified in config")
	}
}

func TestLoadStrings_NewUsersClosedStr(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := map[string]interface{}{
		"newUsersClosedStr": "|12Registration is closed.|07",
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(tmpDir, "strings.json"), data, 0644)

	result, err := LoadStrings(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NewUsersClosedStr != "|12Registration is closed.|07" {
		t.Errorf("expected custom NewUsersClosedStr, got %q", result.NewUsersClosedStr)
	}
}

func TestServerConfig_QWKID_RoundTrip(t *testing.T) {
	// Explicit qwkID in config.json loads through.
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]interface{}{"qwkID": "VISION3"})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.QWKID != "VISION3" {
		t.Errorf("loaded QWKID: want VISION3, got %q", loaded.QWKID)
	}

	// Absent qwkID defaults to empty.
	def, err := LoadServerConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if def.QWKID != "" {
		t.Errorf("default QWKID: want empty, got %q", def.QWKID)
	}

	// Save then load preserves QWKID. Start from a defaults-initialized config
	// (as the editor does: load, edit, save) rather than a zeroed struct, so the
	// round-trip reflects a realistic save and other defaults are retained.
	saveDir := t.TempDir()
	cfg, err := LoadServerConfig(saveDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.QWKID = "ABC123"
	if err := SaveServerConfig(saveDir, cfg); err != nil {
		t.Fatal(err)
	}
	back, err := LoadServerConfig(saveDir)
	if err != nil {
		t.Fatal(err)
	}
	if back.QWKID != "ABC123" {
		t.Errorf("round-trip QWKID: want ABC123, got %q", back.QWKID)
	}
	if back.BoardName != cfg.BoardName {
		t.Errorf("round-trip should retain other defaults: BoardName want %q, got %q", cfg.BoardName, back.BoardName)
	}
}
