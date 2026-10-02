package tosser

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// zipped returns a ZIP archive holding one file, as a nodelist's .Zxx is.
func zipped(t *testing.T, name, content string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func nodelistText(zone int, day string) string {
	return ";A Test Nodelist for Friday, October " + day + ", 2026\r\n" +
		"Zone," + strconv.Itoa(zone) + ",Test_ZC,Dunedin,Zone_Op,-Unpublished-,300,CM\r\n" +
		"Host,4,Net4_HQ,Berlin,Host_Four,-Unpublished-,300,CM\r\n" +
		",158,Hub_BBS,Berkeley_USA,Hub_Sysop,-Unpublished-,300,CM,INA:hub.example,IBN\r\n"
}

// nodelistEnv is a TIC environment whose network takes its nodelist from the
// echo feeding the LINUX area, with the given file pattern.
func nodelistEnv(t *testing.T, pattern string) (*ticEnv, string) {
	t.Helper()
	e := setupTICEnv(t, hub())
	e.tosser.config.Nodelist = config.FTNNodelistConfig{FileEcho: "tqw_linuxfiles", FilePattern: pattern}
	dir := filepath.Join(e.dataDir, "ftn", "nodelist")
	e.tosser.SetNodelistDir(dir)
	return e, dir
}

func TestTICNodelistIsCompiled(t *testing.T) {
	e, dir := nodelistEnv(t, "TEST.Z*")
	writeTIC(t, e.inboundDir, "a.tic", "test.z75", zipped(t, "TEST.275", nodelistText(21, "2")))

	result := e.tosser.ProcessInbound()

	if result.FilesImported != 1 || result.NodelistsCompiled != 1 || len(result.Errors) != 0 {
		t.Fatalf("FilesImported %d, NodelistsCompiled %d, errors %v", result.FilesImported, result.NodelistsCompiled, result.Errors)
	}
	n, ok := ftn.NewNodelistIndex(dir).Lookup("testnet", ftn.Address{Zone: 21, Net: 4, Node: 158, Point: 1})
	if !ok || n.Name != "Hub BBS" {
		t.Errorf("lookup = %+v, %v", n, ok)
	}
	if !exists(e.areaFile("linux", "test.z75")) {
		t.Error("the nodelist was not delivered into its area")
	}
}

// Files in the echo that do not match the pattern are only delivered.
func TestTICNodelistPatternSelectsFiles(t *testing.T) {
	e, dir := nodelistEnv(t, "TEST.Z*")
	writeTIC(t, e.inboundDir, "a.tic", "infopack.zip", zipped(t, "TEST.275", nodelistText(21, "2")))

	result := e.tosser.ProcessInbound()

	if result.FilesImported != 1 || result.NodelistsCompiled != 0 || len(result.Errors) != 0 {
		t.Fatalf("FilesImported %d, NodelistsCompiled %d, errors %v", result.FilesImported, result.NodelistsCompiled, result.Errors)
	}
	if exists(filepath.Join(dir, "testnet.json")) {
		t.Error("a file not matching the pattern was compiled")
	}
}

// Another network's nodelist in the echo is refused: reported when it matches
// the pattern, skipped quietly when there is no pattern.
func TestTICNodelistForAnotherZoneIsNotCompiled(t *testing.T) {
	for _, pattern := range []string{"TEST.Z*", ""} {
		e, dir := nodelistEnv(t, pattern)
		writeTIC(t, e.inboundDir, "a.tic", "test.z75", zipped(t, "TEST.275", nodelistText(1337, "2")))

		result := e.tosser.ProcessInbound()

		wantErrors := 0
		if pattern != "" {
			wantErrors = 1
		}
		if result.FilesImported != 1 || result.NodelistsCompiled != 0 || len(result.Errors) != wantErrors {
			t.Errorf("pattern %q: FilesImported %d, NodelistsCompiled %d, errors %v; want 1, 0, %d errors",
				pattern, result.FilesImported, result.NodelistsCompiled, result.Errors, wantErrors)
		}
		if exists(filepath.Join(dir, "testnet.json")) {
			t.Errorf("pattern %q: another zone's nodelist was compiled", pattern)
		}
	}
}

// An older week arriving late does not replace the newer list.
func TestTICNodelistOlderWeekKeepsTheNewer(t *testing.T) {
	e, dir := nodelistEnv(t, "TEST.Z*")
	writeTIC(t, e.inboundDir, "a.tic", "test.z82", zipped(t, "TEST.282", nodelistText(21, "9")))
	e.tosser.ProcessInbound()
	writeTIC(t, e.inboundDir, "b.tic", "test.z75", zipped(t, "TEST.275", nodelistText(21, "2")))

	result := e.tosser.ProcessInbound()

	if result.NodelistsCompiled != 0 || len(result.Errors) != 0 {
		t.Fatalf("NodelistsCompiled %d, errors %v; want the older week skipped", result.NodelistsCompiled, result.Errors)
	}
	c, err := ftn.LoadCompiledNodelist(dir, "testnet")
	if err != nil || c.Source != "test.z82" {
		t.Errorf("compiled = %v, %v; want test.z82 kept", c, err)
	}
}

// Without a nodelist directory nothing is compiled.
func TestTICNodelistNeedsADirectory(t *testing.T) {
	e, dir := nodelistEnv(t, "TEST.Z*")
	e.tosser.SetNodelistDir("")
	writeTIC(t, e.inboundDir, "a.tic", "test.z75", zipped(t, "TEST.275", nodelistText(21, "2")))

	if r := e.tosser.ProcessInbound(); r.NodelistsCompiled != 0 || r.FilesImported != 1 {
		t.Errorf("NodelistsCompiled %d, FilesImported %d", r.NodelistsCompiled, r.FilesImported)
	}
	if exists(filepath.Join(dir, "testnet.json")) {
		t.Error("compiled with no nodelist directory set")
	}
}
