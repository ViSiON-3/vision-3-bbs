package user

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// CustomPromptMaxLen is the longest custom prompt a caller can keep, in
// characters.
const CustomPromptMaxLen = 80

// CleanCustomPrompt returns s as a custom prompt is stored: one line, without
// control characters or surrounding spaces, and at most CustomPromptMaxLen
// characters.
func CleanCustomPrompt(s string) string {
	s = strings.TrimSpace(ansi.DropControls(s))
	return strings.TrimSpace(ansi.TruncateRunes(s, CustomPromptMaxLen, ""))
}
