package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// The voting booth prompt used to run past 80 columns for a sysop and wrap
// onto a second line. With every option shown and a three-digit topic count
// it must still leave room on the line for the reply.
func TestVoteBoothPromptFitsOneLine(t *testing.T) {
	const maxWidth = 70 // 80 columns less room to type
	for _, tc := range []struct {
		voted, sysop bool
	}{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		p := string(ansi.ReplacePipeCodes([]byte(voteBoothPrompt(tc.voted, tc.sysop, 999))))
		if w := ansi.VisibleLength(p); w > maxWidth {
			t.Errorf("voted=%v sysop=%v: prompt is %d columns, want <= %d: %q", tc.voted, tc.sysop, w, maxWidth, p)
		}
	}
}
