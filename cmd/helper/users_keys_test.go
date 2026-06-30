package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func writeUsersJSON(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const usersJSON = `[{"id":1,"handle":"Boss","accessLevel":255}]`
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(usersJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " co@laptop"
}

// TestResolveUsersDataPathMissingValue ensures --data with no following value
// returns an error instead of silently defaulting to "data/users".
func TestResolveUsersDataPathMissingValue(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		wantVal string
	}{
		{
			name:    "--data absent uses default",
			args:    []string{"Boss", "somefile"},
			wantErr: false,
			wantVal: "data/users",
		},
		{
			name:    "--data with value",
			args:    []string{"--data", "/tmp/users", "Boss"},
			wantErr: false,
			wantVal: "/tmp/users",
		},
		{
			name:    "--data trailing (no value)",
			args:    []string{"Boss", "--data"},
			wantErr: true,
		},
		{
			name:    "--data followed by another flag",
			args:    []string{"--data", "--other"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveUsersDataPath(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveUsersDataPath(%v) = %q, nil; want error", tc.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveUsersDataPath(%v) error: %v", tc.args, err)
			}
			if got != tc.wantVal {
				t.Errorf("resolveUsersDataPath(%v) = %q; want %q", tc.args, got, tc.wantVal)
			}
		})
	}
}

func TestUsersAddAndDelKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "users")
	writeUsersJSON(t, dir)
	keyFile := filepath.Join(t.TempDir(), "co.pub")
	line := makeKeyLine(t)
	if err := os.WriteFile(keyFile, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// addkey from a file.
	cmdUsersAddKey([]string{"--data", dir, "Boss", keyFile})

	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	u, ok := um.GetUser("Boss")
	if !ok || len(u.PublicKeys) != 1 {
		t.Fatalf("addkey did not persist: %+v", u)
	}
	keys, _ := u.ListPublicKeys()
	fp := keys[0].Fingerprint

	// delkey by fingerprint.
	cmdUsersDelKey([]string{"--data", dir, "Boss", fp})
	um2, _ := user.NewUserManager(dir)
	u2, _ := um2.GetUser("Boss")
	if len(u2.PublicKeys) != 0 {
		t.Fatalf("delkey did not persist: %+v", u2)
	}
}
