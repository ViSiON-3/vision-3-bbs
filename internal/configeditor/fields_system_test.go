package configeditor

import (
	"reflect"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestSysFieldsRegistration_QWKIDNormalizesOnSet(t *testing.T) {
	cfg := &config.ServerConfig{}
	fields := sysFieldsRegistration(cfg)

	var f *fieldDef
	for i := range fields {
		if fields[i].Label == "QWK ID" {
			f = &fields[i]
			break
		}
	}
	if f == nil {
		t.Fatal("QWK ID field not found in registration screen")
	}
	if err := f.Set("my id!"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.QWKID != "MYID" {
		t.Errorf("Set should normalize: want cfg.QWKID == MYID, got %q", cfg.QWKID)
	}
	if got := f.Get(); got != "MYID" {
		t.Errorf("Get: want MYID, got %q", got)
	}
}

func TestSysFieldsQWKAPI(t *testing.T) {
	cfg := &config.ServerConfig{}
	fields := sysFieldsQWKAPI(cfg)

	find := func(label string) *fieldDef {
		for i := range fields {
			if fields[i].Label == label {
				return &fields[i]
			}
		}
		t.Fatalf("%s field not found in QWK API screen", label)
		return nil
	}

	enabled := find("Enabled")
	if got := enabled.Get(); got != "N" {
		t.Errorf("Enabled default: want N, got %q", got)
	}
	if err := enabled.Set("Y"); err != nil {
		t.Fatalf("Set Enabled: %v", err)
	}
	if !cfg.QWKAPI.Enabled {
		t.Error("Set(Y) should enable cfg.QWKAPI.Enabled")
	}

	if got := find("Port").Get(); got != "8666" {
		t.Errorf("Port default: want 8666, got %q", got)
	}
	if got := find("Token TTL Hrs").Get(); got != "24" {
		t.Errorf("Token TTL default: want 24, got %q", got)
	}

	// Runtime (TokenTTL) treats any non-positive value as 24h; the editor
	// must display the same for hand-edited negative values.
	cfg.QWKAPI.TokenTTLHours = -5
	if got := find("Token TTL Hrs").Get(); got != "24" {
		t.Errorf("Token TTL for negative value: want 24, got %q", got)
	}
}

func TestSysFieldsLevels_WFCAccess(t *testing.T) {
	cfg := &config.ServerConfig{WFCEnabled: true}
	fields := sysFieldsLevels(cfg)

	var f *fieldDef
	for i := range fields {
		if fields[i].Label == "WFC Access" {
			f = &fields[i]
			break
		}
	}
	if f == nil {
		t.Fatal("WFC Access field not found in Access Levels screen")
	}
	if f.Type != ftYesNo {
		t.Errorf("WFC Access type: want ftYesNo, got %v", f.Type)
	}
	if got := f.Get(); got != "Y" {
		t.Errorf("Get with WFCEnabled=true: want Y, got %q", got)
	}
	if err := f.Set("N"); err != nil {
		t.Fatalf("Set(N): %v", err)
	}
	if cfg.WFCEnabled {
		t.Error("Set(N) should clear cfg.WFCEnabled")
	}
	if got := f.Get(); got != "N" {
		t.Errorf("Get after Set(N): want N, got %q", got)
	}
}

func TestMenuListsPartitionAllScreens(t *testing.T) {
	sys := systemConfigMenuItems()
	sec := securityMenuItems()

	wantSys := []string{
		"BBS Registration", "Server Setup", "Default Settings",
		"DOS Emulation", "Logging", "QWK Mobile API",
	}
	wantSec := []string{
		"Access Levels", "Connection Limits", "Bot Defense",
		"IP Blocklist/Allowlist", "New User Voting (NUV)",
	}

	gotSys := make([]string, len(sys))
	for i, it := range sys {
		gotSys[i] = it.Label
	}
	gotSec := make([]string, len(sec))
	for i, it := range sec {
		gotSec[i] = it.Label
	}
	if !reflect.DeepEqual(gotSys, wantSys) {
		t.Errorf("systemConfigMenuItems labels = %v, want %v", gotSys, wantSys)
	}
	if !reflect.DeepEqual(gotSec, wantSec) {
		t.Errorf("securityMenuItems labels = %v, want %v", gotSec, wantSec)
	}
}

func TestMenuItemBuildersNonEmpty(t *testing.T) {
	m := newRecordModel()
	m.configs.Server = config.ServerConfig{BoardName: "Test BBS", QWKID: "TEST"}
	for _, it := range append(systemConfigMenuItems(), securityMenuItems()...) {
		if it.Build == nil {
			t.Errorf("item %q has nil Build", it.Label)
			continue
		}
		if len(it.Build(m)) == 0 {
			t.Errorf("item %q Build returned no fields", it.Label)
		}
	}
}

func TestSysFieldsBotDefenseRoundTrip(t *testing.T) {
	cfg := &config.ServerConfig{}
	fields := sysFieldsBotDefense(cfg)
	if len(fields) != 10 {
		t.Fatalf("got %d fields, want 10", len(fields))
	}
	byLabel := map[string]fieldDef{}
	for _, f := range fields {
		byLabel[f.Label] = f
	}
	if err := byLabel["Enable Gate"].Set("Y"); err != nil || !cfg.EnableChallengeGate {
		t.Errorf("Enable Gate Set failed: err=%v val=%v", err, cfg.EnableChallengeGate)
	}
	if err := byLabel["Challenge Key"].Set("*"); err != nil || cfg.ChallengeGateKey != "*" {
		t.Errorf("Challenge Key Set failed: %q", cfg.ChallengeGateKey)
	}
	if err := byLabel["Timeout Secs"].Set("30"); err != nil || cfg.ChallengeGateTimeoutSeconds != 30 {
		t.Errorf("Timeout Set failed: %d", cfg.ChallengeGateTimeoutSeconds)
	}
	if got := byLabel["Enable Gate"].Get(); got != "Y" {
		t.Errorf("Enable Gate Get = %q, want Y", got)
	}
}
