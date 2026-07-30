package menu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestLoadRumorsData_MissingFileDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	rd, err := loadRumorsData(filepath.Join(tmpDir, "configs"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rd.Rumors) != 0 {
		t.Errorf("expected 0 rumors, got %d", len(rd.Rumors))
	}
	if rd.NextID != 1 {
		t.Errorf("expected NextID=1, got %d", rd.NextID)
	}
}

func TestLoadRumorsData_NextIDFromMaxExisting(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	// next_id is 0/omitted in the file; loadRumorsData should compute max(ID)+1.
	data := []byte(`{"rumors":[{"id":5},{"id":2},{"id":9}],"next_id":0}`)
	if err := os.WriteFile(filepath.Join(dataDir, "rumors.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	rd, err := loadRumorsData(filepath.Join(tmpDir, "configs"))
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if rd.NextID != 10 {
		t.Errorf("NextID should be 10 (max existing ID 9 + 1), got %d", rd.NextID)
	}
}

func TestSaveAndLoadRumorsData(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmpDir, "configs")

	now := time.Now().Truncate(time.Second)
	rd := &rumorsData{
		NextID: 3,
		Rumors: []RumorRecord{
			{
				ID:       1,
				Author:   "Bob",
				RealUser: "bob",
				UserID:   7,
				Text:     "first rumor",
				PostedAt: now,
				MinLevel: 5,
			},
			{
				ID:       2,
				Author:   "Anonymous",
				RealUser: "sally",
				Text:     "second rumor",
				PostedAt: now,
				MinLevel: 0,
			},
		},
	}

	if err := saveRumorsData(configPath, rd); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	fp := rumorsFilePath(configPath)
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Fatal("rumors.json was not created")
	}

	loaded, err := loadRumorsData(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.NextID != 3 {
		t.Errorf("NextID: got %d, want 3", loaded.NextID)
	}
	if len(loaded.Rumors) != 2 {
		t.Fatalf("rumors count: got %d, want 2", len(loaded.Rumors))
	}
	r0 := loaded.Rumors[0]
	if r0.Author != "Bob" || r0.RealUser != "bob" || r0.UserID != 7 || r0.Text != "first rumor" || r0.MinLevel != 5 {
		t.Errorf("rumors[0] round-trip mismatch: %+v", r0)
	}
	if !r0.PostedAt.Equal(now) {
		t.Errorf("rumors[0].PostedAt: got %v, want %v", r0.PostedAt, now)
	}
	r1 := loaded.Rumors[1]
	if r1.UserID != 0 {
		t.Errorf("rumors[1].UserID should default to 0, got %d", r1.UserID)
	}
}

func TestVisibleRumors(t *testing.T) {
	rd := &rumorsData{
		Rumors: []RumorRecord{
			{ID: 1, MinLevel: 0},
			{ID: 2, MinLevel: 50},
			{ID: 3, MinLevel: 10},
			{ID: 4, MinLevel: 100},
		},
	}

	tests := []struct {
		userLevel int
		want      []int
	}{
		{0, []int{0}},
		{10, []int{0, 2}},
		{50, []int{0, 1, 2}},
		{100, []int{0, 1, 2, 3}},
	}

	for _, tt := range tests {
		got := visibleRumors(rd, tt.userLevel)
		if len(got) != len(tt.want) {
			t.Fatalf("userLevel=%d: got %v, want %v", tt.userLevel, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("userLevel=%d: got %v, want %v", tt.userLevel, got, tt.want)
				break
			}
		}
	}
}

func TestRumorSanitize(t *testing.T) {
	tests := []struct {
		input, expect string
	}{
		{"Normal text", "Normal text"},
		{"|15colored|07 text", "\xc2\xa615colored\xc2\xa607 text"},
		{"no pipes here", "no pipes here"},
		{"", ""},
		{"one|pipe", "one\xc2\xa6pipe"},
	}
	for _, tt := range tests {
		got := rumorSanitize(tt.input)
		if got != tt.expect {
			t.Errorf("rumorSanitize(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestRumorDisplayAuthor(t *testing.T) {
	tests := []struct {
		name          string
		r             RumorRecord
		isSysop       bool
		anonymousName string
		want          string
	}{
		{
			name:          "named author returned as-is regardless of sysop",
			r:             RumorRecord{Author: "Bob", RealUser: "bob"},
			isSysop:       false,
			anonymousName: "Anonymous",
			want:          "Bob",
		},
		{
			name:          "blank author, non-sysop, sees anonymous name",
			r:             RumorRecord{Author: "", RealUser: "bob"},
			isSysop:       false,
			anonymousName: "Anonymous",
			want:          "Anonymous",
		},
		{
			name:          "blank author, sysop, sees anonymous name plus real handle",
			r:             RumorRecord{Author: "", RealUser: "bob"},
			isSysop:       true,
			anonymousName: "Anonymous",
			want:          "Anonymous (bob)",
		},
		{
			name:          "author equal to anonymous name, non-sysop",
			r:             RumorRecord{Author: "Ghost", RealUser: "sally"},
			isSysop:       false,
			anonymousName: "Ghost",
			want:          "Ghost",
		},
		{
			name:          "author equal to anonymous name, sysop reveals real handle",
			r:             RumorRecord{Author: "Ghost", RealUser: "sally"},
			isSysop:       true,
			anonymousName: "Ghost",
			want:          "Ghost (sally)",
		},
		{
			name:          "blank anonymousName defaults to Anonymous",
			r:             RumorRecord{Author: "", RealUser: "bob"},
			isSysop:       false,
			anonymousName: "   ",
			want:          "Anonymous",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rumorDisplayAuthor(&tt.r, tt.isSysop, tt.anonymousName)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandRandomRumorATCode(t *testing.T) {
	t.Run("no @RR code leaves content untouched and does no I/O", func(t *testing.T) {
		content := []byte("hello world, no code here")
		// Intentionally nonexistent path — if this were touched, loadRumorsData
		// would return an error and the guard would have failed to short-circuit.
		got := expandRandomRumorATCode(content, "/nonexistent/path/that/does/not/exist", 10)
		if string(got) != string(content) {
			t.Errorf("got %q, want unchanged %q", got, content)
		}
	})

	t.Run("single visible rumor is substituted deterministically", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := filepath.Join(tmpDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(tmpDir, "configs")
		rd := &rumorsData{
			NextID: 2,
			Rumors: []RumorRecord{{ID: 1, Text: "life is a test", MinLevel: 0}},
		}
		if err := saveRumorsData(configPath, rd); err != nil {
			t.Fatal(err)
		}

		content := []byte("hello @RR@ world")
		got := expandRandomRumorATCode(content, configPath, 10)
		want := "hello life is a test world"
		if string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestGetRandomRumorText(t *testing.T) {
	t.Run("no data file returns empty string", func(t *testing.T) {
		tmpDir := t.TempDir()
		got := getRandomRumorText(filepath.Join(tmpDir, "configs"), 10)
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("no visible rumors at user level returns empty string", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := filepath.Join(tmpDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(tmpDir, "configs")
		rd := &rumorsData{NextID: 2, Rumors: []RumorRecord{{ID: 1, Text: "secret", MinLevel: 100}}}
		if err := saveRumorsData(configPath, rd); err != nil {
			t.Fatal(err)
		}
		got := getRandomRumorText(configPath, 10)
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("single visible rumor returned deterministically", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := filepath.Join(tmpDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(tmpDir, "configs")
		rd := &rumorsData{NextID: 2, Rumors: []RumorRecord{{ID: 1, Text: "the only rumor", MinLevel: 0}}}
		if err := saveRumorsData(configPath, rd); err != nil {
			t.Fatal(err)
		}
		got := getRandomRumorText(configPath, 10)
		if got != "the only rumor" {
			t.Errorf("got %q, want %q", got, "the only rumor")
		}
	})
}

func TestBackfillRumorUserIDs(t *testing.T) {
	t.Run("legacy record with matching handle is backfilled", func(t *testing.T) {
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatalf("NewUserManager: %v", err)
		}
		bob, err := um.AddUser("password", "bob", "Real Bob", "Loc")
		if err != nil {
			t.Fatalf("AddUser: %v", err)
		}

		rd := &rumorsData{Rumors: []RumorRecord{
			{ID: 1, RealUser: "bob", UserID: 0},
		}}

		changed := backfillRumorUserIDs(rd, um)
		if !changed {
			t.Fatal("expected changed=true")
		}
		if rd.Rumors[0].UserID != bob.ID {
			t.Errorf("UserID = %d, want %d", rd.Rumors[0].UserID, bob.ID)
		}
	})

	t.Run("record already has UserID is left alone", func(t *testing.T) {
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatalf("NewUserManager: %v", err)
		}
		if _, err := um.AddUser("password", "bob", "Real Bob", "Loc"); err != nil {
			t.Fatalf("AddUser: %v", err)
		}

		rd := &rumorsData{Rumors: []RumorRecord{
			{ID: 1, RealUser: "bob", UserID: 99},
		}}

		changed := backfillRumorUserIDs(rd, um)
		if changed {
			t.Fatal("expected changed=false")
		}
		if rd.Rumors[0].UserID != 99 {
			t.Errorf("UserID should remain 99, got %d", rd.Rumors[0].UserID)
		}
	})

	t.Run("record with blank RealUser is left alone", func(t *testing.T) {
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatalf("NewUserManager: %v", err)
		}

		rd := &rumorsData{Rumors: []RumorRecord{
			{ID: 1, RealUser: "", UserID: 0},
		}}

		changed := backfillRumorUserIDs(rd, um)
		if changed {
			t.Fatal("expected changed=false")
		}
		if rd.Rumors[0].UserID != 0 {
			t.Errorf("UserID should remain 0, got %d", rd.Rumors[0].UserID)
		}
	})

	t.Run("record whose RealUser no longer exists is left alone", func(t *testing.T) {
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatalf("NewUserManager: %v", err)
		}

		rd := &rumorsData{Rumors: []RumorRecord{
			{ID: 1, RealUser: "ghost", UserID: 0},
		}}

		changed := backfillRumorUserIDs(rd, um)
		if changed {
			t.Fatal("expected changed=false")
		}
		if rd.Rumors[0].UserID != 0 {
			t.Errorf("UserID should remain 0, got %d", rd.Rumors[0].UserID)
		}
	})
}
