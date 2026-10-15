package configeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
)

func writeJSONFixture(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSaveAllKeepsExternalEditsToSharedFiles is issue #641: helper ftnsetup
// and helper fileecho (or the BBS) write conferences.json, file_areas.json,
// ftn.json, v3net.json, and config.json while the editor is open; the
// editor's next save must keep those changes, including when the sysop
// edited nothing in those files.
func TestSaveAllKeepsExternalEditsToSharedFiles(t *testing.T) {
	m, dir := newDiskModel(t)
	m.dirty = true

	// Changes made elsewhere after the editor loaded.
	confs := append([]conference.Conference(nil), m.configs.Conferences...)
	confs = append(confs, conference.Conference{ID: 99, Position: 99, Tag: "FIDONET", Name: "Fidonet"})
	writeJSONFixture(t, filepath.Join(dir, "conferences.json"), confs)

	areas := append([]file.FileArea(nil), m.configs.FileAreas...)
	areas = append(areas, file.FileArea{ID: 99, Tag: "FD_NODE", Name: "Nodelists", Path: "fd_node"})
	writeJSONFixture(t, filepath.Join(dir, "file_areas.json"), areas)

	ftnCfg := m.configs.FTN
	ftnCfg.Networks = map[string]config.FTNNetworkConfig{}
	for k, v := range m.configs.FTN.Networks {
		ftnCfg.Networks[k] = v
	}
	ftnCfg.Networks["fidonet"] = config.FTNNetworkConfig{OwnAddress: "1:2/3"}
	writeJSONFixture(t, filepath.Join(dir, "ftn.json"), ftnCfg)

	if _, err := config.UpdateServerConfig(dir, func(c *config.ServerConfig) error {
		c.AllowNewUsers = !m.configs.Server.AllowNewUsers
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantAllowNew := !m.configs.Server.AllowNewUsers

	// The sysop changes the board name in the editor and saves.
	m.configs.Server.BoardName = "Edited Board"
	if !m.saveAll() {
		t.Fatalf("save failed: %s", m.message)
	}

	got := reloadConfigs(t, dir)
	if got.Server.BoardName != "Edited Board" || got.Server.AllowNewUsers != wantAllowNew {
		t.Errorf("server: board=%q allowNew=%v", got.Server.BoardName, got.Server.AllowNewUsers)
	}
	if !hasConf(got.Conferences, "FIDONET") {
		t.Error("conference added elsewhere was dropped")
	}
	if !hasFileArea(got.FileAreas, "FD_NODE") {
		t.Error("file area added elsewhere was dropped")
	}
	if _, ok := got.FTN.Networks["fidonet"]; !ok {
		t.Error("FTN network added elsewhere was dropped")
	}
	// The editor's own copy now includes them too.
	if !hasConf(m.configs.Conferences, "FIDONET") || !hasFileArea(m.configs.FileAreas, "FD_NODE") {
		t.Error("editor copy not refreshed with the merged result")
	}
}

func hasConf(confs []conference.Conference, tag string) bool {
	for _, c := range confs {
		if c.Tag == tag {
			return true
		}
	}
	return false
}

func hasFileArea(areas []file.FileArea, tag string) bool {
	for _, a := range areas {
		if a.Tag == tag {
			return true
		}
	}
	return false
}
