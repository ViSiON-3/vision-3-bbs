package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// checkPrelogin reports the pre-login screens: the login matrix and,
// when it is on, the connection check, read from the board's prelogin/
// directory, else the shipped stock files in menus/v3, never from a theme.
// A matrix file or the connection check art that is missing or unreadable
// is a warning: the matrix is then skipped (without a command list it is
// shown but no choice acts), or the built-in check prompt shown. An optional file (picture, effects, tones, strings.json) is a
// warning only when it is there and can't be read.
func checkPrelogin(root string, serverCfg config.ServerConfig, serverErr error, add func(string, doctorSeverity, string, string)) {
	const name = "pre-login"
	set := menuset.PreloginFiles(root)
	where := fmt.Sprintf("%s/ or %s", menuset.PreloginDir, path.Join(menuset.ThemesDir, menuset.DefaultTheme))
	warned := false
	warn := func(msg, fix string) {
		warned = true
		add(name, doctorWarn, msg, fix)
	}
	fromBoard := 0

	type preloginFile struct {
		sub, file string
		required  bool
		missing   string // what callers get without it
	}
	files := []preloginFile{
		{"bar", "PDMATRIX.BAR", true, "the login matrix is skipped"},
		{"cfg", "PDMATRIX.CFG", true, "the login matrix is shown but none of its choices does anything"},
		{"ansi", "PDMATRIX.ANS", true, "the login matrix is skipped"},
		{"ansi", "PDMATRIX.PNG", false, ""},
		{"ansi", "PDMATRIX.EFX", false, ""},
		{"ansi", "PDMATRIX.MOVE.WAV", false, ""},
		{"ansi", "PDMATRIX.CHOOSE.WAV", false, ""},
	}
	if serverErr == nil && serverCfg.EnableChallengeGate && serverCfg.ChallengeGateFile != "" {
		files = append(files, preloginFile{"ansi", serverCfg.ChallengeGateFile, true, "callers get the built-in connection check prompt"})
	}
	for _, f := range files {
		rel := f.sub + "/" + f.file
		p, layer, ok, err := set.Locate(f.sub, f.file)
		if err != nil {
			warn(fmt.Sprintf("%s can't be read: %v", rel, err), "Fix or remove "+p+".")
			continue
		}
		if !ok {
			if f.required {
				warn(fmt.Sprintf("%s is in neither %s; %s", rel, where, f.missing),
					fmt.Sprintf("Put %s in %s, or restore the shipped one in %s.", f.file, path.Join(menuset.PreloginDir, f.sub), path.Join(menuset.ThemesDir, menuset.DefaultTheme, f.sub)))
			}
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			warn(fmt.Sprintf("%s can't be read: %v", rel, err), "Fix the file's permissions or replace it.")
			continue
		}
		if f.sub == "cfg" {
			var records []menu.CommandRecord
			if err := json.Unmarshal(data, &records); err != nil {
				warn(fmt.Sprintf("%s (%s) is not a command list: %v", rel, p, err), "Fix the JSON; the login matrix is skipped until then.")
				continue
			}
		}
		if layer == menuset.LayerOverlay {
			fromBoard++
		}
	}

	stringsPath := set.Path(menuset.LayerOverlay, config.ThemeStringsFile)
	// A link whose target is gone reads as no file; it is a mistake, not an
	// absent file.
	if _, lerr := os.Lstat(stringsPath); lerr == nil {
		if _, serr := os.Stat(stringsPath); serr != nil {
			warn(fmt.Sprintf("%s can't be read: %v", stringsPath, serr), "Fix or remove the link.")
		}
	}
	if _, problems, err := config.ReadStringsLayer(stringsPath); err != nil {
		warn(fmt.Sprintf("%s: %v", stringsPath, err), "Fix the JSON; the matrix uses the board's strings until then.")
	} else {
		for _, p := range problems {
			warn(fmt.Sprintf("%s: %s does not fit what the BBS passes it: %s", stringsPath, p.Key, p.Detail), "Correct the value; the board's string prints until then.")
		}
	}

	if !warned {
		msg := fmt.Sprintf("login matrix and connection check from %s (the shipped stock files)", path.Join(menuset.ThemesDir, menuset.DefaultTheme))
		if fromBoard > 0 {
			msg = fmt.Sprintf("login matrix and connection check: %d from %s/, the rest from %s", fromBoard, menuset.PreloginDir, path.Join(menuset.ThemesDir, menuset.DefaultTheme))
		}
		add(name, doctorOK, msg, "")
	}
}
