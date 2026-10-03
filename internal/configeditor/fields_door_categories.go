package configeditor

import (
	"fmt"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func doorSortItems() []LookupItem {
	return []LookupItem{{Value: "", Display: "Default (name, or global setting)"}, {Value: "name", Display: "Name"}, {Value: "code", Display: "Code"}, {Value: "config", Display: "Config file order"}, {Value: "manual", Display: "Manual sort order"}}
}

func nextDoorConfigOrder(doors map[string]config.DoorConfig) int {
	n := 0
	for _, d := range doors {
		n = max(n, d.ConfigOrder+1)
	}
	return n
}

func (m *Model) fieldsDoorCategory() []fieldDef {
	idx := m.recordEditIdx
	if idx < 0 || idx >= len(m.configs.Server.DoorCategories) {
		return nil
	}
	c := &m.configs.Server.DoorCategories[idx]
	return []fieldDef{
		{Label: "Code", Help: "Uppercase category code; OTHER is reserved", Type: ftString, Col: 3, Row: 1, Width: 16, Get: func() string { return c.Code }, Set: func(v string) error {
			code, err := config.NormalizeDoorCode(v)
			if err != nil {
				return err
			}
			if code == "OTHER" {
				return fmt.Errorf("OTHER is reserved")
			}
			for i, cat := range m.configs.Server.DoorCategories {
				if i != idx && cat.Code == code {
					return fmt.Errorf("category already exists")
				}
			}
			for key, d := range m.configs.Doors {
				if d.Category == c.Code {
					d.Category = code
					m.configs.Doors[key] = d
				}
			}
			c.Code = code
			return nil
		}},
		{Label: "Name", Help: "Category display name", Type: ftString, Col: 3, Row: 2, Width: 40, Get: func() string { return c.Name }, Set: func(v string) error { c.Name = v; return nil }},
		{Label: "Description", Help: "Category description (^DS)", Type: ftString, Col: 3, Row: 3, Width: 45, Get: func() string { return c.Description }, Set: func(v string) error { c.Description = v; return nil }},
		{Label: "Min Access Level", Help: "Minimum access to see the category", Type: ftInteger, Col: 3, Row: 4, Width: 3, Min: 0, Max: 255, Get: func() string { return strconv.Itoa(c.MinAccessLevel) }, Set: func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			c.MinAccessLevel = n
			return nil
		}},
		{Label: "ACS", Help: "Category access requirement", Type: ftString, Col: 3, Row: 5, Width: 40, Get: func() string { return c.ACS }, Set: func(v string) error { c.ACS = v; return nil }},
		{Label: "Sort Order", Help: "Category position (lower comes first)", Type: ftInteger, Col: 3, Row: 6, Width: 6, Min: 0, Max: 999999, Get: func() string { return strconv.Itoa(c.SortOrder) }, Set: func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			c.SortOrder = n
			return nil
		}},
		{Label: "Door Sort", Help: "Ordering inside this category", Type: ftLookup, Col: 3, Row: 7, Width: 15, Get: func() string { return c.Sort }, Set: func(v string) error { c.Sort = v; return nil }, LookupItems: doorSortItems},
	}
}
