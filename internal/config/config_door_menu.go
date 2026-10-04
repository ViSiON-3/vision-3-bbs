package config

import (
	"fmt"
	"log/slog"
)

// DoorCategory groups generated-menu entries. Categories live in config.json;
// a door belongs to at most one category. Empty categories are not displayed.
type DoorCategory struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	MinAccessLevel int    `json:"min_access_level,omitempty"`
	ACS            string `json:"acs,omitempty"`
	SortOrder      int    `json:"sort_order,omitempty"`
	Sort           string `json:"sort,omitempty"`
	Columns        int    `json:"columns,omitempty"` // 0 inherits doorMenuColumns
}

// MaxDoorMenuColumns is the most columns the generated door menu lays out.
const MaxDoorMenuColumns = 4

// validDoorMenuColumns accepts 0 (inherit or default) through MaxDoorMenuColumns.
func validDoorMenuColumns(n int) bool {
	return n >= 0 && n <= MaxDoorMenuColumns
}

// DoorMenuColumnsFor returns the column count for a door list: the
// category's own setting when it has one, otherwise the global setting, and
// never less than one. An empty category is the category picker.
func (c *ServerConfig) DoorMenuColumnsFor(category string) int {
	n := c.DoorMenuColumns
	for _, cat := range c.DoorCategories {
		if category != "" && cat.Code == category && cat.Columns > 0 {
			n = cat.Columns
		}
	}
	return max(1, n)
}

// ValidDoorMenuSort reports whether s names a supported ordering; empty inherits the default.
func ValidDoorMenuSort(s string) bool {
	switch s {
	case "", "name", "code", "config", "manual":
		return true
	}
	return false
}

// ValidateDoorMenu also canonicalizes category codes for direct menu commands.
func (c *ServerConfig) ValidateDoorMenu() error {
	if c.DoorMenuMode != "" && c.DoorMenuMode != "lightbar" && c.DoorMenuMode != "list" {
		return fmt.Errorf("doorMenuMode must be lightbar or list")
	}
	if !ValidDoorMenuSort(c.DoorMenuSort) {
		return fmt.Errorf("invalid doorMenuSort %q", c.DoorMenuSort)
	}
	if !validDoorMenuColumns(c.DoorMenuColumns) {
		return fmt.Errorf("doorMenuColumns must be 1 to %d", MaxDoorMenuColumns)
	}
	seen := map[string]bool{}
	for i := range c.DoorCategories {
		cat := &c.DoorCategories[i]
		code, err := NormalizeDoorCode(cat.Code)
		if err != nil {
			return fmt.Errorf("door category %q: %w", cat.Code, err)
		}
		if code == "OTHER" || seen[code] {
			return fmt.Errorf("duplicate or reserved door category %q", code)
		}
		if !ValidDoorMenuSort(cat.Sort) {
			return fmt.Errorf("invalid sort for door category %q", code)
		}
		if !validDoorMenuColumns(cat.Columns) {
			return fmt.Errorf("columns for door category %q must be 1 to %d", code, MaxDoorMenuColumns)
		}
		cat.Code = code
		seen[code] = true
	}
	return nil
}

// SanitizeDoorMenu repairs invalid door-menu settings after a load, so a
// hand-edited typo cannot stop the BBS or the config editor from starting.
// Each repair is logged: a bad mode, sort or column count falls back to its
// default, and a category with a bad, reserved or repeated code is dropped,
// which moves its doors under Other. Saving still validates strictly with
// ValidateDoorMenu.
func (c *ServerConfig) SanitizeDoorMenu() {
	if c.DoorMenuMode != "" && c.DoorMenuMode != "lightbar" && c.DoorMenuMode != "list" {
		slog.Warn("invalid doorMenuMode; using lightbar", "value", c.DoorMenuMode)
		c.DoorMenuMode = ""
	}
	if !ValidDoorMenuSort(c.DoorMenuSort) {
		slog.Warn("invalid doorMenuSort; using the default", "value", c.DoorMenuSort)
		c.DoorMenuSort = ""
	}
	if !validDoorMenuColumns(c.DoorMenuColumns) {
		slog.Warn("invalid doorMenuColumns; using 1", "value", c.DoorMenuColumns, "max", MaxDoorMenuColumns)
		c.DoorMenuColumns = 0
	}
	seen := map[string]bool{}
	kept := make([]DoorCategory, 0, len(c.DoorCategories))
	for _, cat := range c.DoorCategories {
		code, err := NormalizeDoorCode(cat.Code)
		if err != nil {
			slog.Warn("dropping door category with an invalid code; its doors move to Other", "code", cat.Code, "error", err)
			continue
		}
		if code == "OTHER" || seen[code] {
			slog.Warn("dropping duplicate or reserved door category; its doors move to Other", "code", code)
			continue
		}
		if !ValidDoorMenuSort(cat.Sort) {
			slog.Warn("invalid sort for door category; using the default", "code", code, "value", cat.Sort)
			cat.Sort = ""
		}
		if !validDoorMenuColumns(cat.Columns) {
			slog.Warn("invalid columns for door category; inheriting doorMenuColumns", "code", code, "value", cat.Columns)
			cat.Columns = 0
		}
		cat.Code = code
		seen[code] = true
		kept = append(kept, cat)
	}
	c.DoorCategories = kept
}
