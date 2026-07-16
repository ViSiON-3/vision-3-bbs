package conference

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConferences writes a conferences.json fixture into dir.
func writeConferences(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, conferencesFile), []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestNewConferenceManager_MissingFile(t *testing.T) {
	cm, err := NewConferenceManager(t.TempDir())
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if got := cm.ListConferences(); len(got) != 0 {
		t.Errorf("want 0 conferences, got %d", len(got))
	}
}

func TestNewConferenceManager_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeConferences(t, dir, "")
	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatalf("empty file should not error: %v", err)
	}
	if got := cm.ListConferences(); len(got) != 0 {
		t.Errorf("want 0 conferences, got %d", len(got))
	}
}

func TestNewConferenceManager_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeConferences(t, dir, "{not json")
	if _, err := NewConferenceManager(dir); err == nil {
		t.Fatal("invalid JSON should return an error")
	}
}

func TestConferenceManager_Lookups(t *testing.T) {
	dir := t.TempDir()
	writeConferences(t, dir, `[
		{"id": 1, "position": 2, "tag": "LOCAL", "name": "Local"},
		{"id": 2, "position": 1, "tag": "FSXNET", "name": "fsxNet"},
		null,
		{"id": 1, "position": 3, "tag": "DUPID", "name": "Duplicate ID"},
		{"id": 3, "position": 4, "tag": "LOCAL", "name": "Duplicate Tag"}
	]`)
	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}

	// Duplicates and nulls are skipped: only 2 loaded.
	list := cm.ListConferences()
	if len(list) != 2 {
		t.Fatalf("want 2 conferences, got %d", len(list))
	}
	// Sorted by Position: FSXNET (pos 1) before LOCAL (pos 2).
	if list[0].Tag != "FSXNET" || list[1].Tag != "LOCAL" {
		t.Errorf("list order = %s, %s; want FSXNET, LOCAL", list[0].Tag, list[1].Tag)
	}

	if c, ok := cm.GetByID(1); !ok || c.Name != "Local" {
		t.Errorf("GetByID(1) = %+v, %v; want Local", c, ok)
	}
	if _, ok := cm.GetByID(99); ok {
		t.Error("GetByID(99) should not exist")
	}
	if c, ok := cm.GetByTag("FSXNET"); !ok || c.ID != 2 {
		t.Errorf("GetByTag(FSXNET) = %+v, %v; want ID 2", c, ok)
	}
	if _, ok := cm.GetByTag("NOPE"); ok {
		t.Error("GetByTag(NOPE) should not exist")
	}
}

func TestConferenceManager_PositionMigration(t *testing.T) {
	dir := t.TempDir()
	// Conference 5 has an explicit position; 2 and 7 need migration.
	writeConferences(t, dir, `[
		{"id": 7, "position": 0, "tag": "C7", "name": "Seven"},
		{"id": 5, "position": 3, "tag": "C5", "name": "Five"},
		{"id": 2, "position": 0, "tag": "C2", "name": "Two"}
	]`)
	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}

	// Unset positions are assigned after the current max (3), ordered by ID:
	// C2 -> 4, C7 -> 5.
	c2, _ := cm.GetByID(2)
	c7, _ := cm.GetByID(7)
	if c2.Position != 4 {
		t.Errorf("C2 position = %d, want 4", c2.Position)
	}
	if c7.Position != 5 {
		t.Errorf("C7 position = %d, want 5", c7.Position)
	}

	list := cm.ListConferences()
	wantOrder := []string{"C5", "C2", "C7"}
	for i, tag := range wantOrder {
		if list[i].Tag != tag {
			t.Errorf("list[%d] = %s, want %s", i, list[i].Tag, tag)
		}
	}
}

func TestGetSortedConferenceIDs(t *testing.T) {
	dir := t.TempDir()
	writeConferences(t, dir, `[
		{"id": 1, "position": 3, "tag": "A", "name": "A"},
		{"id": 2, "position": 1, "tag": "B", "name": "B"}
	]`)
	cm, err := NewConferenceManager(dir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}

	tests := []struct {
		name string
		in   map[int]bool
		want []int
	}{
		{"empty", map[int]bool{}, nil},
		{"zero first", map[int]bool{0: true, 1: true, 2: true}, []int{0, 2, 1}},
		{"by position", map[int]bool{1: true, 2: true}, []int{2, 1}},
		// ID 9 is unknown: falls back to its ID (9) as position, sorting
		// after B (pos 1) and A (pos 3).
		{"unknown falls back to ID", map[int]bool{1: true, 9: true, 2: true}, []int{2, 1, 9}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cm.GetSortedConferenceIDs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
