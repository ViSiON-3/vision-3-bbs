package scripting

import (
	"fmt"
	"unicode/utf8"
)

// Limits on what a V3 script passes to the BBS and gets back from it. A call
// that would go over one throws an error naming the call, the size and the
// limit (see limitError); nothing is cut short without the script knowing.
const (
	// maxFSFileBytes caps one v3.fs.read, one v3.fs.write or v3.fs.append,
	// and the size v3.fs.append may grow a file to. It matches the cap on an
	// archive member a script may read.
	maxFSFileBytes = 4 << 20

	// maxFSDirEntries caps the entries v3.fs.list returns for one directory.
	maxFSDirEntries = 10000

	// maxDataStoreBytes caps a script's v3.data store as saved on disk. A
	// change that would leave it larger is refused and the previous store is
	// kept. A store already over the cap (from before the cap, or edited by
	// hand) can still be read and made smaller.
	maxDataStoreBytes = 4 << 20

	// maxMessageBodyBytes caps the body of a message a script posts.
	maxMessageBodyBytes = 64 << 10

	// maxSubjectChars and maxToChars cap a posted message's subject and To,
	// at the lengths of the FidoNet message header (see internal/ftn), so a
	// post can travel to an echo or netmail area without being cut.
	maxSubjectChars = 72
	maxToChars      = 36

	// maxFTNAddressChars is the longest FTN address a netmail To can carry
	// after its "@": "32767:32767/32767.32767".
	maxFTNAddressChars = 23

	// maxPageChars caps a page sent with v3.nodes.send or v3.nodes.broadcast.
	maxPageChars = 255

	// maxPadWidth caps the width given to v3.util.padLeft, padRight and
	// center.
	maxPadWidth = 4096
)

// limitError is the error for a call that would go over a limit: call is the
// script-facing name ("v3.fs.write"), what is the thing measured ("content",
// "subject"), and size and limit are in unit ("bytes" or "characters").
func limitError(call, what string, size, limit int, unit string) error {
	if unit == "bytes" {
		got := formatBytes(size)
		if got == formatBytes(limit) { // just over: rounding would hide it
			got = fmt.Sprintf("%d bytes", size)
		}
		return fmt.Errorf("%s: %s is %s, over the %s limit", call, what, got, formatBytes(limit))
	}
	return fmt.Errorf("%s: %s is %d %s, over the %d limit", call, what, size, unit, limit)
}

// checkChars returns a limitError when s is longer than limit characters.
func checkChars(call, what, s string, limit int) error {
	if n := utf8.RuneCountInString(s); n > limit {
		return limitError(call, what, n, limit, "characters")
	}
	return nil
}

// checkBytes returns a limitError when n bytes is over limit.
func checkBytes(call, what string, n, limit int) error {
	if n > limit {
		return limitError(call, what, n, limit, "bytes")
	}
	return nil
}

// formatBytes writes n as a size a sysop reads at a glance: "512 bytes",
// "64 KB", "4 MB", "5.2 MB".
func formatBytes(n int) string {
	const kb, mb = 1 << 10, 1 << 20
	trim := func(f float64, unit string) string {
		s := fmt.Sprintf("%.1f", f)
		if len(s) > 2 && s[len(s)-2:] == ".0" {
			s = s[:len(s)-2]
		}
		return s + " " + unit
	}
	switch {
	case n >= mb:
		return trim(float64(n)/mb, "MB")
	case n >= kb:
		return trim(float64(n)/kb, "KB")
	}
	return fmt.Sprintf("%d bytes", n)
}
