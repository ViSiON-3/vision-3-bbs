package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/rivo/uniseg"
)

// `vision3 doctor --theme <name|dir>` checks one theme: a theme under
// menus/ by name, or a directory anywhere, such as one a sysop has been sent
// and not yet installed. It reports what the theme needs and lacks, files
// that don't load, and what the shipped theme has that it lacks: a
// self-contained theme carries its own copy of every screen, so it does not
// pick up the screens and commands a later release adds to the shipped one.

// themeScreenDirs are the directories of a menu set whose files are drawn on
// the caller's screen.
var themeScreenDirs = []string{"ansi", "bar", "templates"}

// runThemeDoctor is doctor --theme: it checks the theme arg names, with the
// board at root for doors.json and the shipped theme, and prints the report
// as doctor does. The exit code is doctor's: 2 for a failure, 1 for a
// warning, else 0.
func runThemeDoctor(root, arg string, jsonOutput bool, stdout, stderr io.Writer) int {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)

	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	dir, err := themeDirFor(root, arg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "vision3 doctor: %v\n", err)
		return 2
	}
	report := doctorReport{Root: dir}
	add := func(name string, status doctorSeverity, message, fix string) {
		// The board-wide names the shared checks use ("menu references",
		// "theme.json") are named for the theme, so they print with it.
		if doctorSection(name) == "BOARD FILES" {
			name = themeCheckLabel(dir, name)
		}
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	}
	checkTheme(dir, filepath.Join(root, "configs"), add)
	addThemeGaps(root, dir, add)
	for _, check := range report.Checks {
		switch check.Status {
		case doctorWarn:
			report.Summary.Warnings++
		case doctorFail:
			report.Summary.Failures++
		}
	}
	if jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "vision3 doctor: %v\n", err)
			return 2
		}
	} else if err := printDoctorText(stdout, report); err != nil {
		_, _ = fmt.Fprintf(stderr, "vision3 doctor: %v\n", err)
		return 2
	}
	switch {
	case report.Summary.Failures > 0:
		return 2
	case report.Summary.Warnings > 0:
		return 1
	}
	return 0
}

// themeDirFor returns the directory doctor --theme checks: menus/<arg> under
// root when arg is a theme name installed there, else arg as a directory.
func themeDirFor(root, arg string) (string, error) {
	if arg == "" {
		return "", errors.New("--theme needs a theme name or directory")
	}
	if menuset.ValidThemeName(arg) == nil {
		if dir, err := menuset.CheckTheme(root, arg); err == nil {
			return dir, nil
		}
	}
	dir, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("theme %q: not a theme under %s and not a directory", arg, filepath.Join(root, menuset.ThemesDir))
	}
	if !info.IsDir() {
		return "", fmt.Errorf("theme %q: %s is not a directory", arg, dir)
	}
	return dir, nil
}

// themeCheckLabel names a check of the theme at dir; doctor prints these
// in its THEME section.
func themeCheckLabel(dir, what string) string {
	return "theme " + filepath.Base(dir) + ": " + what
}

// checkTheme adds the checks of the theme at dir: its menus and command
// files parse and their GOTO, DOOR, RUN and MSGAREA targets resolve
// (against the board's configs at configDir), its chat screens and
// theme.json load, and checkThemeContents.
func checkTheme(dir, configDir string, add func(string, doctorSeverity, string, string)) {
	checkMenuFiles(dir, configDir, add)
	checkChatScreens(dir, add)
	checkThemeContents(dir, configDir, add)
}

// checkThemeContents adds the checks doctor makes of a theme and of no
// other menu set: a MAIN menu, a screen for every menu that has commands
// and no script to draw it, the scripts its menus and theme.json name, and
// its strings.json.
func checkThemeContents(dir, configDir string, add func(string, doctorSeverity, string, string)) {
	set := menuset.FromPath(dir)

	// MAIN is where every caller starts; without it Konfig does not offer
	// the theme, and the board can't run on it.
	if ok, err := set.Exists("mnu", "MAIN.MNU"); err != nil || !ok {
		add(themeCheckLabel(dir, "menus"), doctorFail, "no mnu/MAIN.MNU: callers can't choose the theme and the board can't run on it",
			"Add mnu/MAIN.MNU, with cfg/MAIN.CFG and ansi/MAIN.ANS.")
	}

	doors, doorsErr := config.LoadDoors(filepath.Join(configDir, "doors.json"))
	themeDefault := ""
	if theme, err := config.LoadThemeConfig(dir); err == nil {
		themeDefault = theme.MenuScript
	}
	scriptProblems := 0
	checkScript := func(where, field, script string) {
		words := strings.Fields(script)
		if len(words) == 0 || strings.EqualFold(words[0], "NONE") {
			return
		}
		problem := themeScriptProblem(set, words[0], doors, doorsErr)
		if problem == "" {
			return
		}
		scriptProblems++
		add(themeCheckLabel(dir, "scripts"), doctorWarn, fmt.Sprintf("%s: %s %s", where, field, problem),
			"Add the script or door, or correct the name; until then the menu shows its screen.")
	}
	checkScript("theme.json", "menuScript", themeDefault)

	menus, err := set.ReadDir("mnu")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		add(themeCheckLabel(dir, "menus"), doctorFail, err.Error(), "Check the menu directory and its permissions.")
	}
	missingScreens := 0
	for _, entry := range menus {
		name, ok := strings.CutSuffix(strings.ToUpper(entry.Name), ".MNU")
		if !ok {
			continue
		}
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			continue // checkMenuFiles reports it
		}
		var rec menu.MenuRecord
		if json.Unmarshal(data, &rec) != nil {
			continue // checkMenuFiles reports it
		}
		checkScript("mnu/"+entry.Name, "SCRIPT", rec.Script)
		script := rec.Script
		if strings.TrimSpace(script) == "" {
			script = themeDefault
		}
		if words := strings.Fields(script); len(words) > 0 && !strings.EqualFold(words[0], "NONE") {
			continue // the script draws the menu
		}
		// A menu with no commands is one the BBS builds itself, as the
		// shipped DOORMENU and TWITLIST are.
		if ok, _ := set.Exists("cfg", name+".CFG"); !ok {
			continue
		}
		if ok, _ := set.Exists("ansi", name+".ANS"); !ok {
			missingScreens++
			add(themeCheckLabel(dir, "screens"), doctorWarn, fmt.Sprintf("menu %s has commands but no screen ansi/%s.ANS and no SCRIPT", name, name),
				fmt.Sprintf("Add ansi/%s.ANS, or a SCRIPT to draw the menu; callers see an error line in its place.", name))
		}
	}
	if missingScreens == 0 && len(menus) > 0 {
		add(themeCheckLabel(dir, "screens"), doctorOK, "every menu with commands has a screen or a script", "")
	}
	if scriptProblems == 0 {
		add(themeCheckLabel(dir, "scripts"), doctorOK, "the scripts its menus name are there", "")
	}

	checkThemeStrings(set, dir, add)
	checkThemeLabel(dir, configDir, add)
}

// checkThemeLabel adds the name and description callers see for the theme
// at dir, with the board's theme.labels applied, and warns about one
// longer than Konfig shows.
func checkThemeLabel(dir, configDir string, add func(string, doctorSeverity, string, string)) {
	name := themeCheckLabel(dir, "name")
	// A missing config.json is an empty selection; one that can't be read
	// leaves the board's labels unknown, so say so rather than report the
	// theme's own as what callers see.
	board, err := config.ThemeSelectionFromConfigDir(configDir)
	if err != nil {
		add(name, doctorWarn, "configs/config.json theme.labels not applied: "+err.Error(),
			"Fix configs/config.json; until then the label below may not be what callers see.")
		board = config.ThemeSelection{}
	}
	label, from := config.ThemeLabelWithSources(dir, board)
	if from.Name == "" && from.Description == "" {
		add(name, doctorInfo, fmt.Sprintf("states no name or description; shown as %q", label.Name),
			`Add "name" and "description" to theme.json, or label it in configs/config.json theme.labels.`)
	} else {
		shown := fmt.Sprintf("shown as %q", label.Name)
		if label.Description != "" {
			shown += ": " + label.Description
		}
		add(name, doctorOK, fmt.Sprintf("%s (%s)", shown, themeLabelSourceText(from)), "")
	}
	// The name shown may be the directory's, which can be as long as any.
	if w := uniseg.StringWidth(label.Name); w > config.MaxThemeName {
		add(name, doctorWarn, fmt.Sprintf("name is %d columns; Konfig's list shows %d", w, config.MaxThemeName),
			"Shorten the name; the list cuts it.")
	}
	if w := uniseg.StringWidth(label.Description); w > config.MaxThemeDescription {
		add(name, doctorWarn, fmt.Sprintf("description is %d columns; Konfig shows %d", w, config.MaxThemeDescription),
			"Shorten the description to one line; Konfig cuts it.")
	}
}

// themeLabelSourceText says where a label's fields come from: one source
// when they share it, else each field's, with "directory name" for a name
// neither sets.
func themeLabelSourceText(from config.ThemeLabelSources) string {
	if from.Name == from.Description {
		return from.Name
	}
	nameFrom := from.Name
	if nameFrom == "" {
		nameFrom = "directory name"
	}
	if from.Description == "" {
		return "name: " + nameFrom
	}
	return "name: " + nameFrom + "; description: " + from.Description
}

// checkThemeLabelTargets warns about each configs/config.json theme.labels
// entry that names no theme under menus/, since it labels nothing.
func checkThemeLabelTargets(root string, sel config.ThemeSelection, add func(string, doctorSeverity, string, string)) {
	dirs := make([]string, 0, len(sel.Labels))
	for d := range sel.Labels {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if _, err := menuset.CheckTheme(root, d); err != nil {
			add("theme", doctorWarn, fmt.Sprintf("configs/config.json theme.labels names %q, which is not a theme under menus/", d),
				"Correct the directory name, or remove the entry.")
		}
	}
}

// themeScriptProblem says what is wrong with the script a menu SCRIPT or
// menuScript names, as the menu loop resolves it, or "" when it will run:
// a .js names a file in the theme's scripts/, anything else a v3_script door
// in the board's doors.json.
func themeScriptProblem(set menuset.Set, word string, doors map[string]config.DoorConfig, doorsErr error) string {
	if strings.HasSuffix(strings.ToLower(word), ".js") {
		if strings.ContainsAny(word, `/\`) || word == "." || word == ".." || filepath.IsAbs(word) || filepath.VolumeName(word) != "" {
			return fmt.Sprintf("%q is not a file name; a theme script is a file in the theme's scripts/", word)
		}
		if ok, err := set.Exists("scripts", word); err != nil || !ok {
			return fmt.Sprintf("names theme script %q, which is not in scripts/", word)
		}
		return ""
	}
	code := strings.ToUpper(word)
	if doorsErr != nil {
		return fmt.Sprintf("names door %q, and configs/doors.json can't be read: %v", code, doorsErr)
	}
	door, ok := doors[code]
	if !ok {
		return fmt.Sprintf("names door %q, which configs/doors.json does not have", code)
	}
	if door.Type != "v3_script" {
		return fmt.Sprintf("names door %q, which is not a v3_script door (type %q)", code, door.Type)
	}
	return ""
}

// checkThemeStrings adds a check of the theme's strings.json, in each
// layer that has one: it parses, and each value fits its call site.
func checkThemeStrings(set menuset.Set, dir string, add func(string, doctorSeverity, string, string)) {
	layers := []menuset.Layer{menuset.LayerBase}
	if set.HasOverlay() {
		layers = append(layers, menuset.LayerOverlay)
	}
	for _, layer := range layers {
		path := set.Path(layer, config.ThemeStringsFile)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(dir)), filepath.Base(dir), config.ThemeStringsFile))
		if layer == menuset.LayerOverlay {
			rel = filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(dir))+menuset.OverlaySuffix, filepath.Base(dir), config.ThemeStringsFile))
		}
		_, problems, err := config.ReadStringsLayer(path)
		if err != nil {
			add(themeCheckLabel(dir, "strings"), doctorFail, fmt.Sprintf("%s: %v", rel, err),
				"Fix the JSON; until then callers on the theme see the board's strings.")
			continue
		}
		for _, p := range problems {
			add(themeCheckLabel(dir, "strings"), doctorWarn, fmt.Sprintf("%s: %s: %s", rel, p.Key, p.Detail),
				"Correct the value; the value beneath it prints until then.")
		}
		if len(problems) == 0 {
			add(themeCheckLabel(dir, "strings"), doctorOK, rel+" loads", "")
		}
	}
}

// themeGapList is what the shipped theme has that a theme lacks: menus by
// name, screens by path in the menu set, and menu commands as CMD values,
// upper-cased, each list sorted.
type themeGapList struct {
	Menus    []string
	Screens  []string
	Commands []string
}

func (g themeGapList) empty() bool {
	return len(g.Menus)+len(g.Screens)+len(g.Commands) == 0
}

// themeGaps compares the theme at dir, with its overlay, against the
// shipped theme at stock as it ships, without its overlay.
func themeGaps(stock, dir string) (themeGapList, error) {
	var gaps themeGapList
	shipped := menuset.Bare(stock)
	theme := menuset.FromPath(dir)
	names := func(set menuset.Set, sub string) ([]menuset.Entry, error) {
		entries, err := set.ReadDir(sub)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return entries, err
	}

	menus, err := names(shipped, "mnu")
	if err != nil {
		return gaps, err
	}
	for _, e := range menus {
		if menuset.IsPreloginFile("mnu", e.Name) {
			continue // the board's, not a theme's
		}
		if name, ok := strings.CutSuffix(e.Name, ".MNU"); ok {
			if has, _ := theme.Exists("mnu", e.Name); !has {
				gaps.Menus = append(gaps.Menus, name)
			}
		}
	}
	for _, sub := range themeScreenDirs {
		entries, err := names(shipped, sub)
		if err != nil {
			return gaps, err
		}
		for _, e := range entries {
			if menuset.IsPreloginFile(sub, e.Name) {
				continue
			}
			if has, _ := theme.Exists(sub, e.Name); !has {
				gaps.Screens = append(gaps.Screens, sub+"/"+e.Name)
			}
		}
	}

	stockCommands, err := themeCommands(shipped)
	if err != nil {
		return gaps, err
	}
	themeCmds, err := themeCommands(theme)
	if err != nil {
		return gaps, err
	}
	for _, c := range stockCommands {
		if !slices.Contains(themeCmds, c) {
			gaps.Commands = append(gaps.Commands, c)
		}
	}
	sort.Strings(gaps.Menus)
	sort.Strings(gaps.Screens)
	return gaps, nil
}

// themeCommands returns the distinct commands of a menu set's cfg/*.CFG
// files, upper-cased with their spacing normalised, sorted. A file that
// doesn't parse is skipped; checkMenuFiles reports it.
func themeCommands(set menuset.Set) ([]string, error) {
	entries, err := set.ReadDir("cfg")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.Name), ".CFG") || menuset.IsPreloginFile("cfg", e.Name) {
			continue
		}
		data, err := os.ReadFile(e.Path)
		if err != nil {
			continue
		}
		var records []menu.CommandRecord
		if json.Unmarshal(data, &records) != nil {
			continue
		}
		for _, r := range records {
			if c := strings.Join(strings.Fields(strings.ToUpper(r.Command)), " "); c != "" {
				seen[c] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// addThemeGaps adds doctor --theme's comparison of the theme at dir with
// the shipped theme under root: a line for each kind of thing the theme
// lacks, listing them.
func addThemeGaps(root, dir string, add func(string, doctorSeverity, string, string)) {
	stock := menuset.ThemeDir(root, menuset.DefaultTheme)
	label := themeCheckLabel(dir, "compared with "+menuset.DefaultTheme)
	if same, _ := sameDir(stock, dir); same {
		add(label, doctorInfo, "is the shipped theme", "")
		return
	}
	if info, err := os.Stat(stock); err != nil || !info.IsDir() {
		add(label, doctorInfo, fmt.Sprintf("no %s/%s to compare with", menuset.ThemesDir, menuset.DefaultTheme), "")
		return
	}
	gaps, err := themeGaps(stock, dir)
	if err != nil {
		add(label, doctorWarn, "can't compare: "+err.Error(), "Check the permissions of both themes' directories.")
		return
	}
	if gaps.empty() {
		add(label, doctorInfo, "has every menu, screen and command the shipped theme has", "")
		return
	}
	fix := "Copy what the theme should have from " + filepath.ToSlash(filepath.Join(menuset.ThemesDir, menuset.DefaultTheme)) + ", drawn in the theme's style; leave out what it does without on purpose."
	for _, part := range []struct {
		one, other string
		items      []string
	}{{"menu", "menus", gaps.Menus}, {"screen", "screens", gaps.Screens}, {"command", "commands", gaps.Commands}} {
		if len(part.items) > 0 {
			add(label, doctorInfo, fmt.Sprintf("lacks %s: %s", countOf(len(part.items), part.one, part.other), strings.Join(part.items, ", ")), fix)
		}
	}
}

// themeGapsNote adds the board doctor's note, under its theme line, of how
// much of the shipped theme the board's theme lacks, with the command that
// lists it. The shipped theme itself gets no note.
func themeGapsNote(root, dir, name string, add func(string, doctorSeverity, string, string)) {
	if name == menuset.DefaultTheme {
		return
	}
	stock := menuset.ThemeDir(root, menuset.DefaultTheme)
	if info, err := os.Stat(stock); err != nil || !info.IsDir() {
		return
	}
	gaps, err := themeGaps(stock, dir)
	if err != nil || gaps.empty() {
		return
	}
	var parts []string
	for _, part := range []struct {
		n          int
		one, other string
	}{{len(gaps.Menus), "menu", "menus"}, {len(gaps.Screens), "screen", "screens"}, {len(gaps.Commands), "command", "commands"}} {
		if part.n > 0 {
			parts = append(parts, countOf(part.n, part.one, part.other))
		}
	}
	add("theme", doctorInfo, fmt.Sprintf("%s lacks %s of the shipped theme %s", name, joinAnd(parts), menuset.DefaultTheme),
		"Run ./vision3 doctor --theme "+name+" to list them.")
}

// countOf is n with the noun for it: "1 screen", "2 screens".
func countOf(n int, one, other string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, other)
}

// joinAnd joins parts as "a, b and c".
func joinAnd(parts []string) string {
	if len(parts) <= 1 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// sameDir reports whether a and b are the same directory.
func sameDir(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}
