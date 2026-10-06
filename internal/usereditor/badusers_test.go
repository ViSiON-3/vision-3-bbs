package usereditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleEditWarnsAndAllowsBadName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "badusers.txt")
	if err := os.WriteFile(path, []byte("admin*"), 0600); err != nil {
		t.Fatal(err)
	}
	m := Model{dataDir: filepath.Join(root, "data"), users: []*user.User{{Handle: "Tester"}}}
	for _, f := range editFields() {
		if f.Label == "Handle" {
			m.textInput.SetValue("Admin Jane")
			if err := m.applyFieldValue(f); err != nil {
				t.Fatal(err)
			}
			if m.users[0].Handle != "Admin Jane" || !strings.Contains(m.message, "override allowed") {
				t.Fatalf("handle %q, message %q", m.users[0].Handle, m.message)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			m.textInput.SetValue("Legitimate")
			if err := m.applyFieldValue(f); err != nil {
				t.Fatal(err)
			}
			if m.users[0].Handle != "Legitimate" || !strings.Contains(m.message, "cannot read") {
				t.Fatalf("handle %q, message %q", m.users[0].Handle, m.message)
			}
			return
		}
	}
	t.Fatal("Handle field missing")
}
