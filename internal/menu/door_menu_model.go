package menu

import (
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

type doorMenuEntry struct {
	code, name, description string
	category                bool
	door                    config.DoorConfig
}

// buildDoorMenuEntries is shared by both input modes. Unknown category names
// join Other; known but inaccessible categories never leak through Other.
func buildDoorMenuEntries(doors map[string]config.DoorConfig, cfg config.ServerConfig, category string, level int, allows func(string) bool, otherName string) []doorMenuEntry {
	cats := map[string]config.DoorCategory{}
	for _, cat := range cfg.DoorCategories {
		cats[cat.Code] = cat
	}
	groups := map[string][]doorMenuEntry{}
	for code, d := range doors {
		if d.Hidden || d.MinAccessLevel > level {
			continue
		}
		group := d.Category
		if cat, ok := cats[group]; ok {
			if cat.MinAccessLevel > level || !allows(cat.ACS) {
				continue
			}
		} else {
			group = "OTHER"
		}
		groups[group] = append(groups[group], doorMenuEntry{code: code, name: d.Name, description: d.Description, door: d})
	}
	var entries []doorMenuEntry
	if len(cats) > 0 && category == "" {
		for code, cat := range cats {
			if len(groups[code]) > 0 {
				entries = append(entries, doorMenuEntry{code: code, name: cat.Name, description: cat.Description, category: true})
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			a, b := cats[entries[i].code], cats[entries[j].code]
			if a.SortOrder != b.SortOrder {
				return a.SortOrder < b.SortOrder
			}
			if !strings.EqualFold(a.Name, b.Name) {
				return strings.ToUpper(a.Name) < strings.ToUpper(b.Name)
			}
			return a.Code < b.Code
		})
		if len(groups["OTHER"]) > 0 {
			entries = append(entries, doorMenuEntry{code: "OTHER", name: otherName, category: true})
		}
		return entries
	}
	if category != "" {
		entries = groups[category]
	} else {
		for _, group := range groups {
			entries = append(entries, group...)
		}
	}
	order := cfg.DoorMenuSort
	if cat, ok := cats[category]; ok && cat.Sort != "" {
		order = cat.Sort
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		switch order {
		case "manual":
			if a.door.SortOrder != b.door.SortOrder {
				return a.door.SortOrder < b.door.SortOrder
			}
		case "config":
			if a.door.ConfigOrder != b.door.ConfigOrder {
				return a.door.ConfigOrder < b.door.ConfigOrder
			}
		case "code":
			return a.code < b.code
		default:
			if !strings.EqualFold(a.name, b.name) {
				return strings.ToUpper(a.name) < strings.ToUpper(b.name)
			}
		}
		return a.code < b.code
	})
	return entries
}

func doorMenuPageSize(height, top, bottom, prompt, rowHeight int) int {
	if rowHeight < 1 {
		rowHeight = 1
	}
	// One spare line keeps the last prompt off the scrolling bottom row.
	return max(1, (height-top-bottom-prompt-1)/rowHeight)
}

// doorMenuMinCellWidth is the narrowest column worth drawing. A terminal too
// narrow for the configured columns gets as many as fit, and at least one.
const doorMenuMinCellWidth = 20

// doorMenuFitColumns returns how many of cols columns fit in width, leaving
// the last column free as the rest of the layout does.
func doorMenuFitColumns(cols, width int) int {
	return max(1, min(cols, (width-1)/doorMenuMinCellWidth))
}
