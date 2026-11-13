package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// accountIDFor is idFor for BindSponsors and BindUploaders: the ID of the
// live account with handle, 0 when there is none.
func accountIDFor(um *user.UserMgr) func(string) int {
	return func(handle string) int {
		u, ok := um.GetUser(handle)
		if !ok || u.DeletedUser {
			return 0
		}
		return u.ID
	}
}

// bindSponsors binds message area sponsors named only by handle to their
// accounts (#1020). Called at startup and after message_areas.json reloads.
// An area it couldn't bind has no sponsor until it is (SponsoredBy needs an
// ID), so a failure grants nothing; the error goes back to the caller so the
// config watcher tries the reload again.
func bindSponsors(um *user.UserMgr, mm *message.MessageManager) error {
	if um == nil || mm == nil {
		return nil
	}
	if _, _, err := mm.BindSponsors(accountIDFor(um)); err != nil {
		slog.Error("binding area sponsors to accounts failed; the areas left unbound have no sponsor until it succeeds", "error", err)
		return err
	}
	return nil
}

// bindUploaders binds file records from before uploader IDs to the
// accounts holding their uploaders' handles, once, at startup (#1020).
func bindUploaders(um *user.UserMgr, fm *file.FileManager) {
	if um == nil || fm == nil {
		return
	}
	// A record left unbound is nobody's to edit until a later start binds
	// it (UploadedByAccount needs an ID), so a failure grants nothing.
	bound, unowned, err := fm.BindUploaders(accountIDFor(um))
	if err != nil {
		slog.Error("binding uploads to accounts failed; the uploads left unbound can be edited only by co-sysops until the next start binds them", "error", err)
	}
	if bound+unowned > 0 {
		slog.Info("uploads bound to accounts", "bound", bound, "no_account", unowned)
	}
}

// checkSponsors adds doctor's findings on message area sponsors (#1020): a
// sponsor that names no account, one whose account is gone, and one whose
// name no longer matches its account (a rename, or a hand edit of "sponsor"
// that left the old "sponsor_id"). An unbound sponsor is fine: the BBS binds
// it at its next start.
func checkSponsors(configDir, root string, add func(string, doctorSeverity, string, string)) {
	data, err := os.ReadFile(filepath.Join(configDir, "message_areas.json"))
	if err != nil {
		return // reported by the areas check
	}
	var areas []message.MessageArea
	if json.Unmarshal(data, &areas) != nil {
		return
	}
	var users []struct {
		ID          int    `json:"id"`
		Handle      string `json:"handle"`
		DeletedUser bool   `json:"deletedUser"`
	}
	data, err = os.ReadFile(filepath.Join(root, "data", "users", "users.json"))
	if err != nil || json.Unmarshal(data, &users) != nil {
		return
	}
	byID := map[int]string{}
	for _, u := range users {
		if !u.DeletedUser {
			byID[u.ID] = u.Handle
		}
	}
	const name = "message area sponsors"
	problems := 0
	for _, a := range areas {
		if a.Sponsor == "" || a.SponsorID == 0 {
			continue
		}
		handle, live := byID[a.SponsorID]
		switch {
		case a.SponsorID == message.NoSponsorID:
			add(name, doctorWarn, fmt.Sprintf("area %s: sponsor %q matched no account, so the area has no sponsor", a.Tag, a.Sponsor),
				"Set the sponsor again from the Sponsor Menu or the config editor once the account exists.")
		case !live:
			add(name, doctorWarn, fmt.Sprintf("area %s: sponsor %q was account #%d, which no longer exists or is deleted, so the area has no sponsor", a.Tag, a.Sponsor, a.SponsorID),
				"Set a new sponsor, or clear it.")
		case !strings.EqualFold(handle, a.Sponsor):
			add(name, doctorWarn, fmt.Sprintf("area %s: sponsor is account #%d, now %q, but the area names %q", a.Tag, a.SponsorID, handle, a.Sponsor),
				"If the account was renamed, nothing is wrong; set the sponsor again to update the name. If you meant to change the sponsor, set it from the Sponsor Menu or the config editor: an edit to \"sponsor\" alone leaves \"sponsor_id\" on the old account.")
		default:
			continue
		}
		problems++
	}
	if problems == 0 {
		add(name, doctorOK, "every sponsor is bound to a live account, or will be at the next start", "")
	}
}

// checkUserIDMark adds doctor's finding on data/users/next_user_id (#1020):
// it must hold a number past every user ID, or the BBS refuses new accounts
// rather than risk giving out a purged account's ID.
func checkUserIDMark(root string, add func(string, doctorSeverity, string, string)) {
	const name = "user ID mark"
	path := filepath.Join(root, "data", "users", user.NextUserIDFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		add(name, doctorInfo, "data/users/next_user_id is absent; the BBS writes it at its next start", "")
		return
	}
	n, perr := 0, err
	if err == nil {
		n, perr = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if perr != nil || n < 1 {
		add(name, doctorFail, "data/users/next_user_id can't be read or doesn't hold a user ID, so the BBS refuses new accounts",
			"Write the next unused user ID into it: one more than the highest ID ever given out, including purged accounts.")
		return
	}
	var users []struct {
		ID int `json:"id"`
	}
	if data, err := os.ReadFile(filepath.Join(root, "data", "users", "users.json")); err == nil && json.Unmarshal(data, &users) == nil {
		for _, u := range users {
			if u.ID >= n {
				add(name, doctorWarn, fmt.Sprintf("data/users/next_user_id is %d, but account #%d exists; the BBS raises it at its next start", n, u.ID), "")
				return
			}
		}
	}
	add(name, doctorOK, fmt.Sprintf("the next user ID is %d", n), "")
}
