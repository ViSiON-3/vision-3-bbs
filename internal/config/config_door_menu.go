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
		cat.Code = code
		seen[code] = true
	}
	return nil
}
