package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// The bug: the version was written into strings.json, and nothing rewrites a
// config file on upgrade, so a 0.8.2 build advertised v0.1.0 (Pre-Alpha).
func TestVersionStringReportsTheRunningVersion(t *testing.T) {
	got := renderVersionString(upgradeVersionTemplate(defaultVersionTemplate))

	if !strings.Contains(got, version.Display()) {
		t.Errorf("banner %q does not contain the running version %q", got, version.Display())
	}
	if strings.Contains(got, "%s") {
		t.Errorf("the verb was left unexpanded: %q", got)
	}
}

// An install carrying the old baked-in banner has never been edited, so it is
// upgraded rather than left advertising a version that is years out of date.
func TestSupersededTemplateIsUpgraded(t *testing.T) {
	for _, old := range supersededVersionTemplates {
		got := renderVersionString(upgradeVersionTemplate(old))

		if strings.Contains(got, "v0.1.0") || strings.Contains(got, "Pre-Alpha") {
			t.Errorf("the stale banner survived: %q", got)
		}
		if !strings.Contains(got, version.Display()) {
			t.Errorf("banner %q does not contain the running version %q", got, version.Display())
		}
	}
}

// A sysop's own banner is theirs. Upgrading must not overwrite it, even though
// the shipped default changed.
func TestCustomTemplateIsLeftAlone(t *testing.T) {
	custom := "|13The Dungeon |08:: |15%s|07"

	got := renderVersionString(upgradeVersionTemplate(custom))

	if !strings.Contains(got, "The Dungeon") {
		t.Errorf("a customised banner was overwritten: %q", got)
	}
	if !strings.Contains(got, version.Display()) {
		t.Errorf("banner %q does not contain the running version", got)
	}
}

// An empty value falls back to the shipped default rather than printing
// nothing at all.
func TestEmptyTemplateFallsBackToTheDefault(t *testing.T) {
	got := renderVersionString(upgradeVersionTemplate("   "))

	if !strings.Contains(got, version.Display()) {
		t.Errorf("an empty template printed %q, with no version", got)
	}
}

// Sprintf with the wrong arity does not fail, it renders %!s(MISSING) into the
// middle of the banner. A template that cannot be filled is printed as-is.
func TestMalformedTemplatesNeverRenderSprintfNoise(t *testing.T) {
	for _, tmpl := range []string{
		"|15No verb here at all|07",
		"|15Two %s verbs %s|07",
		"|15Trailing percent %|07",
		"|15Escaped 100%% and one %s|07",
		"|15100% Go|07",
		"|15Percent at the end %|07",
	} {
		got := renderVersionString(tmpl)

		for _, noise := range []string{"%!", "(MISSING)", "(EXTRA"} {
			if strings.Contains(got, noise) {
				t.Errorf("template %q rendered Sprintf noise: %q", tmpl, got)
			}
		}
	}
}

// The renderer has to see past %%, which is a literal percent and not a verb,
// and has to tell a verb that takes a string from one that does not: "100% Go"
// carries one verb by a naive count, but it is %<space>G, wants a number, and
// filling it mangles the banner.
func TestOnlyASingleStringVerbIsFilled(t *testing.T) {
	cases := []struct {
		template string
		filled   bool // whether the version is substituted in
	}{
		{"%s", true},
		{"100%% pure %s", true},
		{"%%%s", true},
		{"|15Escaped 100%% and one %s|07", true},
		// Padded and general forms are filled too. The original check looked
		// only at the byte after %, so it printed a sysop's padded banner
		// literally; the editor calls padding cosmetic, so the runtime must
		// agree with it rather than refuse the same edit.
		{"|15ViSiON/3 - %-20s|07", true},
		{"|15ViSiON/3 - %20s|07", true},
		{"|15ViSiON/3 - %v|07", true},
		{"|15ViSiON/3 - %q|07", true},
		{"", false},
		{"no verbs", false},
		{"100%% pure", false},
		{"%d and %s", false},
		{"trailing %", false},
		{"%%", false},
		// The case that caught a bug here: the verb is %<space>G, not %s.
		{"100% Go", false},
		{"|15Trailing percent %|07", false},
		// One argument, but not one a version string can satisfy.
		{"|15ViSiON/3 - %d|07", false},
		{"|15ViSiON/3 - %f|07", false},
		{"|15ViSiON/3 - %t|07", false},
	}
	for _, c := range cases {
		got := renderVersionString(c.template)
		filled := got != c.template
		if filled != c.filled {
			t.Errorf("renderVersionString(%q) = %q; filled = %v, want %v",
				c.template, got, filled, c.filled)
		}
		if strings.Contains(got, "%!") {
			t.Errorf("renderVersionString(%q) rendered Sprintf noise: %q", c.template, got)
		}
	}
}

// Whitespace is not incidental in a banner: leading spaces indent it on screen.
// A value differing from a shipped default by only whitespace is therefore an
// edit, and must not be overwritten by the upgrade.
func TestSupersededMatchIsExactNotTrimmed(t *testing.T) {
	for _, old := range supersededVersionTemplates {
		for _, edited := range []string{"  " + old, old + "  ", "\t" + old} {
			if got := upgradeVersionTemplate(edited); got != edited {
				t.Errorf("an indented banner was overwritten:\n  had  %q\n  got  %q", edited, got)
			}
		}
	}
}

// And such a sysop is told why no version shows, rather than left guessing.
func TestAnEditedStaleBannerStillWarns(t *testing.T) {
	edited := "  " + supersededVersionTemplates[0]

	got := renderVersionString(upgradeVersionTemplate(edited))

	if got != edited {
		t.Errorf("the sysop's banner was altered: %q", got)
	}
	if strings.Contains(got, "%!") {
		t.Errorf("Sprintf noise rendered into an unfillable banner: %q", got)
	}
}
