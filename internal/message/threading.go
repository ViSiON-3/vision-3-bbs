package message

import (
	"regexp"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

var reReplySuffix = regexp.MustCompile(` -Re: #\d+-$`)

// NormalizeThreadSubject strips Pascal-style reply markers for thread matching.
func NormalizeThreadSubject(subject string) string {
	// A subject read straight from a header may still hold what GetMessage
	// cleans, and must still match its cleaned self.
	s := strings.TrimSpace(ansi.HeaderText(subject))
	s = reReplySuffix.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	for strings.HasPrefix(strings.ToUpper(s), "RE: ") {
		s = strings.TrimSpace(s[4:])
	}
	return s
}

// ThreadKey returns a normalized, case-insensitive key for thread grouping.
func ThreadKey(subject string) string {
	return strings.ToLower(NormalizeThreadSubject(subject))
}

// SubjectsMatchThread checks if two subjects belong to the same thread.
func SubjectsMatchThread(msgSubject, searchSubject string) bool {
	return strings.EqualFold(NormalizeThreadSubject(msgSubject), NormalizeThreadSubject(searchSubject))
}
