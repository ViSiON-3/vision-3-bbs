package protocol

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxBBSNameLen is the longest BBS name, in characters, a node may subscribe
// with: the same as a message's origin_board.
const MaxBBSNameLen = MaxOriginBoardLen

// ValidNetworkName reports whether name is a well-formed network name: 1 to 32
// lowercase letters, digits, underscores and hyphens. Names go into hub URLs
// and onto BBS screens, so one that fails names no network a node can join.
func ValidNetworkName(name string) bool {
	return networkRe.MatchString(name)
}

// ValidateBBSName checks the name a node subscribes with. Every board on the
// network may show it, so it cannot hold pipes or control characters, and it is
// at most MaxBBSNameLen characters. An empty name is allowed; hubs show the
// node ID instead.
func ValidateBBSName(name string) error {
	if utf8.RuneCountInString(name) > MaxBBSNameLen {
		return fmt.Errorf("bbs_name must be at most %d characters", MaxBBSNameLen)
	}
	if hasScreenCodes(name) {
		return fmt.Errorf("bbs_name cannot contain pipes or control characters")
	}
	return nil
}

// CleanBBSName returns name as ValidateBBSName accepts it: each control
// character becomes a space, each pipe a slash (as for a name kept from another
// system), and the result is cut to MaxBBSNameLen characters. A node cleans its
// configured board name with it before subscribing, so a name with colour codes
// in it is not refused.
func CleanBBSName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '|':
			return '/'
		case r < 0x20 || (r >= 0x7F && r <= 0x9F):
			return ' '
		}
		return r
	}, name)
	return TrimField(name, MaxBBSNameLen)
}
