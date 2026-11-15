package menu

import (
	"strings"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// A caller's custom prompt (User.CustomPrompt) takes the place of a menu's
// PROMPT1 in the run loop. It is the caller's own text, so it never gets what
// a sysop's prompt does: it is never read as a !script or %file value, and of
// the codes a prompt can hold it keeps only colours and the session
// placeholders. Everything else in it prints as written.

// customPromptAllowed reports whether u may set a custom prompt, and have a
// stored one shown, under the board's customPromptACS.
func (e *MenuExecutor) customPromptAllowed(u *user.User, s ssh.Session, start time.Time) bool {
	return u != nil && checkACS(e.GetServerConfig().CustomPromptACS, u, s, nil, start)
}

// customPromptFor is the custom prompt to show u in place of PROMPT1, or ""
// for the menu's own.
func (e *MenuExecutor) customPromptFor(u *user.User, s ssh.Session, start time.Time) string {
	if u == nil {
		return ""
	}
	p := user.CleanCustomPrompt(u.CustomPrompt)
	if p == "" || !e.customPromptAllowed(u, s, start) {
		return ""
	}
	return p
}

// renderCustomPrompt turns a custom prompt into the bytes to write, as
// renderPromptText does for a sysop's: CP437, with colours expanded. Only
// colour codes (|00-|15, |B0-|B7) and the keys of placeholders expand; every
// other pipe is doubled so it prints, and include tags, AT-codes and @RR@ are
// never looked for. Control characters are dropped and the rest go to CP437
// here, before any placeholder is filled in; one with no CP437 form shows as
// "?".
func renderCustomPrompt(text string, placeholders map[string]string) []byte {
	src := []byte(toCP437Safe(user.CleanCustomPrompt(text)))
	keys := placeholderKeysLongestFirst(placeholders)
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		if src[i] != '|' {
			b.WriteByte(src[i])
			i++
			continue
		}
		if i+1 < len(src) && src[i+1] == '|' {
			b.WriteString("||")
			i += 2
			continue
		}
		if customPromptColourAt(src, i) {
			b.Write(src[i : i+3])
			i += 3
			continue
		}
		matched := false
		for _, k := range keys {
			if strings.HasPrefix(string(src[i:]), k) {
				b.WriteString(placeholders[k])
				i += len(k)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteString("||")
			i++
		}
	}
	return ansi.ReplacePipeCodes([]byte(b.String()))
}

// customPromptColourAt reports whether src[i:] starts with a colour code a
// custom prompt may use: |00 to |15, or |B0 to |B7.
func customPromptColourAt(src []byte, i int) bool {
	if i+3 > len(src) || src[i] != '|' {
		return false
	}
	a, d := src[i+1], src[i+2]
	switch a {
	case 'B':
		return d >= '0' && d <= '7'
	case '0':
		return d >= '0' && d <= '9'
	case '1':
		return d >= '0' && d <= '5'
	}
	return false
}
