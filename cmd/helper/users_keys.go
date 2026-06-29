package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// resolveUsersDataPath returns the value of a trailing/leading --data flag,
// defaulting to "data/users" (mirrors cmdUsersList/cmdUsersPurge).
func resolveUsersDataPath(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--data" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return "data/users"
}

// stripFlags removes "--data <v>" pairs, leaving positional args.
func stripFlags(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--data" {
			i++ // skip value
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func loadUserOrExit(dataPath, handle string) (*user.UserMgr, *user.User) {
	um, err := user.NewUserManager(dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load users from %s: %v\n", dataPath, err)
		os.Exit(1)
	}
	u, ok := um.GetUser(handle)
	if !ok || u == nil {
		fmt.Fprintf(os.Stderr, "Error: no user with handle %q\n", handle)
		os.Exit(1)
	}
	return um, u
}

func cmdUsersAddKey(args []string) {
	dataPath := resolveUsersDataPath(args)
	pos := stripFlags(args)
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: helper users addkey <handle> <keyfile|->")
		os.Exit(1)
	}
	handle, src := pos[0], pos[1]

	var raw []byte
	var err error
	if src == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(src)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read key from %s: %v\n", src, err)
		os.Exit(1)
	}

	um, u := loadUserOrExit(dataPath, handle)
	info, err := u.AddPublicKey(strings.TrimSpace(string(raw)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := um.UpdateUser(u); err != nil { // writes the mutated copy back + saves
		fmt.Fprintf(os.Stderr, "Error: failed to save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added key %s (%s) to %s\n", info.Fingerprint, info.Comment, u.Handle)
}

func cmdUsersListKeys(args []string) {
	dataPath := resolveUsersDataPath(args)
	pos := stripFlags(args)
	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: helper users listkeys <handle>")
		os.Exit(1)
	}
	_, u := loadUserOrExit(dataPath, pos[0])
	keys, unparseable := u.ListPublicKeys()
	if len(keys) == 0 {
		fmt.Printf("%s has no WFC public keys.\n", u.Handle)
	}
	for i, k := range keys {
		fmt.Printf("  %d. %-12s %s  %s\n", i+1, k.Type, k.Fingerprint, k.Comment)
	}
	if unparseable > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d unparseable key line(s) in this user's record.\n", unparseable)
	}
}

func cmdUsersDelKey(args []string) {
	dataPath := resolveUsersDataPath(args)
	pos := stripFlags(args)
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: helper users delkey <handle> <fingerprint|index>")
		os.Exit(1)
	}
	um, u := loadUserOrExit(dataPath, pos[0])
	info, err := u.RemovePublicKey(pos[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := um.UpdateUser(u); err != nil { // writes the mutated copy back + saves
		fmt.Fprintf(os.Stderr, "Error: failed to save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed key %s from %s\n", info.Fingerprint, u.Handle)
}
