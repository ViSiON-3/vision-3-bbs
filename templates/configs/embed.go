// Package configtemplates embeds the shipped configuration templates so tools
// can reach the factory defaults without depending on a file path relative to
// the current directory.
//
// The Go file lives alongside the JSON rather than the JSON being copied into
// a package directory, so there is exactly one canonical copy of each template:
// the same files the install scripts write into a new BBS.
package configtemplates

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed *.json
var FS embed.FS

// StringDefaults returns the factory values for strings.json.
func StringDefaults() (map[string]string, error) {
	data, err := FS.ReadFile("strings.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded strings.json: %w", err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("parsing embedded strings.json: %w", err)
	}
	return values, nil
}
