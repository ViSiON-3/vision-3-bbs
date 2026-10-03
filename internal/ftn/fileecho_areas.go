package ftn

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

// FileEchoAreaOptions configures the file areas PlanFileEchoAreas creates.
type FileEchoAreaOptions struct {
	Network      string // ftn.json network key the echoes come from
	TagPrefix    string // prefix for the new areas' tags
	ConferenceID int    // conference the new areas belong to (0 = ungrouped)
	ACSList      string
	ACSDownload  string
	ACSUpload    string
}

// PlanFileEchoAreas returns the file areas to add for the echoes that no area
// is linked to yet, and the existing areas that already carry one. It only
// plans: callers append added to their file areas and save them. A new
// area's tag is the prefix and echo tag, made unique against every tag in
// use; its directory is <network>/<echo tag>, lower-cased.
func PlanFileEchoAreas(existing []file.FileArea, echoes []EchoArea, opt FileEchoAreaOptions) (added, linked []file.FileArea) {
	usedTags := map[string]bool{}
	usedPaths := map[string]bool{}
	carried := map[string]file.FileArea{}
	maxID := 0
	for _, a := range existing {
		usedTags[strings.ToUpper(a.Tag)] = true
		usedPaths[strings.ToLower(filepath.ToSlash(filepath.Clean(a.Path)))] = true
		if a.ID > maxID {
			maxID = a.ID
		}
		if a.IsFileEcho() && strings.EqualFold(a.Network, opt.Network) {
			carried[strings.ToUpper(a.FileEcho)] = a
		}
	}

	for _, e := range echoes {
		echo := strings.ToUpper(e.Tag)
		if a, ok := carried[echo]; ok {
			linked = append(linked, a)
			continue
		}
		// Parsed tags are never dots alone, but this is also the last
		// check before a tag becomes a directory.
		if file.CheckFilename(e.Tag) != nil || strings.Trim(e.Tag, ".") == "" {
			continue
		}
		tag := uniqueAreaName(strings.ToUpper(opt.TagPrefix+e.Tag), usedTags, "_")
		usedTags[tag] = true
		path := uniqueAreaName(strings.ToLower(opt.Network+"/"+e.Tag), usedPaths, "_")
		usedPaths[path] = true

		name := e.Description
		if name == "" {
			name = e.Tag
		}
		maxID++
		added = append(added, file.FileArea{
			ID:           maxID,
			Tag:          tag,
			Name:         name,
			Description:  fmt.Sprintf("%s file echo %s", opt.Network, e.Tag),
			Path:         path,
			ACSList:      opt.ACSList,
			ACSUpload:    opt.ACSUpload,
			ACSDownload:  opt.ACSDownload,
			ConferenceID: opt.ConferenceID,
			Network:      opt.Network,
			FileEcho:     echo,
		})
		carried[echo] = added[len(added)-1]
	}
	return added, linked
}

// uniqueAreaName returns base, or base with a numeric suffix, whichever is not in
// used. Keys of used are compared as stored, so callers normalise case first.
func uniqueAreaName(base string, used map[string]bool, sep string) string {
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%s%d", base, sep, i)
		if !used[candidate] {
			return candidate
		}
	}
}
