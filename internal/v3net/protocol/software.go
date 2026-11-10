package protocol

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Software reports.
//
// A leaf names the software it runs, and the optional V3Net features that
// software supports, in its signed subscribe request (SubscribeRequest.Software).
// The hub records the latest report for each node, with the time it arrived,
// and shows it to the hub operator (NodeInfo); nothing about it is published
// to other boards. A leaf subscribes each time it starts, so an upgrade or
// downgrade replaces the record on the next start, and a node whose software
// sends no report has its old one cleared.
//
// The report is what the node says about itself, not proof of the binary it
// runs. Capabilities decide what a node is told it supports; the version is
// only shown, and used to name the release to upgrade to. A version string
// never turns a feature on, because backports, custom builds and other BBS
// packages can differ from a stock release of the same number.

// SoftwareNameVision3 is the SoftwareReport.Name of a stock ViSiON/3 build.
// FeatureRequirement.MinVersion is in its release numbering.
const SoftwareNameVision3 = "ViSiON/3"

// Limits on a SoftwareReport. A hub ignores a report over any of them.
const (
	// MaxSoftwareNameLen is the longest software name, in characters.
	MaxSoftwareNameLen = 32
	// MaxSoftwareVersionLen is the longest version, in characters.
	MaxSoftwareVersionLen = 32
	// MaxSoftwareBuildLen is the longest build identifier, in characters.
	MaxSoftwareBuildLen = 48
	// MaxSoftwareCapabilities is the most capability names one report may
	// carry. Each is at most MaxCentralCapabilityLen characters.
	MaxSoftwareCapabilities = 32
)

// softwareVersionRe matches a version or build identifier: printable ASCII
// with no spaces or pipes, such as "0.9.7", "v1.0.0-rc.1" or "a1b2c3d-dirty".
var softwareVersionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_~-]*$`)

// SoftwareReport is the software a node says it runs.
type SoftwareReport struct {
	// Name is the software package, SoftwareNameVision3 for ViSiON/3.
	Name string `json:"name"`
	// Version is its release number, such as "0.9.7". Empty when unknown.
	Version string `json:"version,omitempty"`
	// Build identifies a development or prerelease build, such as the
	// source revision it was built from; empty for a plain release.
	Build string `json:"build,omitempty"`
	// Capabilities are the optional features (Feature* names) the software
	// supports, whether or not the board has them turned on.
	Capabilities []string `json:"capabilities,omitempty"`
}

// Validate checks a report against the limits above: a name is required,
// the version and build must look like release numbers, and capabilities
// must be well-formed feature names with none repeated. A report that fails
// is ignored as a whole, rather than half believed.
func (s *SoftwareReport) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("software name is required")
	}
	if utf8.RuneCountInString(s.Name) > MaxSoftwareNameLen {
		return fmt.Errorf("software name must be at most %d characters", MaxSoftwareNameLen)
	}
	if hasScreenCodes(s.Name) {
		return errors.New("software name cannot contain pipes or control characters")
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{
		{"version", s.Version, MaxSoftwareVersionLen},
		{"build", s.Build, MaxSoftwareBuildLen},
	} {
		if f.value == "" {
			continue
		}
		if len(f.value) > f.max {
			return fmt.Errorf("software %s must be at most %d characters", f.name, f.max)
		}
		if !softwareVersionRe.MatchString(f.value) {
			return fmt.Errorf("software %s %q is not a version", f.name, f.value)
		}
	}
	if len(s.Capabilities) > MaxSoftwareCapabilities {
		return fmt.Errorf("at most %d capabilities", MaxSoftwareCapabilities)
	}
	for i, c := range s.Capabilities {
		if !centralCapabilityRe.MatchString(c) {
			return fmt.Errorf("invalid capability %q", c)
		}
		if slices.Contains(s.Capabilities[:i], c) {
			return fmt.Errorf("capability %q repeated", c)
		}
	}
	return nil
}

// String formats the report for a screen or log: "ViSiON/3 0.9.7", with the
// build in parentheses when there is one, "ViSiON/3 0.9.7 (a1b2c3d)". A nil
// report is "".
func (s *SoftwareReport) String() string {
	if s == nil {
		return ""
	}
	out := s.Name
	if s.Version != "" {
		out += " " + s.Version
	}
	if s.Build != "" {
		out += " (" + s.Build + ")"
	}
	return out
}

// HasCapability reports whether the report names feature. A nil report has
// no capabilities.
func (s *SoftwareReport) HasCapability(feature string) bool {
	return s != nil && slices.Contains(s.Capabilities, feature)
}

// FeatureRequirement says which stock ViSiON/3 release first supported a
// feature, for upgrade advice.
type FeatureRequirement struct {
	// Feature is the Feature* name.
	Feature string
	// Title names the feature on a sysop's screen.
	Title string
	// MinVersion is the first ViSiON/3 release with the feature, such as
	// "0.9.7". Empty until a release carrying it has shipped: advice then
	// says the software lacks the feature without naming a release.
	MinVersion string
}

// FeatureRequirements lists the optional features a hub assesses each node
// for, in the order they are shown. A feature joins the list when it is
// added, with MinVersion empty; the release that ships it fills MinVersion
// in. A MinVersion is only ever set to a release that has shipped with the
// feature, never a planned one.
var FeatureRequirements = []FeatureRequirement{
	{Feature: FeatureCentralDirectory, Title: "Central directory", MinVersion: "0.9.7"},
	{Feature: FeatureCentralPresence, Title: "Who's Online"},
	{Feature: FeatureCentralLastCallers, Title: "Last Callers"},
	{Feature: FeatureCentralChat, Title: "Network chat"},
}

// FeatureMinVersion returns FeatureRequirements' MinVersion for feature: the
// first ViSiON/3 release with it, or "" when none has shipped or the feature
// is not listed.
func FeatureMinVersion(feature string) string {
	for _, r := range FeatureRequirements {
		if r.Feature == feature {
			return r.MinVersion
		}
	}
	return ""
}

// Feature support states, FeatureSupport.State.
const (
	// FeatureSupported: the node reports the feature among its
	// capabilities. Whether the board has it turned on, and whether its
	// callers take part, is the board's and its callers' choice; the hub is
	// not told and does not guess.
	FeatureSupported = "supported"
	// FeatureNeedsUpgrade: the node reported its software and the feature
	// is not among its capabilities.
	FeatureNeedsUpgrade = "unsupported"
	// FeatureUnknown: the node has not reported its software, so the hub
	// cannot tell. This is not a sign that the node is out of date.
	FeatureUnknown = "unknown"
	// FeatureHubLacks: the hub does not serve the feature on this network,
	// whatever the node runs; upgrading the node would not help.
	FeatureHubLacks = "hub"
)

// FeatureSupport is one feature's support on one node, as the hub sees it.
type FeatureSupport struct {
	Feature string `json:"feature"`
	Title   string `json:"title"`
	// State is one of FeatureSupported, FeatureNeedsUpgrade,
	// FeatureUnknown or FeatureHubLacks.
	State string `json:"state"`
	// UpgradeTo is the ViSiON/3 release with the feature, set only for
	// FeatureNeedsUpgrade on a node that reports a ViSiON/3 version before
	// it, and only when the release is known. Another package's numbering
	// means nothing here, and a node at or past the release that still
	// lacks the feature runs a build that differs from the stock one.
	UpgradeTo string `json:"upgrade_to,omitempty"`
}

// versionBefore reports whether release number v is earlier than min, both
// dotted numbers with an optional "v" prefix ("0.9.7", "v1.0"). A suffix
// after the numbers, such as "-rc.1", makes v count as before the plain
// release of the same numbers. It is false if either does not parse.
func versionBefore(v, min string) bool {
	parse := func(s string) ([]int, bool, bool) {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
		num, suffix := s, false
		if i := strings.IndexAny(s, "-+~_"); i >= 0 {
			num, suffix = s[:i], true
		}
		if num == "" {
			return nil, false, false
		}
		var parts []int
		for _, f := range strings.Split(num, ".") {
			n := 0
			if f == "" || len(f) > 6 {
				return nil, false, false
			}
			for _, r := range f {
				if r < '0' || r > '9' {
					return nil, false, false
				}
				n = n*10 + int(r-'0')
			}
			parts = append(parts, n)
		}
		return parts, suffix, true
	}
	a, aPre, ok1 := parse(v)
	b, _, ok2 := parse(min)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return aPre
}

// AssessFeatures reports, for each of FeatureRequirements, whether a node
// with report supports it on a hub that serves hubFeatures. report is nil
// for a node that sent none. Capabilities alone decide support; the version
// is used only for UpgradeTo.
func AssessFeatures(report *SoftwareReport, hubFeatures []string) []FeatureSupport {
	out := make([]FeatureSupport, 0, len(FeatureRequirements))
	for _, req := range FeatureRequirements {
		fs := FeatureSupport{Feature: req.Feature, Title: req.Title}
		switch {
		case !slices.Contains(hubFeatures, req.Feature):
			fs.State = FeatureHubLacks
		case report == nil:
			fs.State = FeatureUnknown
		case report.HasCapability(req.Feature):
			fs.State = FeatureSupported
		default:
			fs.State = FeatureNeedsUpgrade
			// A node already at or past the release that has the feature
			// runs a build that differs from it; naming a release would
			// send its sysop to one they have.
			if report.Name == SoftwareNameVision3 && req.MinVersion != "" &&
				versionBefore(report.Version, req.MinVersion) {
				fs.UpgradeTo = req.MinVersion
			}
		}
		out = append(out, fs)
	}
	return out
}
