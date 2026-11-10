package protocol

import (
	"strings"
	"testing"
)

func TestSoftwareReportValidate(t *testing.T) {
	good := func() SoftwareReport {
		return SoftwareReport{Name: "ViSiON/3", Version: "0.9.7", Build: "a23f12a7c0de-dirty",
			Capabilities: []string{FeatureCentralDirectory, FeatureCentralChat}}
	}
	if r := good(); r.Validate() != nil {
		t.Fatalf("good report: %v", r.Validate())
	}
	for _, ok := range []func(*SoftwareReport){
		func(r *SoftwareReport) { r.Version, r.Build = "", "" },                 // version unknown
		func(r *SoftwareReport) { r.Version = "v1.0.0-rc.1" },                   // prerelease
		func(r *SoftwareReport) { r.Name, r.Version = "Mystic BBS", "1.12a49" }, // another package
		func(r *SoftwareReport) { r.Capabilities = nil },
	} {
		r := good()
		ok(&r)
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v, want valid", r, err)
		}
	}
	for name, bad := range map[string]func(*SoftwareReport){
		"no name":          func(r *SoftwareReport) { r.Name = " " },
		"long name":        func(r *SoftwareReport) { r.Name = strings.Repeat("n", MaxSoftwareNameLen+1) },
		"name escape":      func(r *SoftwareReport) { r.Name = "ViSiON/3\x1b[2J" },
		"name C1":          func(r *SoftwareReport) { r.Name = "ViSiON\u009b3" },
		"name pipe":        func(r *SoftwareReport) { r.Name = "|04ViSiON/3" },
		"version space":    func(r *SoftwareReport) { r.Version = "0.9 7" },
		"version escape":   func(r *SoftwareReport) { r.Version = "0.9.7\x1b[0m" },
		"version long":     func(r *SoftwareReport) { r.Version = strings.Repeat("1", MaxSoftwareVersionLen+1) },
		"build punctuated": func(r *SoftwareReport) { r.Build = "-dirty" },
		"build long":       func(r *SoftwareReport) { r.Build = strings.Repeat("a", MaxSoftwareBuildLen+1) },
		"bad capability":   func(r *SoftwareReport) { r.Capabilities = []string{"Central Chat"} },
		"repeated":         func(r *SoftwareReport) { r.Capabilities = []string{FeatureCentralChat, FeatureCentralChat} },
		"too many": func(r *SoftwareReport) {
			r.Capabilities = nil
			for i := 0; i <= MaxSoftwareCapabilities; i++ {
				r.Capabilities = append(r.Capabilities, "f"+strings.Repeat("x", i%20)+string(rune('a'+i%26))+strings.Repeat("y", i/26))
			}
		},
	} {
		r := good()
		bad(&r)
		if r.Validate() == nil {
			t.Errorf("%s: %+v validated", name, r)
		}
	}
}

func TestSoftwareReportString(t *testing.T) {
	var none *SoftwareReport
	for _, c := range []struct {
		r    *SoftwareReport
		want string
	}{
		{none, ""},
		{&SoftwareReport{Name: "ViSiON/3", Version: "0.9.7"}, "ViSiON/3 0.9.7"},
		{&SoftwareReport{Name: "ViSiON/3", Version: "0.9.7", Build: "a23f12a7c0de"}, "ViSiON/3 0.9.7 (a23f12a7c0de)"},
		{&SoftwareReport{Name: "Other"}, "Other"},
	} {
		if got := c.r.String(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
}

func TestVersionBefore(t *testing.T) {
	for _, c := range []struct {
		v, min string
		want   bool
	}{
		{"0.9.6", "0.9.7", true},
		{"0.9.7", "0.9.7", false},
		{"0.9.10", "0.9.7", false},
		{"v0.9.6", "0.9.7", true},
		{"0.9", "0.9.7", true},
		{"1.0", "0.9.7", false},
		{"0.9.7-rc.1", "0.9.7", true}, // a prerelease comes before its release
		{"0.9.8-dev", "0.9.7", false},
		{"", "0.9.7", false}, // unknown: no advice
		{"1.12a49", "0.9.7", false},
		{"0.9.7", "", false},
	} {
		if got := versionBefore(c.v, c.min); got != c.want {
			t.Errorf("versionBefore(%q, %q) = %v, want %v", c.v, c.min, got, c.want)
		}
	}
}

// withRequirements swaps FeatureRequirements for the test.
func withRequirements(t *testing.T, reqs []FeatureRequirement) {
	t.Helper()
	old := FeatureRequirements
	FeatureRequirements = reqs
	t.Cleanup(func() { FeatureRequirements = old })
}

func TestAssessFeatures(t *testing.T) {
	withRequirements(t, []FeatureRequirement{
		{Feature: "f.released", Title: "Released", MinVersion: "1.2.0"},
		{Feature: "f.unreleased", Title: "Unreleased"},
		{Feature: "f.hubless", Title: "Hubless", MinVersion: "1.0.0"},
	})
	hub := []string{"f.released", "f.unreleased"}
	state := func(r *SoftwareReport) map[string]FeatureSupport {
		got := map[string]FeatureSupport{}
		for _, f := range AssessFeatures(r, hub) {
			got[f.Feature] = f
		}
		if len(got) != 3 {
			t.Fatalf("assessed %v, want all three features", got)
		}
		return got
	}

	// No report: unknown, not out of date. The hub's own gap is still its own.
	got := state(nil)
	if got["f.released"].State != FeatureUnknown || got["f.unreleased"].State != FeatureUnknown {
		t.Errorf("no report: %+v, want unknown", got)
	}
	if got["f.hubless"].State != FeatureHubLacks {
		t.Errorf("feature the hub lacks: %+v, want %q whatever the node runs", got["f.hubless"], FeatureHubLacks)
	}

	// An older stock release is told which release has the feature; an
	// unreleased feature names none.
	got = state(&SoftwareReport{Name: SoftwareNameVision3, Version: "1.1.0"})
	if f := got["f.released"]; f.State != FeatureNeedsUpgrade || f.UpgradeTo != "1.2.0" {
		t.Errorf("older release: %+v, want an upgrade to 1.2.0", f)
	}
	if f := got["f.unreleased"]; f.State != FeatureNeedsUpgrade || f.UpgradeTo != "" {
		t.Errorf("unreleased feature: %+v, want unsupported naming no release", f)
	}
	if f := got["f.hubless"]; f.State != FeatureHubLacks {
		t.Errorf("hub's gap blamed on the node: %+v", f)
	}

	// Capabilities decide, not the version: a new version without the
	// capability is unsupported but not sent to an older release, and an
	// old version with it is supported.
	got = state(&SoftwareReport{Name: SoftwareNameVision3, Version: "9.0.0"})
	if f := got["f.released"]; f.State != FeatureNeedsUpgrade || f.UpgradeTo != "" {
		t.Errorf("custom build past the release: %+v, want unsupported naming no release", f)
	}
	got = state(&SoftwareReport{Name: SoftwareNameVision3, Version: "0.1", Capabilities: []string{"f.released"}})
	if f := got["f.released"]; f.State != FeatureSupported {
		t.Errorf("old version with the capability: %+v, want supported", f)
	}

	// Another package's numbering means nothing for ViSiON/3 releases.
	got = state(&SoftwareReport{Name: "Other BBS", Version: "1.0"})
	if f := got["f.released"]; f.State != FeatureNeedsUpgrade || f.UpgradeTo != "" {
		t.Errorf("other software: %+v, want unsupported naming no release", f)
	}
	// A development build with no parseable version gets no release named.
	got = state(&SoftwareReport{Name: SoftwareNameVision3, Build: "a23f12a7c0de"})
	if f := got["f.released"]; f.UpgradeTo != "" {
		t.Errorf("unversioned build: %+v, want no release named", f)
	}
}

// Every feature a hub can serve or a leaf can support is in the table, so
// none goes unassessed.
func TestFeatureRequirementsCoverFeatures(t *testing.T) {
	for _, f := range []string{FeatureCentralDirectory, FeatureCentralPresence, FeatureCentralLastCallers, FeatureCentralChat} {
		found := false
		for _, r := range FeatureRequirements {
			if r.Feature == f {
				found = true
				if r.Title == "" {
					t.Errorf("%s has no title", f)
				}
				if r.MinVersion != "" && !softwareVersionRe.MatchString(r.MinVersion) {
					t.Errorf("%s: MinVersion %q is not a version", f, r.MinVersion)
				}
			}
		}
		if !found {
			t.Errorf("%s is missing from FeatureRequirements", f)
		}
	}
}

func TestFeatureMinVersion(t *testing.T) {
	withRequirements(t, []FeatureRequirement{{Feature: "f.a", MinVersion: "1.2.0"}, {Feature: "f.b"}})
	if got := FeatureMinVersion("f.a"); got != "1.2.0" {
		t.Errorf("f.a = %q", got)
	}
	if got := FeatureMinVersion("f.b") + FeatureMinVersion("f.none"); got != "" {
		t.Errorf("unreleased or unknown feature named %q", got)
	}
}
