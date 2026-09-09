package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// The sysop should not be able to reach the impossible state through the
// editor at all, in either direction.
func TestEditorRefusesAutoValidateWhileNUVOn(t *testing.T) {
	cfg := &config.ServerConfig{UseNUV: true}
	f := fieldByLabel(t, sysFieldsLevels(cfg), "Auto Validate")

	err := f.Set("Y")
	if err == nil {
		t.Fatal("enabling Auto Validate while NUV is on should be refused")
	}
	if !strings.Contains(err.Error(), "New User Voting") {
		t.Errorf("error should name the conflicting setting, got %q", err)
	}
	if cfg.AutoValidateNewUsers {
		t.Error("the value was changed despite the error")
	}
}

func TestEditorRefusesNUVWhileAutoValidateOn(t *testing.T) {
	cfg := &config.ServerConfig{AutoValidateNewUsers: true}
	f := fieldByLabel(t, sysFieldsNUV(cfg), "Use NUV")

	err := f.Set("Y")
	if err == nil {
		t.Fatal("enabling NUV while Auto Validate is on should be refused")
	}
	if !strings.Contains(err.Error(), "Auto Validate") {
		t.Errorf("error should name the conflicting setting, got %q", err)
	}
	if cfg.UseNUV {
		t.Error("the value was changed despite the error")
	}
}

// Turning either OFF is always allowed — otherwise a sysop could not escape
// the state to enable the other one.
func TestEditorAllowsTurningEitherOff(t *testing.T) {
	cfg := &config.ServerConfig{UseNUV: true}
	if err := fieldByLabel(t, sysFieldsNUV(cfg), "Use NUV").Set("N"); err != nil {
		t.Fatalf("turning NUV off should be allowed: %v", err)
	}
	if err := fieldByLabel(t, sysFieldsLevels(cfg), "Auto Validate").Set("Y"); err != nil {
		t.Fatalf("enabling Auto Validate after NUV is off should be allowed: %v", err)
	}
	if !cfg.AutoValidateNewUsers || cfg.UseNUV {
		t.Errorf("unexpected state: useNuv=%v autoValidate=%v", cfg.UseNUV, cfg.AutoValidateNewUsers)
	}
}

// Both switches refused each other already, but only Auto Validate said so in
// its help line. A sysop reading the NUV screen got no warning at all until the
// editor rejected the keystroke. Enforcement and warning should be symmetric,
// since the help text is the part you read before deciding.
func TestBothSwitchesWarnAboutEachOther(t *testing.T) {
	cfg := &config.ServerConfig{}

	autoValidate := fieldByLabel(t, sysFieldsLevels(cfg), "Auto Validate")
	if !strings.Contains(autoValidate.Help, "New User Voting") {
		t.Errorf("Auto Validate help does not mention New User Voting: %q", autoValidate.Help)
	}

	useNUV := fieldByLabel(t, sysFieldsNUV(cfg), "Use NUV")
	if !strings.Contains(useNUV.Help, "Auto Validate") {
		t.Errorf("Use NUV help does not mention Auto Validate: %q", useNUV.Help)
	}
}
