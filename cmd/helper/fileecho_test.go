package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckConference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conferences.json")
	if err := os.WriteFile(path, []byte(`[{"id":2,"tag":"T","name":"T"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkConference(path, 2); err != nil {
		t.Errorf("existing conference rejected: %v", err)
	}
	if err := checkConference(path, 9); err == nil {
		t.Error("unknown conference accepted")
	}
}
