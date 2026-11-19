package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// loadRoles reads configs/roles.json into the roles every permission check
// reads (perm.Current), logging each problem in it. Called at startup and
// whenever the file changes or is removed.
//
// A file that can't be read grants nothing until it is fixed, at startup and
// on reload alike: keeping the previous roles would leave a role the sysop
// was in the middle of removing in force. Sysops keep every permission
// through their level either way.
func loadRoles(configDir string) {
	roles, diags, err := perm.Load(configDir)
	if err != nil {
		perm.SetCurrent(nil)
		slog.Error("configs/roles.json can't be read, so no role grants anything until it is fixed", "error", err)
		return
	}
	for _, d := range diags {
		slog.Warn("configs/roles.json: " + d.String())
	}
	perm.SetCurrent(roles)
	if roles.Len() > 0 {
		slog.Info("roles loaded", "roles", roles.Len())
	}
}

// checkRoles adds doctor's roles section: configs/roles.json can be read,
// each problem in it, and the users whose records name a role it doesn't
// define. A board without the file gets one note, since roles are optional.
func checkRoles(root string, add func(string, doctorSeverity, string, string)) {
	configDir := filepath.Join(root, "configs")
	data, err := os.ReadFile(perm.Path(configDir))
	// Without the file nothing is defined, but users can still carry roles,
	// groups or flags from before it went, and those are reported below.
	absent := errors.Is(err, fs.ErrNotExist)
	if err != nil && !absent {
		add("admin roles", doctorFail, "configs/roles.json can't be read: "+err.Error(), "Fix the file's permissions; until then no role grants anything.")
		return
	}
	if absent {
		data = []byte("{}")
	}
	roles, diags, err := perm.Parse(data)
	if err != nil {
		add("admin roles", doctorFail, "configs/roles.json: "+jsonErrorLocation(data, err),
			"Fix the JSON; until then no role grants anything. See docs/sysop/users/admin-roles.md.")
		return
	}
	for _, d := range diags {
		add("admin roles", doctorWarn, "configs/roles.json: "+d.String(), "See docs/sysop/users/admin-roles.md for role names and the permissions there are.")
	}
	unknownTags := checkGrantTags(configDir, roles)
	for _, msg := range unknownTags {
		add("admin roles", doctorWarn, "configs/roles.json: "+msg,
			"Correct the tag, or take it out of the grant; see docs/sysop/users/admin-roles.md.")
	}

	var users []struct {
		ID          int               `json:"id"`
		Handle      string            `json:"handle"`
		Roles       []string          `json:"roles"`
		Expiring    map[string]string `json:"expiringRoles"`
		Groups      []string          `json:"groups"`
		NamedFlags  map[string]string `json:"namedFlags"`
		DeletedUser bool              `json:"deletedUser"`
	}
	members, grouped, flagged := 0, 0, 0
	// No users.json yet is a new board; one that can't be read or decoded
	// means the members weren't checked, which the summary must not hide.
	usersChecked := true
	usersPath := filepath.Join(root, "data", "users", "users.json")
	data, err = os.ReadFile(usersPath)
	if err == nil {
		err = json.Unmarshal(data, &users)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		usersChecked = false
		add("admin roles", doctorWarn, "data/users/users.json can't be read, so users' roles and groups weren't checked: "+err.Error(), "")
	}
	if usersChecked {
		for _, u := range users {
			if u.DeletedUser {
				continue
			}
			rec := &user.User{Roles: u.Roles, ExpiringRoles: u.Expiring}
			if len(perm.RoleGrants(rec)) > 0 {
				members++
			}
			if len(perm.NormalizeRoles(u.Groups)) > 0 {
				grouped++
			}
			// Mail to "@name" goes to that handle, not the group (#1036).
			if h := strings.TrimSpace(u.Handle); strings.HasPrefix(h, "@") && roles.GroupDefined(strings.TrimPrefix(h, "@")) {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has a handle that is also group %s's mail address, so mail to %s goes to the user, not the group",
					u.Handle, u.ID, strings.TrimPrefix(h, "@"), h),
					"Rename the user (new handles can't start with @), or rename the group in configs/roles.json.")
			}
			permanent := map[string]bool{}
			for _, r := range perm.NormalizeRoles(u.Roles) {
				permanent[r] = true
			}
			// Every unreadable entry, including one a readable entry for the
			// same role under another case outlasts.
			for _, g := range perm.UnreadableRoleExpiries(rec) {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has role %s with expiry %q, which isn't an RFC 3339 time, so it grants nothing",
					u.Handle, u.ID, g.Name, g.Stored),
					"Set the expiry again in ./ue or the admin user editor.")
			}
			for _, g := range perm.RoleGrants(rec) {
				if permanent[g.Name] && hasExpiry(u.Expiring, g.Name) {
					add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has role %s both for good (roles) and until a time (expiringRoles), so the expiry is ignored",
						u.Handle, u.ID, g.Name),
						"Set the role again in ./ue or the admin user editor, which keeps it in one place.")
				}
			}
			if undefined := perm.Undefined(perm.RoleNamesOf(rec), roles); len(undefined) > 0 {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has role %s, which configs/roles.json doesn't define, so it grants nothing",
					u.Handle, u.ID, strings.Join(undefined, ", ")),
					"Define the role in configs/roles.json, or take it off the user in ./ue or the admin user editor.")
			}
			if undefined := perm.UndefinedGroups(u.Groups, roles); len(undefined) > 0 {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) is in group %s, which configs/roles.json doesn't define, so GROUP: never matches it",
					u.Handle, u.ID, strings.Join(undefined, ", ")),
					"Define the group in configs/roles.json, or take it off the user in ./ue or the admin user editor.")
			}
			flagRec := &user.User{NamedFlags: u.NamedFlags}
			grants := perm.FlagGrants(flagRec)
			if len(grants) > 0 {
				flagged++
			}
			var names []string
			for _, g := range grants {
				names = append(names, g.Name)
			}
			for _, g := range perm.UnreadableFlagExpiries(flagRec) {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has flag %s with expiry %q, which isn't an RFC 3339 time, so FLAG:%s never matches it",
					u.Handle, u.ID, g.Name, g.Stored, g.Name),
					"Set the expiry again in ./ue or the admin user editor.")
			}
			if undefined := perm.UndefinedFlags(names, roles); len(undefined) > 0 {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has flag %s, which configs/roles.json doesn't declare, so FLAG: never matches it",
					u.Handle, u.ID, strings.Join(undefined, ", ")),
					"Declare the flag in configs/roles.json, or take it off the user in ./ue or the admin user editor.")
			}
		}
	}
	if absent {
		add("admin roles", doctorInfo, "configs/roles.json is absent: no roles are defined, so admin functions are the sysop's alone", "")
		return
	}
	if len(diags) == 0 && len(unknownTags) == 0 && usersChecked {
		msg := fmt.Sprintf("%d role(s) defined, %d user(s) with a role", roles.Len(), members)
		if n := len(roles.GroupNames()); n > 0 || grouped > 0 {
			msg += fmt.Sprintf("; %d group(s) defined, %d user(s) in a group", n, grouped)
		}
		if n := len(roles.FlagNames()); n > 0 || flagged > 0 {
			msg += fmt.Sprintf("; %d flag(s) declared, %d user(s) with a named flag", n, flagged)
		}
		add("admin roles", doctorOK, msg, "")
	}
}

// checkGrantTags lists the area and conference tags roles' grants name that
// message_areas.json and conferences.json don't have. Such a tag covers
// nothing, so the grant is narrower than it reads. A file that can't be
// read is left to the checks that report on it.
func checkGrantTags(configDir string, roles *perm.Roles) []string {
	type tagged struct {
		Tag string `json:"tag"`
	}
	// tags reads a config file's tags; ok is false when it can't be read,
	// and then its tags aren't checked.
	tags := func(name string) (map[string]bool, bool) {
		var list []tagged
		data, err := os.ReadFile(filepath.Join(configDir, name))
		if err != nil || json.Unmarshal(data, &list) != nil {
			return nil, false
		}
		out := map[string]bool{}
		for _, t := range list {
			out[strings.ToUpper(strings.TrimSpace(t.Tag))] = true
		}
		return out, true
	}
	// A grant's areas are of its permission's kind: message areas for
	// messages.moderate, file areas for files.manage.
	areaFile := map[perm.AreaKind]string{perm.MessageAreas: "message_areas.json", perm.FileAreas: "file_areas.json"}
	areaTags := map[perm.AreaKind]map[string]bool{}
	haveAreas := map[perm.AreaKind]bool{}
	for k, name := range areaFile {
		areaTags[k], haveAreas[k] = tags(name)
	}
	confTags, haveConfs := tags("conferences.json")
	var out []string
	for _, role := range roles.List() {
		for _, g := range role.Permissions {
			kind := perm.KindOf(perm.Permission(g.Permission))
			for _, tag := range g.Areas {
				if haveAreas[kind] && !areaTags[kind][tag] {
					out = append(out, fmt.Sprintf("role %s: %s names %s %s, which %s doesn't have, so it covers nothing there", role.Name, g.Permission, kind, tag, areaFile[kind]))
				}
			}
			for _, tag := range g.Conferences {
				if haveConfs && !confTags[tag] {
					out = append(out, fmt.Sprintf("role %s: %s names conference %s, which conferences.json doesn't have, so it covers nothing there", role.Name, g.Permission, tag))
				}
			}
		}
	}
	return out
}

// hasExpiry reports whether expiring, a record's expiringRoles, names role
// (normalized), in any case.
func hasExpiry(expiring map[string]string, role string) bool {
	for name := range expiring {
		if perm.NormalizeRoleName(name) == role {
			return true
		}
	}
	return false
}
