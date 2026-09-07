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

// formatVerbCounts has to see past %%, which renders as a literal percent and
// is not a verb, and has to tell %s from any other verb: "100% Go" carries one
// verb by a naive count, but it is %<space> and filling it mangles the banner.
func TestFormatVerbCounts(t *testing.T) {
	cases := []struct {
		template   string
		total, str int
	}{
		{"", 0, 0},
		{"no verbs", 0, 0},
		{"%s", 1, 1},
		{"%d and %s", 2, 1},
		{"100%% pure", 0, 0},
		{"100%% pure %s", 1, 1},
		{"%%%s", 1, 1},
		{"trailing %", 0, 0},
		{"%%", 0, 0},
		// The case that caught a bug here: the verb is %<space>, not %s.
		{"100% Go", 1, 0},
		{"|15Trailing percent %|07", 1, 0},
	}
	for _, c := range cases {
		total, str := formatVerbCounts(c.template)
		if total != c.total || str != c.str {
			t.Errorf("formatVerbCounts(%q) = (%d, %d), want (%d, %d)",
				c.template, total, str, c.total, c.str)
		}
	}
}
