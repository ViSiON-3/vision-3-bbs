package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
)

// acsSource is one ACS string in the board's configuration: which file holds
// it, which record and field within that file, and the string itself.
type acsSource struct {
	File   string // board-relative path, slash-separated
	Record string // e.g. `command "G"`, `area GENERAL`, `conference 1`
	Field  string // e.g. "ACS", "acs_read"
	ACS    string
	// Headless says where the string is checked without a caller's session
	// ("replies uploaded through the QWK API"), "" when it always has one.
	// There, a string with L or A in it fails as a whole.
	Headless string
}

// acsFinding is the diagnostics for one source string, or, for a string
// that reads cleanly, a note that it uses L or A where it is checked
// without a session.
type acsFinding struct {
	Source acsSource
	Diags  []acs.Diagnostic
	// NoSession is set for a valid string with L or A in it from a
	// Headless source: it denies every caller there.
	NoSession bool
	// Undefined lists the roles and groups a valid string's ROLE and GROUP
	// terms name that configs/roles.json doesn't define, as the terms are
	// written (ROLE:name, GROUP:name); any makes it deny every caller.
	Undefined []string
}

// failing reports whether the string denies every caller everywhere (it
// has diagnostics or names an undefined role), rather than only where it has
// no session.
func (f acsFinding) failing() bool { return len(f.Diags) > 0 || len(f.Undefined) > 0 }

// Detail is the finding without its file, as doctor prints it under the
// file's name, e.g.
//
//	command 3 ("G"), ACS "S20 FA": column 5: no operator before "FA": put & or | between terms
func (f acsFinding) Detail() string {
	if f.NoSession {
		return fmt.Sprintf("%s, %s %q: uses L or A, which fail the whole string for %s, where there is no session",
			f.Source.Record, f.Source.Field, f.Source.ACS, f.Source.Headless)
	}
	if len(f.Undefined) > 0 {
		return fmt.Sprintf("%s, %s %q: names %s, which configs/roles.json doesn't define, so it fails for every caller",
			f.Source.Record, f.Source.Field, f.Source.ACS, strings.Join(f.Undefined, ", "))
	}
	parts := make([]string, len(f.Diags))
	for i, d := range f.Diags {
		parts[i] = d.String()
	}
	return fmt.Sprintf("%s, %s %q: %s", f.Source.Record, f.Source.Field, f.Source.ACS, strings.Join(parts, "; "))
}

// String is the finding as the BBS logs it: Detail after the file.
func (f acsFinding) String() string {
	return f.Source.File + ", " + f.Detail()
}

// scanACS checks every ACS string the board declares and returns the
// problems found, ordered by file, and how many non-empty strings it checked.
// It reads the files itself, so it covers menus nobody has opened; a file it
// can't read or parse is left to the checks that report on that file.
//
// ROLE and GROUP terms are checked against configs/roles.json as it is on
// disk; a file that can't be read defines none, as the BBS treats it.
func scanACS(root string) (findings []acsFinding, checked int) {
	roles, _, err := perm.Load(filepath.Join(root, "configs"))
	if err != nil {
		roles = nil
	}
	for _, src := range collectACSSources(root) {
		if src.ACS != "" {
			checked++
		}
		if diags := acs.Validate(src.ACS); len(diags) > 0 {
			findings = append(findings, acsFinding{Source: src, Diags: diags})
			continue
		}
		e := acs.Parse(src.ACS)
		var undefined []string
		for _, r := range perm.Undefined(e.RoleNames(), roles) {
			undefined = append(undefined, "ROLE:"+r)
		}
		for _, g := range perm.UndefinedGroups(e.GroupNames(), roles) {
			undefined = append(undefined, "GROUP:"+g)
		}
		if len(undefined) > 0 {
			findings = append(findings, acsFinding{Source: src, Undefined: undefined})
		} else if src.Headless != "" && src.ACS != "" && e.UsesSession() {
			findings = append(findings, acsFinding{Source: src, NoSession: true})
		}
	}
	return findings, checked
}

// collectACSSources lists every ACS string declared under root.
func collectACSSources(root string) []acsSource {
	var out []acsSource
	rel := func(path string) string {
		if r, err := filepath.Rel(root, path); err == nil {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(path)
	}
	readJSON := func(path string, v any) bool {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return false
		}
		return json.Unmarshal(data, v) == nil
	}
	configDir := filepath.Join(root, "configs")

	// Menu sets: each directory under menus/, with its overlay under menus.d/.
	// The merged view is what the BBS loads, so a shadowed stock file is not
	// checked.
	sets, _ := os.ReadDir(filepath.Join(root, "menus"))
	for _, set := range sets {
		if !set.IsDir() {
			continue
		}
		ms := menuset.FromPath(filepath.Join(root, "menus", set.Name()))
		// The BBS opens a menu's files by name, NAME.MNU, NAME.CFG and NAME.ANS
		// with NAME in upper case, through Locate. So the listings only
		// suggest names; each file is then located with the loader's own
		// spelling. On a case-sensitive filesystem a stray main.cfg is not
		// what LoadCommands("MAIN") opens and is skipped; where names fold
		// case, Locate finds it just as the loader does.
		names := map[string]bool{}
		for _, sub := range []string{"mnu", "cfg"} {
			entries, _ := ms.ReadDir(sub)
			for _, e := range entries {
				ext := filepath.Ext(e.Name)
				if strings.EqualFold(ext, "."+sub) {
					names[strings.ToUpper(strings.TrimSuffix(e.Name, ext))] = true
				}
			}
		}
		sorted := make([]string, 0, len(names))
		for name := range names {
			sorted = append(sorted, name)
		}
		sort.Strings(sorted)
		locate := func(sub, file string) (string, bool) {
			path, _, ok, err := ms.Locate(sub, file)
			return path, ok && err == nil
		}
		isMenu := map[string]bool{}
		for _, name := range sorted {
			path, ok := locate("mnu", name+".MNU")
			if !ok {
				continue
			}
			isMenu[name] = true
			var m menu.MenuRecord
			if readJSON(path, &m) {
				out = append(out, acsSource{File: rel(path), Record: "menu " + name, Field: "ACS", ACS: m.ACS})
			}
		}
		for _, name := range sorted {
			path, ok := locate("cfg", name+".CFG")
			if !ok {
				continue
			}
			var cmds []menu.CommandRecord
			if !readJSON(path, &cmds) {
				continue
			}
			for i, c := range cmds {
				rec := fmt.Sprintf("command %d (%q)", i+1, c.Keys)
				out = append(out, acsSource{File: rel(path), Record: rec, Field: "ACS", ACS: c.ACS})
			}
		}
		// {{acs}} regions work only in the screen named after a menu. The art
		// is read as the renderer reads it, SAUCE record stripped.
		for _, name := range sorted {
			if !isMenu[name] {
				continue
			}
			path, ok := locate("ansi", name+".ANS")
			if !ok {
				continue
			}
			data, err := ansi.GetAnsiFileContent(path)
			if err != nil {
				continue
			}
			for _, rc := range menu.RegionConditions(data) {
				out = append(out, acsSource{File: rel(path), Record: fmt.Sprintf("region on line %d", rc.Line), Field: "{{acs}}", ACS: rc.ACS})
			}
		}
	}

	var msgAreas []message.MessageArea
	if readJSON(filepath.Join(configDir, "message_areas.json"), &msgAreas) {
		for _, a := range msgAreas {
			rec := "area " + a.Tag
			out = append(out,
				acsSource{File: "configs/message_areas.json", Record: rec, Field: "acs_read", ACS: a.ACSRead},
				acsSource{File: "configs/message_areas.json", Record: rec, Field: "acs_write", ACS: a.ACSWrite, Headless: qwkAPIReplies})
		}
	}
	// File-echo areas are file areas like any other; the wizard and helper
	// options that seed their ACS end up here.
	var fileAreas []file.FileArea
	if readJSON(filepath.Join(configDir, "file_areas.json"), &fileAreas) {
		for _, a := range fileAreas {
			rec := "area " + a.Tag
			if a.IsFileEcho() {
				rec += " (file echo " + a.FileEcho + ")"
			}
			out = append(out,
				acsSource{File: "configs/file_areas.json", Record: rec, Field: "acs_list", ACS: a.ACSList},
				acsSource{File: "configs/file_areas.json", Record: rec, Field: "acs_upload", ACS: a.ACSUpload},
				acsSource{File: "configs/file_areas.json", Record: rec, Field: "acs_download", ACS: a.ACSDownload})
		}
	}
	var confs []conference.Conference
	if readJSON(filepath.Join(configDir, "conferences.json"), &confs) {
		for _, c := range confs {
			out = append(out, acsSource{File: "configs/conferences.json", Record: fmt.Sprintf("conference %d (%s)", c.ID, c.Tag), Field: "acs", ACS: c.ACS})
		}
	}
	var server struct {
		DoorCategories []config.DoorCategory `json:"doorCategories"`
	}
	if readJSON(filepath.Join(configDir, "config.json"), &server) {
		for _, c := range server.DoorCategories {
			out = append(out, acsSource{File: "configs/config.json", Record: "door category " + c.Code, Field: "acs", ACS: c.ACS})
		}
	}
	var ftn struct {
		Networks map[string]config.FTNNetworkConfig `json:"networks"`
	}
	if readJSON(filepath.Join(configDir, "ftn.json"), &ftn) {
		names := make([]string, 0, len(ftn.Networks))
		for name := range ftn.Networks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			ac := ftn.Networks[name].AutoCreate
			rec := "network " + name + " auto_create"
			out = append(out,
				acsSource{File: "configs/ftn.json", Record: rec, Field: "acs_read", ACS: ac.ACSRead},
				acsSource{File: "configs/ftn.json", Record: rec, Field: "acs_write", ACS: ac.ACSWrite, Headless: qwkAPIReplies})
		}
	}
	// roles.json: who may mail each user group (#1036).
	var roles struct {
		Groups []struct {
			Name    string `json:"name"`
			MailACS string `json:"mail_acs"`
		} `json:"groups"`
	}
	if readJSON(filepath.Join(configDir, "roles.json"), &roles) {
		for i, g := range roles.Groups {
			out = append(out, acsSource{File: "configs/roles.json", Record: fmt.Sprintf("group %d (%s)", i+1, g.Name),
				Field: "mail_acs", ACS: strings.TrimSpace(g.MailACS)})
		}
	}
	// doors.json, read as LoadDoors reads it: a blank or all-whitespace acs
	// is no rule.
	var doors []config.DoorConfig
	if readJSON(filepath.Join(configDir, "doors.json"), &doors) {
		for i, d := range doors {
			out = append(out, acsSource{File: "configs/doors.json", Record: fmt.Sprintf("door %d (%s)", i+1, d.Code),
				Field: "acs", ACS: strings.TrimSpace(d.ACS)})
		}
	}
	return out
}

// qwkAPIReplies is where a message area's acs_write is checked without a
// session: menu.QWKWriteAuthorizer, for replies the QWK API takes in.
const qwkAPIReplies = "replies uploaded through the QWK API"

// checkACSStrings adds doctor's ACS section: a failing check for each string
// with a problem, since it denies every caller; a warning for each string
// that uses L or A where it is checked without a session; one passing check
// when there are neither.
func checkACSStrings(root string, add func(string, doctorSeverity, string, string)) {
	findings, checked := scanACS(root)
	if len(findings) == 0 {
		add("ACS strings", doctorOK, fmt.Sprintf("%d strings checked", checked), "")
		return
	}
	for _, f := range findings {
		if len(f.Undefined) > 0 {
			add("ACS: "+f.Source.File, doctorFail, f.Detail(),
				"This string denies every caller, sysops included. Define the role or group in configs/roles.json or correct its name; see docs/sysop/users/admin-roles.md.")
			continue
		}
		if f.failing() {
			add("ACS: "+f.Source.File, doctorFail, f.Detail(),
				"This string denies every caller, sysops included. Fix it; see docs/sysop/reference/acs.md.")
			continue
		}
		add("ACS: "+f.Source.File, doctorWarn, f.Detail(),
			"Callers in a session are checked as usual, but every reply uploaded through the QWK API is refused. Take L and A out if those replies should go through; see docs/sysop/reference/acs.md.")
	}
}

// acsLogged is the set of findings in the last scan, so each problem is
// logged once while it stays: at startup, and again only when a reload
// brings a new one. Each scan replaces the set, so a string that is fixed
// and later comes back is logged again.
var acsLogged = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

// logACSFindings scans the board and logs each problem the previous scan
// didn't have, with the text doctor prints: a string with a problem at
// error level, one using L or A without a session at warn. It never stops
// the board and never changes a string: a bad string denies every caller
// until it is fixed. Called at startup and after a config reload.
func logACSFindings(root string) {
	findings, _ := scanACS(root)
	acsLogged.Lock()
	defer acsLogged.Unlock()
	seen := make(map[string]bool, len(findings))
	for _, f := range findings {
		key := f.Source.File + "\x00" + f.Source.Record + "\x00" + f.Source.Field + "\x00" + f.Source.ACS
		seen[key] = true
		if acsLogged.seen[key] {
			continue
		}
		if f.failing() {
			slog.Error("ACS string problem: "+f.String(), "file", f.Source.File)
		} else {
			slog.Warn("ACS string problem: "+f.String(), "file", f.Source.File)
		}
	}
	acsLogged.seen = seen
}
