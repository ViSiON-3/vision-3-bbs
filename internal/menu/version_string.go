package menu

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/formatspec"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// renderVersionString fills the sysop's version template with the running
// version.
//
// The template lives in strings.json so a sysop can brand the line, and marks
// where the version goes with a single %s. The value comes from
// internal/version, which is what release builds stamp -- writing the number
// into strings.json instead is how it came to advertise v0.1.0 (Pre-Alpha) from
// a 0.8.2 build, since nothing updates a config file on upgrade.
//
// A template carrying anything other than exactly one verb is printed as-is
// rather than fed to Sprintf. Sprintf with the wrong arity does not fail, it
// renders %!s(MISSING) or (EXTRA ...) into the middle of the sysop's banner,
// which is worse than leaving their text alone and saying why in the log.
func renderVersionString(template string) string {
	spec, err := formatspec.Parse(template)
	switch {
	case err != nil:
		slog.Warn("execVersionString is not a valid format string; printing it unchanged to avoid a malformed banner",
			"error", err, "template", template)
	case spec.Arity() == 1 && spec.Args[0].Accepts(formatspec.KindString):
		return fmt.Sprintf(template, version.Display())
	case spec.Arity() == 0:
		slog.Warn("execVersionString has no %s, so the version cannot be shown; add one where the version should appear",
			"template", template, "version", version.Display())
	default:
		slog.Warn("execVersionString must contain exactly one %s and nothing else; printing it unchanged to avoid a malformed banner",
			"signature", spec.String(), "template", template)
	}
	return template
}

// defaultVersionTemplate is the shipped banner. Kept here beside the renderer
// so the two stay in step.
const defaultVersionTemplate = "|15ViSiON/3 Go Edition - %s|07"

// supersededVersionTemplates are banners previously shipped with the version
// baked in. An install still carrying one has never been edited by its sysop,
// so replacing it restores a correct version without overwriting anyone's
// customisation. Add to this list rather than editing it when the default
// changes again.
var supersededVersionTemplates = []string{
	"|15ViSiON/3 Go Edition - v0.1.0 (Pre-Alpha)|07",
}

// upgradeVersionTemplate returns the template to use, replacing a superseded
// shipped default with the current one and leaving anything else untouched.
func upgradeVersionTemplate(configured string) string {
	// Nothing configured at all: the command would print a blank line, so fall
	// back rather than show the sysop nothing.
	if strings.TrimSpace(configured) == "" {
		return defaultVersionTemplate
	}
	// Match exactly, without trimming. Whitespace is not incidental in a banner
	// -- leading spaces indent it on screen -- so a value that differs from a
	// shipped default by so much as a space is an edit, and edits are the
	// sysop's. Trimming here would quietly overwrite one.
	//
	// The cost is that such a sysop keeps a banner with the old version baked
	// in. They are not left guessing: it has no %s, so renderVersionString logs
	// exactly why no version appears and what to add.
	for _, old := range supersededVersionTemplates {
		if configured == old {
			return defaultVersionTemplate
		}
	}
	return configured
}
