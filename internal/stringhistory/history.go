// Package stringhistory knows every value each strings.json key has shipped
// with. A full copy of the strings from an older release holds those values
// for every string its sysop never changed; telling them apart from real
// customisations is what lets an upgrade trim such a file down to the
// sysop's own wording (#1100).
//
// history.json is generated from the git history of the shipped template by
// `go generate ./internal/stringhistory`, and committed, since a release
// build has no git history to read. Regenerate it whenever
// templates/configs/strings.json changes; TestHistoryHasCurrentTemplate
// fails until you do.
package stringhistory

//go:generate go run ./gen -out history.json

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"slices"
	"sync"
)

//go:embed history.json
var historyJSON []byte

var history = sync.OnceValue(func() map[string][]string {
	var h map[string][]string
	if err := json.Unmarshal(historyJSON, &h); err != nil {
		panic(fmt.Sprintf("stringhistory: embedded history.json: %v", err))
	}
	return h
})

// Shipped reports whether value is one key has shipped with, in this
// release or an earlier one.
func Shipped(key, value string) bool {
	return slices.Contains(history()[key], value)
}

// Hash is a short, stable fingerprint of a shipped value, recorded beside a
// customised string so a later release can tell its default has changed.
func Hash(value string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%016x", h.Sum64())
}

// ByHash returns the value key shipped with whose Hash is hash.
func ByHash(key, hash string) (string, bool) {
	for _, v := range history()[key] {
		if Hash(v) == hash {
			return v, true
		}
	}
	return "", false
}
