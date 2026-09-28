package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// loadUsers opens the BBS's users file in dataDir. Unlike
// user.NewUserManager alone it never creates one: a missing users.json is an
// error here rather than a reason to write a default sysop account into a
// data directory that may simply be the wrong one.
func loadUsers(dataDir string) (*user.UserMgr, error) {
	path := filepath.Join(dataDir, "users.json")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("users file %s: %w", path, err)
	}
	return user.NewUserManager(dataDir)
}

// loadRecipients returns the accounts inbound private mail is addressed to,
// for the importers' SetRecipientResolver. When the users file cannot be read
// it logs why and returns nil, which the importers treat as "store To as
// received": mail still tosses, it just is not readdressed.
func loadRecipients(dataDir string) user.RecipientResolver {
	um, err := loadUsers(dataDir)
	if err != nil {
		slog.Warn("private mail will be stored with To as received: cannot load users", "error", err)
		return nil
	}
	return um
}

// recipientDirectory is what readdressing needs from the user manager.
type recipientDirectory interface {
	user.RecipientResolver
	HandleExists(handle string) bool
	GetAllUsers() []*user.User
}

// readdressResult counts what readdressBase did with a base's private mail
// that was not already addressed to an account's handle.
type readdressResult struct {
	Readdressed   int // To rewritten to the recipient's handle
	Undeliverable int // To names no account
	Ambiguous     int // To is a real name two or more accounts share
}

func (r *readdressResult) add(o readdressResult) {
	r.Readdressed += o.Readdressed
	r.Undeliverable += o.Undeliverable
	r.Ambiguous += o.Ambiguous
}

// cmdReaddress implements 'v3mail readdress': rewrite the To of stored
// private mail from a real name or "Sysop" to the recipient's handle, so the
// mail imported before importers addressed it by handle can be read again.
func cmdReaddress(args []string) {
	fs := flag.NewFlagSet("readdress", flag.ExitOnError)
	allFlag, configDir, dataDir, quiet := addGlobalFlags(fs)
	dryRun := fs.Bool("dry-run", false, "Report what would change without writing")
	_ = fs.Parse(args) // ExitOnError: Parse exits the program on failure

	paths, err := resolveBasePaths(*allFlag, *configDir, *dataDir, fs.Args())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	users, err := loadUsers(*dataDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	hadErrors := false
	var total readdressResult
	for _, meta := range paths {
		b, err := jam.Open(meta.Path)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", meta.Path, err)
			hadErrors = true
			continue
		}
		res, rerr := readdressBase(b, users, *dryRun, *quiet, meta.Tag)
		if !closeBase(b, meta.Path) {
			hadErrors = true
		}
		if rerr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error readdressing %s: %v\n", meta.Path, rerr)
			hadErrors = true
		}
		total.add(res)
	}

	if !*quiet {
		verb := "readdressed"
		if *dryRun {
			verb = "would be readdressed (dry run, nothing written)"
		}
		fmt.Printf("Readdress complete: %d %s, %d left undeliverable, %d ambiguous\n",
			total.Readdressed, verb, total.Undeliverable, total.Ambiguous)
	}
	if hadErrors {
		os.Exit(1)
	}
}

// readdressBase rewrites, in one base, the To of every private message not
// already addressed to an account's handle when user.UserMgr.ResolveRecipient
// resolves it. Mail it cannot resolve is left as it is (sysops can still read
// it in the BBS). A message already addressed by handle is never touched, so
// running it again changes nothing. With dryRun nothing is written.
func readdressBase(b *jam.Base, users recipientDirectory, dryRun, quiet bool, tag string) (readdressResult, error) {
	var res readdressResult
	total, err := b.GetMessageCount()
	if err != nil {
		return res, err
	}
	for n := 1; n <= total; n++ {
		hdr, err := b.ReadMessageHeader(n)
		if errors.Is(err, jam.ErrNotFound) {
			continue // free index slot
		}
		if err != nil {
			return res, fmt.Errorf("message %d: %w", n, err)
		}
		if hdr.Attribute&jam.MsgDeleted != 0 || hdr.Attribute&jam.MsgPrivate == 0 {
			continue
		}
		to := ""
		if sf := hdr.GetSubfieldByType(jam.SfldReceiverName); sf != nil {
			to = string(sf.Buffer)
		}
		if users.HandleExists(to) {
			continue
		}
		u, ok := users.ResolveRecipient(to)
		if !ok {
			if realNameShared(users, to) {
				res.Ambiguous++
				if !quiet {
					fmt.Printf("%s #%d: %q matches more than one account's real name; left as is\n", tag, n, to)
				}
			} else {
				res.Undeliverable++
				if !quiet {
					fmt.Printf("%s #%d: %q matches no account; left as is\n", tag, n, to)
				}
			}
			continue
		}
		if !quiet {
			fmt.Printf("%s #%d: %q -> %q\n", tag, n, to, u.Handle)
		}
		if !dryRun {
			if err := b.SetReceiverName(n, u.Handle); err != nil {
				return res, fmt.Errorf("message %d: %w", n, err)
			}
		}
		res.Readdressed++
	}
	return res, nil
}

// realNameShared reports whether two or more live accounts have name as their
// real name, which is why ResolveRecipient declined it.
func realNameShared(users recipientDirectory, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	n := 0
	for _, u := range users.GetAllUsers() {
		if !u.DeletedUser && strings.EqualFold(strings.TrimSpace(u.RealName), name) {
			n++
		}
	}
	return n > 1
}
