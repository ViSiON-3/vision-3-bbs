package util

import "strings"

// Plural is one when n is 1, else many: Plural(n, "vote", "votes").
func Plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// AndList joins words as prose: "a", "a and b", "a, b and c".
func AndList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
