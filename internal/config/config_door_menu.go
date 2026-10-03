package config

import "fmt"

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
