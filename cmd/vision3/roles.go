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

	var users []struct {
		ID          int      `json:"id"`
		Handle      string   `json:"handle"`
		Roles       []string `json:"roles"`
		DeletedUser bool     `json:"deletedUser"`
	}
	members := 0
	if data, err := os.ReadFile(filepath.Join(root, "data", "users", "users.json")); err == nil && json.Unmarshal(data, &users) == nil {
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
	if len(diags) == 0 {
		add("admin roles", doctorOK, fmt.Sprintf("%d role(s) defined, %d user(s) with a role", roles.Len(), members), "")
	}
}
