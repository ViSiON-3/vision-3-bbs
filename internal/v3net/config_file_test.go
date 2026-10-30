package v3net

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A write that replaces v3net.json while it is being read must not pair the
// old timestamp with the new contents: the next reload would take that write
// for the sysop saving the file as it was, and drop unsaved hubSigned records
// on every leaf. A touch after that is still recognised.
func TestConfigFileReplacedDuringReadIsNotATouch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v3net.json")
	base := time.Now().Add(-time.Hour)
	replace := func(contents string, at time.Time) {
		t.Helper()
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(tmp, at, at); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
	}
	replace(`{"enabled":true}`, base)

	s := &Service{}
	s.cfg.ConfigPath = dir
	if s.noteConfigFile() {
		t.Fatal("the first record reported a touch")
	}

	s.configFileReadHook = func() {
		s.configFileReadHook = nil
		replace(`{"enabled":false,"registryUrl":"x"}`, base.Add(time.Minute))
	}
	if s.noteConfigFile() {
		t.Error("a file replaced during the read reported a touch")
	}
	if s.noteConfigFile() {
		t.Fatal("the reload after a replacement during the read took it for a touch")
	}

	at := base.Add(2 * time.Minute)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	if !s.noteConfigFile() {
		t.Error("a touch after the replacement was not recognised")
	}
}
