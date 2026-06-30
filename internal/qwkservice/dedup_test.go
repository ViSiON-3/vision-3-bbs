package qwkservice

import (
	"path/filepath"
	"testing"
)

func TestREPDedup_RecordIfNew(t *testing.T) {
	d, err := openREPDedup(filepath.Join(t.TempDir(), "dedup.db"))
	if err != nil {
		t.Fatalf("openREPDedup: %v", err)
	}
	defer d.Close()

	isNew, err := d.RecordIfNew("tester", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("first record should be new")
	}

	isNew, err = d.RecordIfNew("tester", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("second identical record should be a duplicate")
	}
}

func TestREPDedup_PerHandleIsolation(t *testing.T) {
	d, err := openREPDedup(filepath.Join(t.TempDir(), "dedup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.RecordIfNew("alice", "h"); err != nil {
		t.Fatal(err)
	}
	isNew, err := d.RecordIfNew("bob", "h") // same hash, different handle
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("same hash under a different handle must not be a duplicate")
	}
}

func TestREPDedup_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dedup.db")
	d1, err := openREPDedup(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d1.RecordIfNew("tester", "h"); err != nil {
		t.Fatal(err)
	}
	d1.Close()

	d2, err := openREPDedup(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	isNew, err := d2.RecordIfNew("tester", "h")
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("record should persist across reopen")
	}
}
