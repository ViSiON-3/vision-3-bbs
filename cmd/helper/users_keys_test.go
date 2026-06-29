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
