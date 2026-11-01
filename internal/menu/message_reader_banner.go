package menu

import (
	"regexp"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// builtinQuoteBanners are the editor's own "Said" / "Done" defaults, so posts
// written before a board customised quoteTop/quoteBottom still match.
var builtinQuoteBanners = []string{"|08[|15^N |07Said|08]", "|08[|15^N |07Done|08]|07"}

// pipeColourRe matches a two-digit pipe colour code such as |08.
var pipeColourRe = regexp.MustCompile(`\|[0-9]{2}`)

// quoteCodeRe matches the editor's quote substitutions (^N author, ^I
// initials, ^T title, ^F who to, ^D date, ^W time).
var quoteCodeRe = regexp.MustCompile(`\^[NnIiTtFfDdWw]`)

// quoteBannerMatcher reports whether a message line is a quote banner the
// editor wrote, for the board's configured templates and the built-in ones.
// A nil matcher matches nothing.
type quoteBannerMatcher []*regexp.Regexp

// newQuoteBannerMatcher compiles the board's quote banner templates. A
// template with fewer than two literal characters, such as a bare "^N", is
// skipped: it would match ordinary lines.
func newQuoteBannerMatcher(st *config.StringsConfig) quoteBannerMatcher {
	templates := append([]string(nil), builtinQuoteBanners...)
	if st != nil {
		templates = append(templates, st.QuoteTop, st.QuoteBottom, st.QuoteTopNet, st.QuoteBottomNet)
	}
	var m quoteBannerMatcher
	seen := map[string]bool{}
	for _, tmpl := range templates {
		plain := strings.TrimSpace(pipeColourRe.ReplaceAllString(tmpl, ""))
		literal := quoteCodeRe.Split(plain, -1)
		if len(strings.TrimSpace(strings.Join(literal, ""))) < 2 {
			continue
		}
		for i, part := range literal {
			literal[i] = regexp.QuoteMeta(part)
		}
		expr := "^" + strings.Join(literal, ".+?") + "$"
		if seen[expr] {
			continue
		}
		seen[expr] = true
		if re, err := regexp.Compile(expr); err == nil {
			m = append(m, re)
		}
	}
	return m
}

// match reports whether line, once its colour codes are removed, is a banner.
func (m quoteBannerMatcher) match(line string) bool {
	if len(m) == 0 {
		return false
	}
	plain := strings.TrimSpace(pipeColourRe.ReplaceAllString(stripEscapes(line), ""))
	if plain == "" {
		return false
	}
	for _, re := range m {
		if re.MatchString(plain) {
			return true
		}
	}
	return false
}
