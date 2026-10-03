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

// The suggested filefix command must read the config the areas were written
// to, so a non-default --config is carried along.
func TestFilefixSeedCommand(t *testing.T) {
	for _, tc := range []struct{ network, dir, want string }{
		{"tqwnet", "configs", "helper filefix --network tqwnet --seed"},
		{"tqwnet", "/srv/bbs/configs", "helper filefix --network tqwnet --seed --config /srv/bbs/configs"},
		{"tqwnet", "/srv/my bbs/it's", `helper filefix --network tqwnet --seed --config '/srv/my bbs/it'\''s'`},
	} {
		if got := filefixSeedCommand(tc.network, tc.dir); got != tc.want {
			t.Errorf("filefixSeedCommand(%q, %q) = %q, want %q", tc.network, tc.dir, got, tc.want)
		}
	}
}
