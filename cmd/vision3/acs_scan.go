package main

import (
	"context"
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
)

// acsSource is one ACS string in the board's configuration: which file holds
// it, which record and field within that file, and the string itself.
type acsSource struct {
	File   string // board-relative path, slash-separated
	Record string // e.g. `command "G"`, `area GENERAL`, `conference 1`
	Field  string // e.g. "ACS", "acs_read"
	ACS    string
}

// acsFinding is the diagnostics for one source string.
type acsFinding struct {
	Source acsSource
	Diags  []acs.Diagnostic
}

// Severity is the finding's worst diagnostic.
func (f acsFinding) Severity() acs.Severity {
	for _, d := range f.Diags {
		if d.Severity == acs.Error {
			return acs.Error
		}
	}
	return acs.Warning
}

// Detail is the finding without its file, as doctor prints it under the
// file's name, e.g.
//
//	command 3 ("G"), ACS "S20 FA": column 5: no operator before "FA": put & or | between terms
func (f acsFinding) Detail() string {
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
func scanACS(root string) (findings []acsFinding, checked int) {
	for _, src := range collectACSSources(root) {
		if src.ACS != "" {
			checked++
		}
		if diags := acs.Validate(src.ACS); len(diags) > 0 {
			findings = append(findings, acsFinding{Source: src, Diags: diags})
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
				acsSource{File: "configs/message_areas.json", Record: rec, Field: "acs_write", ACS: a.ACSWrite})
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
				acsSource{File: "configs/ftn.json", Record: rec, Field: "acs_write", ACS: ac.ACSWrite})
		}
	}
	// doors.json: doors have no ACS of their own yet, only min_access_level.
	// When they do, list each door's acs here.
	return out
}

// checkACSStrings adds doctor's ACS section: one check per finding, a
// failure for a string that can't be read as a whole (it fails for every
// caller) and a warning for one whose meaning a stricter ACS policy would
// change; one passing check when everything reads cleanly.
func checkACSStrings(root string, add func(string, doctorSeverity, string, string)) {
	findings, checked := scanACS(root)
	if len(findings) == 0 {
		add("ACS strings", doctorOK, fmt.Sprintf("%d strings checked", checked), "")
		return
	}
	for _, f := range findings {
		status, fix := doctorWarn, "It works as it always has for now, but a stricter ACS check will change what it does. Fix it; see docs/sysop/reference/acs.md."
		if f.Severity() == acs.Error {
			status, fix = doctorFail, "This string denies every caller. Fix it; see docs/sysop/reference/acs.md."
		}
		add("ACS: "+f.Source.File, status, f.Detail(), fix)
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
// didn't have, errors at error level and warnings at warn, with the text
// doctor prints. It never stops the board and never changes a string: a bad
// string keeps failing the way it always has. Called at startup and after a
// config reload.
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
		level := slog.LevelWarn
		if f.Severity() == acs.Error {
			level = slog.LevelError
		}
		slog.Log(context.Background(), level, "ACS string problem: "+f.String(), "file", f.Source.File)
	}
	acsLogged.seen = seen
}
