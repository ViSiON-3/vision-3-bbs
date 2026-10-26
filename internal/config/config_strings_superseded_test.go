package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// TestStringSupersededHasCurrentDefault guards the table: a superseded key
// needs a fallback to replace the old text with, that fallback must be what
// the template ships (or ./strings would call the upgraded value an edit),
// and an old text equal to the current one would be pointless.
func TestStringSupersededHasCurrentDefault(t *testing.T) {
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatal(err)
	}
	for key, olds := range StringSuperseded {
		current, ok := StringFallbacks[key]
		if !ok || current == "" {
			t.Errorf("%s is superseded but has no fallback", key)
			continue
		}
		if shipped[key] != current {
			t.Errorf("%s: template %q differs from fallback %q", key, shipped[key], current)
		}
		for _, old := range olds {
			if old == current {
				t.Errorf("%s lists its current text as superseded", key)
			}
		}
	}
}

// TestLoadStringsReplacesSuperseded checks an unedited old value is replaced
// with the current default while an edited one is kept.
func TestLoadStringsReplacesSuperseded(t *testing.T) {
	old := StringSuperseded["konfigAutoSigChoice"][0]
	for _, tc := range []struct {
		name, value, want string
	}{
		{"unedited", old, StringFallbacks["konfigAutoSigChoice"]},
		{"edited", old + " ", old + " "},
		{"custom", "Sig: E/D", "Sig: E/D"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			data, _ := json.Marshal(map[string]string{"konfigAutoSigChoice": tc.value})
			if err := os.WriteFile(filepath.Join(dir, "strings.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := LoadStrings(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.KonfigAutoSigChoice != tc.want {
				t.Errorf("KonfigAutoSigChoice = %q, want %q", c.KonfigAutoSigChoice, tc.want)
			}
		})
	}
}
