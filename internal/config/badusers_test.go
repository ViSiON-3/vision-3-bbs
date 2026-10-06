package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBadUserNamesMatching(t *testing.T) {
	path := filepath.Join(t.TempDir(), "names.txt")
	if err := os.WriteFile(path, []byte(" ; comment\r\n # comment\r\n\r\nroot\r\n admin* \r\n*sysop*\r\nguest\r\nÉmile\r\na.b\r\nx?y\r\n[a]\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ handle, rule string }{
		{"ROOT", "root"}, {" root ", "root"}, {"rooted", ""}, {"uproot", ""},
		{"Admin", "admin*"}, {"aDmIn Jane", "admin*"}, {"xadmin", ""},
		{"TheSysOp", "*sysop*"}, {"sysop", "*sysop*"}, {"SySoP staff", "*sysop*"},
		{"émILE", "Émile"}, {"Emile", ""}, {"E\u0301mile", ""},
		{"a.b", "a.b"}, {"axb", ""}, {"x?y", "x?y"}, {"xay", ""}, {"[a]", "[a]"}, {"a", ""},
		{"Felonius", ""},
	} {
		t.Run(tc.handle, func(t *testing.T) {
			got, err := MatchBadUserName(path, tc.handle)
			if err != nil || got != tc.rule {
				t.Fatalf("got %q, %v; want %q", got, err, tc.rule)
			}
		})
	}
}

func TestBadUserNamesReloadAndInvalidFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "names.txt")
	for _, tc := range []struct {
		content      []byte
		handle, rule string
		bad          bool
	}{
		{[]byte("root\n"), "root", "root", false},
		{[]byte("guest\n"), "root", "", false},
		{[]byte(""), "root", "", false},
		{[]byte("; only comments\n# comment\n"), "root", "", false},
		{[]byte("root\n\xff"), "root", "", true},
		{[]byte("root\nbad\x00rule"), "root", "", true},
	} {
		if err := os.WriteFile(path, tc.content, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := MatchBadUserName(path, tc.handle)
		if got != tc.rule || (err != nil) != tc.bad {
			t.Fatalf("%q: got %q, %v", tc.content, got, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, filepath.Dir(path)} {
		if got, err := MatchBadUserName(p, "root"); got != "" || err == nil {
			t.Fatalf("%s: got %q, %v", p, got, err)
		}
	}
}

func TestBadUsersFilePaths(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ configured, want string }{
		{"", filepath.Join(dir, "badusers.txt")}, {"configs/badusers.txt", filepath.Join(dir, "badusers.txt")},
		{"custom/names.txt", "custom/names.txt"}, {filepath.Join(dir, "names.txt"), filepath.Join(dir, "names.txt")},
	} {
		if got := (ServerConfig{BadUsersPath: tc.configured}).BadUsersFile(dir); got != tc.want {
			t.Fatalf("%q: %q != %q", tc.configured, got, tc.want)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BadUsersPath != "configs/badusers.txt" {
		t.Fatalf("default = %q", cfg.BadUsersPath)
	}
}
