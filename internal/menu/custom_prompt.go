package menu

import (
	"bytes"
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
	// Trimmed, so a hand-edited " " is blank (everyone), as the docs say.
	return u != nil && checkACS(strings.TrimSpace(e.GetServerConfig().CustomPromptACS), u, s, nil, start)
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
// renderPromptText does for a sysop's: CP437, with colours expanded. text is
// already cleaned (user.CleanCustomPrompt); customPromptFor and the Konfig
// preview do that. Only colour codes, the full set the rest of the board takes
// (ansi.ColorPipeCodeLen: |00-|15, |23, |B0-|B15), and the keys of
// placeholders expand; every other pipe prints, and include tags, AT-codes and
// @RR@ are never looked for. Its characters go to CP437; one with no CP437
// form shows as "?".
//
// Each code and each placeholder's value is expanded on its own, so a value
// never runs into the code before it (|B1 then a handle starting "0" is not
// |B10), and a value is escaped first, so only its colour codes act.
func renderCustomPrompt(text string, placeholders map[string]string) []byte {
	src := []byte(toCP437Safe(text))
	keys := placeholderKeysLongestFirst(placeholders)
	var b bytes.Buffer
	b.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case src[i] != '|':
			b.WriteByte(src[i])
			i++
		case i+1 < len(src) && src[i+1] == '|':
			b.WriteByte('|')
			i += 2
		default:
			if n := ansi.ColorPipeCodeLen(string(src[i:min(i+4, len(src))])); n > 0 {
				b.Write(ansi.ReplacePipeCodes(src[i : i+n]))
				i += n
				continue
			}
			k := placeholderKeyAt(src[i:], keys)
			if k == "" {
				b.WriteByte('|')
				i++
				continue
			}
			v := toCP437Safe(ansi.DropControls(ansi.EscapeScreenPipeCodes(placeholders[k])))
			b.Write(ansi.ReplacePipeCodes([]byte(v)))
			i += len(k)
		}
	}
	return b.Bytes()
}

// placeholderKeyAt returns the first of keys that src starts with, or "".
func placeholderKeyAt(src []byte, keys []string) string {
	for _, k := range keys {
		if bytes.HasPrefix(src, []byte(k)) {
			return k
		}
	}
	return ""
}
