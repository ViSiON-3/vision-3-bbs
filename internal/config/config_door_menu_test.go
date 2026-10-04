package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoorMenuConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ServerConfig
		fail bool
	}{
		{"legacy", ServerConfig{}, false},
		{"normalization", ServerConfig{DoorCategories: []DoorCategory{{Code: " games ", Sort: "manual"}}}, false},
		{"mode", ServerConfig{DoorMenuMode: "bad"}, true},
		{"sort", ServerConfig{DoorMenuSort: "bad"}, true},
		{"duplicate", ServerConfig{DoorCategories: []DoorCategory{{Code: "games"}, {Code: "GAMES"}}}, true},
		{"reserved", ServerConfig{DoorCategories: []DoorCategory{{Code: "other"}}}, true},
		{"path", ServerConfig{DoorCategories: []DoorCategory{{Code: "../GAMES"}}}, true},
		{"category sort", ServerConfig{DoorCategories: []DoorCategory{{Code: "GAMES", Sort: "bad"}}}, true},
		{"columns", ServerConfig{DoorMenuColumns: 4}, false},
		{"too many columns", ServerConfig{DoorMenuColumns: 5}, true},
		{"negative columns", ServerConfig{DoorMenuColumns: -1}, true},
		{"category columns", ServerConfig{DoorCategories: []DoorCategory{{Code: "GAMES", Columns: 5}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.ValidateDoorMenu(); (err != nil) != tc.fail {
				t.Fatalf("error %v", err)
			}
			if tc.name == "normalization" && tc.cfg.DoorCategories[0].Code != "GAMES" {
				t.Fatal(tc.cfg)
			}
		})
	}
}

func TestLoadDoorsMenuFieldsAndOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "doors.json")
	if err := os.WriteFile(p, []byte(`[{"code":"z","category":"games","hidden":true,"sort_order":5,"description":"hello"},{"code":"a"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	doors, err := LoadDoors(p)
	if err != nil {
		t.Fatal(err)
	}
	d := doors["Z"]
	if d.Category != "GAMES" || !d.Hidden || d.SortOrder != 5 || d.Description != "hello" || d.ConfigOrder != 0 || doors["A"].ConfigOrder != 1 {
		t.Fatal(doors)
	}
}

func TestDoorMenuColumnsFor(t *testing.T) {
	cfg := ServerConfig{DoorMenuColumns: 2, DoorCategories: []DoorCategory{{Code: "GAMES", Columns: 3}, {Code: "UTILS"}}}
	for category, want := range map[string]int{"": 2, "GAMES": 3, "UTILS": 2, "OTHER": 2} {
		if got := cfg.DoorMenuColumnsFor(category); got != want {
			t.Errorf("%q: got %d want %d", category, got, want)
		}
	}
	if got := (&ServerConfig{}).DoorMenuColumnsFor(""); got != 1 {
		t.Errorf("unset: got %d want 1", got)
	}
}

// TestLoadRepairsInvalidDoorMenuSettings pins that a hand-edited typo in the
// door-menu settings is repaired on load instead of stopping the BBS: bad
// values fall back to their defaults, unusable categories are dropped, and
// a door with a bad category is listed under Other.
func TestLoadRepairsInvalidDoorMenuSettings(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{"doorMenuMode":"bad","doorMenuSort":"bad","doorMenuColumns":9,
		"doorCategories":[{"code":" games ","sort":"bad","columns":7},{"code":"GAMES"},{"code":"other"},{"code":"../x"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfgJSON), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if cfg.DoorMenuMode != "" || cfg.DoorMenuSort != "" || cfg.DoorMenuColumns != 0 {
		t.Errorf("mode/sort/columns = %q/%q/%d, want defaults", cfg.DoorMenuMode, cfg.DoorMenuSort, cfg.DoorMenuColumns)
	}
	if len(cfg.DoorCategories) != 1 {
		t.Fatalf("categories = %+v, want only GAMES", cfg.DoorCategories)
	}
	if c := cfg.DoorCategories[0]; c.Code != "GAMES" || c.Sort != "" || c.Columns != 0 {
		t.Errorf("GAMES = %+v, want normalized code with default sort and columns", c)
	}
	if err := cfg.ValidateDoorMenu(); err != nil {
		t.Errorf("repaired config fails validation: %v", err)
	}

	p := filepath.Join(dir, "doors.json")
	if err := os.WriteFile(p, []byte(`[{"code":"a","category":"../bad"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	doors, err := LoadDoors(p)
	if err != nil {
		t.Fatalf("LoadDoors: %v", err)
	}
	if doors["A"].Category != "" {
		t.Errorf("door category = %q, want cleared", doors["A"].Category)
	}
}
