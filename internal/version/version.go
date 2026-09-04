package version

import (
	"runtime"
	"strings"
)

// Number is the global ViSiON/3 version.
// Set this at build time with:
// -ldflags "-X github.com/ViSiON-3/vision-3-bbs/internal/version.Number=1.0.0"
var Number = "0.8.2"

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
