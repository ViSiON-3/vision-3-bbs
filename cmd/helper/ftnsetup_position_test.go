package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// TestFTNSetupAssignsSequentialPositions covers the import bug behind areas
// landing "out of place": ftnsetup created areas with no Position (0), so they
// all sorted to the top of the config editor's list, away from their
// conference. Imported areas must instead get the next positions after the
// existing ones.
func TestFTNSetupAssignsSequentialPositions(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "message_areas.json"), []message.MessageArea{
		{ID: 1, Position: 1, Tag: "GENERAL", Name: "General", ConferenceID: 1, AreaType: "local"},
		{ID: 2, Position: 2, Tag: "PRIVMAIL", Name: "Private", ConferenceID: 1, AreaType: "local"},
	})
	writeJSONFile(t, filepath.Join(dir, "conferences.json"), []conferenceConfig{})

	naPath := filepath.Join(dir, "tqwnet.na")
	if err := os.WriteFile(naPath, []byte("TQW_FOO Foo Area\nTQW_BAR Bar Area\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmdFTNSetup([]string{
		"--na", naPath,
		"--address", "1337:3/123.1",
		"--hub", "1337:3/100",
		"--network", "tqwnet",
		"--config", dir,
		"--quiet",
	})

	var areas []message.MessageArea
	data, err := os.ReadFile(filepath.Join(dir, "message_areas.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &areas); err != nil {
		t.Fatal(err)
	}

	byTag := map[string]message.MessageArea{}
	for _, a := range areas {
		byTag[a.Tag] = a
	}
	for _, tc := range []struct {
		tag string
		pos int
	}{
		{"TQW_FOO", 3},
		{"TQW_BAR", 4},
	} {
		a, ok := byTag[tc.tag]
		if !ok {
			t.Fatalf("imported area %s missing", tc.tag)
		}
		if a.Position != tc.pos {
			t.Errorf("%s position = %d, want %d (sequential after existing, never 0)", tc.tag, a.Position, tc.pos)
		}
	}
}

func writeJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
