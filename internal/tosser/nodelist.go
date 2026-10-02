package tosser

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// Nodelists by file echo. A network whose nodelist setting names the file
// echo its weekly nodelist arrives in has each new one compiled, right after
// delivery, into <nodelist dir>/<network>.json for the BBS to look systems up
// in (see ftn.CompiledNodelist).

// SetNodelistDir sets the directory compiled nodelists are written to.
// Without it (the default), nodelists delivered by file echo are delivered
// like any other file and not compiled.
func (t *Tosser) SetNodelistDir(dir string) {
	t.nodelistDir = dir
}

// compileNodelist compiles a file just delivered into area when it is this
// network's nodelist: it came in the network's nodelist echo and matches the
// nodelist file pattern.
//
// A file that matches a configured pattern and cannot be used is reported as
// a toss error; the sysop said it would be a nodelist. Without a pattern,
// every file in the echo is tried, and those that are not a nodelist for this
// network (infopacks, nodediffs, other networks' lists) are skipped quietly.
func (t *Tosser) compileNodelist(area file.FileArea, delivered string, tic *ftn.TIC, result *TossResult) {
	nlc := t.config.Nodelist
	if t.nodelistDir == "" || nlc.FileEcho == "" || !strings.EqualFold(nlc.FileEcho, tic.Area) {
		return
	}
	if nlc.FilePattern != "" && !ftn.MatchFileName(nlc.FilePattern, delivered) {
		return
	}
	fail := func(err error) {
		if nlc.FilePattern != "" {
			result.Errors = append(result.Errors, fmt.Sprintf("compile nodelist %s from file area %s: %v", delivered, area.Tag, err))
			return
		}
		slog.Info("file in the nodelist echo is not a usable nodelist", "network", t.networkName, "file", delivered, "reason", err)
	}

	areaDir, err := t.fileAreas.GetAreaUploadPath(area.ID)
	if err != nil {
		fail(err)
		return
	}
	// The file sits under its record's name, which a replacement keeps.
	name := delivered
	for _, r := range t.fileAreas.GetFilesForArea(area.ID) {
		if strings.EqualFold(r.Filename, delivered) {
			name = r.Filename
			break
		}
	}

	nl, err := ftn.ReadNodelistFile(filepath.Join(areaDir, name))
	if err != nil {
		fail(err)
		return
	}
	compiled := ftn.CompileNodelist(nl, t.networkName, name)
	if !compiled.HasZone(t.ownAddr.Zone) {
		fail(fmt.Errorf("it has no entry for zone %d, so it is not this network's nodelist", t.ownAddr.Zone))
		return
	}
	saved, current, err := ftn.SaveCompiledNodelist(t.nodelistDir, t.networkName, compiled, false)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("save compiled nodelist for %s: %v", t.networkName, err))
		return
	}
	if !saved {
		slog.Info("kept the newer compiled nodelist", "network", t.networkName, "file", name,
			"date", compiled.Date.Format("2006-01-02"), "current", current.Source, "current_date", current.Date.Format("2006-01-02"))
		return
	}
	result.NodelistsCompiled++
	slog.Info("compiled nodelist", "network", t.networkName, "file", name, "nodes", len(compiled.Nodes),
		"date", compiled.Date.Format("2006-01-02"), "day", compiled.DayNumber)
}
