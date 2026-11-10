// Package version reports the ViSiON/3 release number, stamped at build time
// through Number, and the host platform name shown alongside it.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Number is the global ViSiON/3 version.
// Set this at build time with:
// -ldflags "-X github.com/ViSiON-3/vision-3-bbs/internal/version.Number=1.0.0"
var Number = "0.9.7"

// Display returns the version prefixed with "v", e.g. "v0.8.0". A Number that
// already carries the prefix (a build stamped from a git tag) is left alone.
func Display() string {
	n := strings.TrimSpace(Number)
	if n == "" {
		return ""
	}
	if n[0] == 'v' || n[0] == 'V' {
		return n
	}
	return "v" + n
}

// Platform returns a human-readable name for the running operating system,
// e.g. "macOS" rather than Go's "darwin". Unknown platforms fall back to the
// raw GOOS value.
func Platform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	case "dragonfly":
		return "DragonFly"
	case "solaris":
		return "Solaris"
	case "illumos":
		return "illumos"
	case "aix":
		return "AIX"
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	case "plan9":
		return "Plan9"
	default:
		return runtime.GOOS
	}
}

// Build identifies the source this binary was built from: the first 12
// characters of the git revision Go records when it builds from a checkout,
// with "-dirty" added when the checkout had uncommitted changes, such as
// "a23f12a7c0de-dirty". Number alone cannot tell two builds between
// releases apart. It is "" when the build recorded no revision (built
// outside a git checkout, or with -buildvcs=false).
func Build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
