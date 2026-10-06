package usereditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"os"
	"path/filepath"
	"strconv"
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
	m := Model{configDir: dir, dataDir: filepath.Join(root, "data"), users: []*user.User{{Handle: "Tester"}}}
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

func TestAlternateUsersPathUsesExplicitBoardConfig(t *testing.T) {
	board := t.TempDir()
	t.Chdir(board)
	usersDir := filepath.Join(t.TempDir(), "external-accounts")
	if err := os.MkdirAll(usersDir, 0700); err != nil {
		t.Fatal(err)
	}
	usersFile := filepath.Join(usersDir, "users.json")
	if _, err := SaveUsers(usersFile, []*user.User{{ID: 1, Handle: "Tester"}}); err != nil {
		t.Fatal(err)
	}
	for _, configDir := range []string{"configs", filepath.Join(t.TempDir(), "board-config")} {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			t.Fatal(err)
		}
		names := filepath.Join(board, "configured-names.txt")
		if err := os.WriteFile(names, []byte("admin*"), 0600); err != nil {
			t.Fatal(err)
		}
		data := []byte(`{"badUsersPath":` + strconv.Quote(names) + `,"deletedUserRetentionDays":7}`)
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		m, err := NewWithConfig(usersFile, configDir)
		if err != nil {
			t.Fatal(err)
		}
		if m.retentionDays != 7 {
			t.Fatalf("config not loaded: retention = %d", m.retentionDays)
		}
		for _, f := range m.fields {
			if f.Label == "Handle" {
				m.textInput.SetValue("Admin Jane")
				if err := m.applyFieldValue(f); err != nil {
					t.Fatal(err)
				}
				if m.users[0].Handle != "Admin Jane" || !strings.Contains(m.message, "matches bad user names list") {
					t.Fatalf("config %q: handle %q; warning %q", configDir, m.users[0].Handle, m.message)
				}
			}
		}
	}
}
