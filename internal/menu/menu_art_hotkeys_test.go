package menu

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// advertisedHotkey matches a hotkey as menu art presents one: a single
// character in brackets or parentheses, e.g. "[X] Sysop Menu" or "(%) Sysop".
var advertisedHotkey = regexp.MustCompile(`[(\[]([A-Za-z0-9%/!^+*-])[)\]]`)

// conditionalMarker matches the {{ACS}}...{{/}} wrappers used for
// access-gated lines. The markers come out; the text between them stays,
// because a key advertised only to sysops still has to work for sysops.
var conditionalMarker = regexp.MustCompile(`\{\{[^}]*\}\}`)

// knownUnboundHotkeys records art that advertises a key its CFG does not bind,
// where that is understood rather than a defect to fix now. Anything not listed
// here is drift and fails the test.
var knownUnboundHotkeys = map[string]map[string]string{
	"DOORSM": {
		"B": "sample Synchronet door in the shipped art; doors are site-specific and sysops bind their own",
		"C": "sample Synchronet door in the shipped art; doors are site-specific and sysops bind their own",
	},
	// SPONSORM reads its own keys in runSponsorMenu rather than dispatching
	// through its CFG, so the CFG lists only a subset and the art is the
	// accurate record. P (reorder areas) is implemented and works.
	"SPONSORM": {
		"P": "handled directly in runSponsorMenu, not dispatched via SPONSORM.CFG",
	},
}

// TestMenuArtHotkeysAreBound guards against art and CFG drifting apart, which
// leaves a key printed on screen that does nothing when pressed. That is how
// the sysop menu became unreachable: MAIN.ANS advertised "(%) Sysop Menu" while
// MAIN.CFG bound only X and /SYSOP, so the one account able to see the option
// could not use it (issue #176).
func TestMenuArtHotkeysAreBound(t *testing.T) {
	menuSet := filepath.Join("..", "..", "menus", "v3")
	ansiDir := filepath.Join(menuSet, "ansi")
	cfgDir := filepath.Join(menuSet, "cfg")

	entries, err := os.ReadDir(ansiDir)
	if err != nil {
		t.Skipf("menu set not available: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ANS") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))

		if _, err := os.Stat(filepath.Join(cfgDir, name+".CFG")); err != nil {
			continue // art with no command set of its own
		}

		commands, err := LoadCommands(name, cfgDir)
		if err != nil {
			t.Errorf("%s.CFG: %v", name, err)
			continue
		}
		bound := make(map[string]bool)
		for _, cmd := range commands {
			for _, key := range strings.Fields(strings.ToUpper(cmd.Keys)) {
				bound[key] = true
			}
		}

		raw, err := ansi.GetAnsiFileContent(filepath.Join(ansiDir, entry.Name()))
		if err != nil {
			t.Errorf("%s.ANS: %v", name, err)
			continue
		}
		text := conditionalMarker.ReplaceAllString(string(ansi.StripAnsi(string(raw))), "")

		var missing []string
		for _, match := range advertisedHotkey.FindAllStringSubmatch(text, -1) {
			key := strings.ToUpper(match[1])
			if bound[key] {
				continue
			}
			if reason, known := knownUnboundHotkeys[name][key]; known {
				t.Logf("%s: %q advertised but unbound (known: %s)", name, key, reason)
				continue
			}
			missing = append(missing, key)
		}

		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("%s.ANS advertises %v but %s.CFG does not bind %v; "+
				"either bind the key or stop printing it in the art",
				name, missing, name, missing)
		}
	}
}
