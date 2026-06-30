package qwk

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a committed packet sample from testdata.
func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	p := filepath.Join(append([]string{"testdata"}, parts...)...)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return data
}

func TestFixture_VISION3_QWK_Structure(t *testing.T) {
	data := readFixture(t, "vision3", "VISION3.QWK")

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("fixture is not a valid zip: %v", err)
	}

	names := make(map[string]bool)
	for _, f := range zr.File {
		names[strings.ToUpper(f.Name)] = true
	}
	for _, want := range []string{"CONTROL.DAT", "DOOR.ID", "MESSAGES.DAT", "001.NDX", "PERSONAL.NDX"} {
		if !names[want] {
			t.Errorf("VISION3.QWK fixture missing %s", want)
		}
	}
}

func TestFixture_VISION3_REP_Reads(t *testing.T) {
	data := readFixture(t, "vision3", "VISION3.REP")

	msgs, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP on fixture failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 message in fixture, got %d", len(msgs))
	}
	if msgs[0].To != "SysOp" {
		t.Errorf("fixture reply To: want 'SysOp', got %q", msgs[0].To)
	}
	if !strings.Contains(msgs[0].Subject, "Welcome") {
		t.Errorf("fixture reply Subject: want to contain 'Welcome', got %q", msgs[0].Subject)
	}
}

func TestFixture_Malformed_REP_FailsGracefully(t *testing.T) {
	data := readFixture(t, "malformed", "TRUNCATED.REP")

	_, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err == nil {
		t.Fatal("expected ReadREP to reject the truncated fixture")
	}
}
