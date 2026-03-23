package ftn

import (
	"strings"
	"testing"
)

func TestParseEcholist(t *testing.T) {
	input := `; Comment line
; Another comment

FSX_GEN          General Discussion
FSX_BOT          Bot/AI Discussion
FSX_BBS          BBS Discussion
FSX_DAD          Dad Jokes

; More comments
FSX_MYS	Mystic BBS
`

	areas, err := ParseEcholist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseEcholist() error: %v", err)
	}

	if len(areas) != 5 {
		t.Fatalf("ParseEcholist() returned %d areas, want 5", len(areas))
	}

	expected := []EchoArea{
		{"FSX_GEN", "General Discussion"},
		{"FSX_BOT", "Bot/AI Discussion"},
		{"FSX_BBS", "BBS Discussion"},
		{"FSX_DAD", "Dad Jokes"},
		{"FSX_MYS", "Mystic BBS"},
	}

	for i, want := range expected {
		got := areas[i]
		if got.Tag != want.Tag {
			t.Errorf("area[%d].Tag = %q, want %q", i, got.Tag, want.Tag)
		}
		if got.Description != want.Description {
			t.Errorf("area[%d].Description = %q, want %q", i, got.Description, want.Description)
		}
	}
}

func TestParseEcholistEmpty(t *testing.T) {
	input := `; Only comments
; Nothing here
`
	areas, err := ParseEcholist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseEcholist() error: %v", err)
	}
	if len(areas) != 0 {
		t.Errorf("ParseEcholist() returned %d areas, want 0", len(areas))
	}
}

func TestParseEcholistTagOnly(t *testing.T) {
	input := `SOLITAIRE`
	areas, err := ParseEcholist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseEcholist() error: %v", err)
	}
	if len(areas) != 1 {
		t.Fatalf("ParseEcholist() returned %d areas, want 1", len(areas))
	}
	if areas[0].Tag != "SOLITAIRE" {
		t.Errorf("area.Tag = %q, want SOLITAIRE", areas[0].Tag)
	}
	if areas[0].Description != "" {
		t.Errorf("area.Description = %q, want empty", areas[0].Description)
	}
}

func TestCleanEcholist(t *testing.T) {
	areas := []EchoArea{
		{"FSX_GEN", "fsxNet: General Discussion"},
		{"FSX_BOT", "fsxNet: Bot/AI Discussion"},
		{"FSX_SYSOP", "fsxNet: SysOp Only"},
		{"FSX_TEST", "fsxNet: Testing"},
	}

	cleaned := CleanEcholist(areas, []string{"FSX_SYSOP", "FSX_TEST"}, "fsxNet: ")

	if len(cleaned) != 2 {
		t.Fatalf("CleanEcholist() returned %d areas, want 2", len(cleaned))
	}

	if cleaned[0].Tag != "FSX_GEN" {
		t.Errorf("cleaned[0].Tag = %q, want FSX_GEN", cleaned[0].Tag)
	}
	if cleaned[0].Description != "General Discussion" {
		t.Errorf("cleaned[0].Description = %q, want 'General Discussion'", cleaned[0].Description)
	}
	if cleaned[1].Description != "Bot/AI Discussion" {
		t.Errorf("cleaned[1].Description = %q, want 'Bot/AI Discussion'", cleaned[1].Description)
	}
}

func TestCleanEcholistCaseInsensitive(t *testing.T) {
	areas := []EchoArea{
		{"fsx_gen", "General"},
		{"FSX_BOT", "Bot"},
	}
	cleaned := CleanEcholist(areas, []string{"FSX_GEN"}, "")
	if len(cleaned) != 1 {
		t.Fatalf("CleanEcholist() returned %d areas, want 1", len(cleaned))
	}
	if cleaned[0].Tag != "FSX_BOT" {
		t.Errorf("cleaned[0].Tag = %q, want FSX_BOT", cleaned[0].Tag)
	}
}
