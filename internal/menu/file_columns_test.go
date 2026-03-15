package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestFileColumnEnabled_AllDefault(t *testing.T) {
	u := &user.User{}
	// All-default (zero value) should show all columns
	for _, col := range []string{"name", "size", "date", "downloads", "uploader", "description"} {
		if !fileColumnEnabled(u, col, false) {
			t.Errorf("expected column %q enabled when all defaults, got disabled", col)
		}
	}
}

func TestFileColumnEnabled_Extended(t *testing.T) {
	u := &user.User{}
	u.FileListColumns.Name = false
	u.FileListColumns.Size = true
	// Extended mode overrides all config
	if !fileColumnEnabled(u, "name", true) {
		t.Error("expected name enabled in extended mode")
	}
}

func TestFileColumnEnabled_Selective(t *testing.T) {
	u := &user.User{}
	u.FileListColumns.Name = true
	u.FileListColumns.Size = false
	u.FileListColumns.Date = true
	u.FileListColumns.Downloads = false
	u.FileListColumns.Uploader = true
	u.FileListColumns.Description = true

	tests := []struct {
		col  string
		want bool
	}{
		{"name", true},
		{"size", false},
		{"date", true},
		{"downloads", false},
		{"uploader", true},
		{"description", true},
	}
	for _, tt := range tests {
		got := fileColumnEnabled(u, tt.col, false)
		if got != tt.want {
			t.Errorf("fileColumnEnabled(%q) = %v, want %v", tt.col, got, tt.want)
		}
	}
}

func TestFileColumnEnabled_UnknownColumn(t *testing.T) {
	u := &user.User{}
	u.FileListColumns.Name = true // make non-default
	if !fileColumnEnabled(u, "unknown", false) {
		t.Error("expected unknown column to be enabled")
	}
}
