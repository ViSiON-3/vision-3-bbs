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
	if errors.Is(err, fs.ErrNotExist) {
		add("admin roles", doctorInfo, "configs/roles.json is absent: no roles are defined, so admin functions are the sysop's alone", "")
		return
	}
	if err != nil {
		add("admin roles", doctorFail, "configs/roles.json can't be read: "+err.Error(), "Fix the file's permissions; until then no role grants anything.")
		return
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
		ID          int      `json:"id"`
		Handle      string   `json:"handle"`
		Roles       []string `json:"roles"`
		DeletedUser bool     `json:"deletedUser"`
	}
	members := 0
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
		add("admin roles", doctorWarn, "data/users/users.json can't be read, so users' roles weren't checked: "+err.Error(), "")
	}
	if usersChecked {
		for _, u := range users {
			if u.DeletedUser {
				continue
			}
			if len(perm.NormalizeRoles(u.Roles)) > 0 {
				members++
			}
			if undefined := perm.Undefined(u.Roles, roles); len(undefined) > 0 {
				add("admin roles", doctorWarn, fmt.Sprintf("%s (#%d) has role %s, which configs/roles.json doesn't define, so it grants nothing",
					u.Handle, u.ID, strings.Join(undefined, ", ")),
					"Define the role in configs/roles.json, or take it off the user in ./ue or the admin user editor.")
			}
		}
	}
	if len(diags) == 0 && len(unknownTags) == 0 && usersChecked {
		add("admin roles", doctorOK, fmt.Sprintf("%d role(s) defined, %d user(s) with a role", roles.Len(), members), "")
	}
}

// checkGrantTags lists the area and conference tags roles' grants name that
// message_areas.json and conferences.json don't have. Such a tag covers
// nothing, so the grant is narrower than it reads. A file that can't be
// read is left to the checks that report on it.
func checkGrantTags(configDir string, roles *perm.Roles) []string {
	var areas []struct {
		Tag string `json:"tag"`
	}
	var confs []struct {
		Tag string `json:"tag"`
	}
	read := func(name string, v any) bool {
		data, err := os.ReadFile(filepath.Join(configDir, name))
		return err == nil && json.Unmarshal(data, v) == nil
	}
	haveAreas, haveConfs := read("message_areas.json", &areas), read("conferences.json", &confs)
	areaTags, confTags := map[string]bool{}, map[string]bool{}
	for _, a := range areas {
		areaTags[strings.ToUpper(strings.TrimSpace(a.Tag))] = true
	}
	for _, c := range confs {
		confTags[strings.ToUpper(strings.TrimSpace(c.Tag))] = true
	}
	var out []string
	for _, role := range roles.List() {
		for _, g := range role.Permissions {
			for _, tag := range g.Areas {
				if haveAreas && !areaTags[tag] {
					out = append(out, fmt.Sprintf("role %s: %s names area %s, which message_areas.json doesn't have, so it covers nothing there", role.Name, g.Permission, tag))
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
