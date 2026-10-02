package ftn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// datedNodelist is testNodelist under a real-world header line.
func datedNodelist(header string) string {
	return header + "\r\n" + strings.TrimPrefix(testNodelist, ";A fsxNet test nodelist\r\n")
}

const header275 = ";A fsxNet Nodelist for Friday, October 2, 2026 -- Day number 275 : 59569"

func TestParseNodelistHeaderDate(t *testing.T) {
	cases := []struct {
		header string
		date   string
		day    int
	}{
		{header275, "2026-10-02", 275},
		{";A FidoNet Nodelist for Thursday, October 1, 2026 -- Day number 274 : 36797", "2026-10-01", 274},
		{";A Some Nodelist for Monday, March 9, 2026", "2026-03-09", 0},
		{";A fsxNet test nodelist", "", 0},
	}
	for _, c := range cases {
		nl, err := ParseNodelist(strings.NewReader(datedNodelist(c.header)))
		if err != nil {
			t.Fatalf("%q: %v", c.header, err)
		}
		got := ""
		if !nl.Date.IsZero() {
			got = nl.Date.Format("2006-01-02")
		}
		if got != c.date || nl.DayNumber != c.day {
			t.Errorf("%q: date %q day %d, want %q day %d", c.header, got, nl.DayNumber, c.date, c.day)
		}
	}
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadNodelistFile(t *testing.T) {
	text := datedNodelist(header275)
	// FSXNET.Z75 is a ZIP holding FSXNET.275.
	for name, data := range map[string][]byte{
		"FSXNET.275": []byte(text),
		"FSXNET.Z75": zipOf(t, map[string]string{"FSXNET.275": text}),
	} {
		nl, err := ReadNodelistFile(writeTemp(t, name, data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(nl.Entries) != 11 || nl.DayNumber != 275 {
			t.Errorf("%s: %d entries, day %d; want 11 and 275", name, len(nl.Entries), nl.DayNumber)
		}
	}
}

// A nodediff's added lines would parse as a nodelist short of every line that
// did not change.
func TestReadNodelistFileRefusesNodediff(t *testing.T) {
	diff := header275 + "\r\nD1\r\nA1\r\n;A fsxNet Nodelist for Friday, October 9, 2026 -- Day number 282 : 1\r\nC3\r\n" +
		",101,Some_BBS,Portland_OR,Some_Sysop,-Unpublished-,300,CM\r\n"
	_, err := ReadNodelistFile(writeTemp(t, "FSXNET.D82", []byte(diff)))
	if err == nil || !strings.Contains(err.Error(), "nodediff") {
		t.Errorf("err = %v, want a nodediff refusal", err)
	}
}

func TestCompileNodelist(t *testing.T) {
	nl, err := ParseNodelist(strings.NewReader(datedNodelist(header275)))
	if err != nil {
		t.Fatal(err)
	}
	c := CompileNodelist(nl, "fsxnet", "FSXNET.Z75")
	if c.Network != "fsxnet" || c.Source != "FSXNET.Z75" || c.DayNumber != 275 || len(c.Nodes) != 11 {
		t.Fatalf("compiled = %+v", c)
	}
	if !c.HasZone(21) || c.HasZone(1337) {
		t.Errorf("HasZone(21) = %v, HasZone(1337) = %v; want true, false", c.HasZone(21), c.HasZone(1337))
	}
	var down CompiledNode
	for _, n := range c.Nodes {
		if n.Address == "21:2/102" {
			down = n
		}
	}
	if down.Status != "Down" || down.Name != "Dead BBS" || down.Sysop != "Dead Op" {
		t.Errorf("21:2/102 = %+v", down)
	}
}

func compiledAt(date string) *CompiledNodelist {
	c := &CompiledNodelist{Network: "fsxnet", Source: date, Nodes: []CompiledNode{{Address: "21:1/100", Name: date}}}
	if date != "" {
		c.Date, _ = time.Parse("2006-01-02", date)
	}
	return c
}

// Weeks can arrive out of order: an older list does not replace a newer one
// unless forced, and an undated list always replaces.
func TestSaveCompiledNodelistKeepsTheNewer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nodelist")
	steps := []struct {
		date        string
		force, save bool
		current     string
	}{
		{"2026-10-02", false, true, "2026-10-02"},
		{"2026-09-25", false, false, "2026-10-02"},
		{"2026-09-25", true, true, "2026-09-25"},
		{"2026-10-09", false, true, "2026-10-09"},
		{"", false, true, ""},
	}
	for i, s := range steps {
		saved, current, err := SaveCompiledNodelist(dir, "FSXNet", compiledAt(s.date), s.force)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if saved != s.save || current.Source != s.current {
			t.Errorf("step %d (%s, force %v): saved %v, current %q; want %v, %q", i, s.date, s.force, saved, current.Source, s.save, s.current)
		}
		onDisk, err := LoadCompiledNodelist(dir, "fsxnet")
		if err != nil || onDisk.Source != s.current {
			t.Errorf("step %d: on disk %v, %v; want %q", i, onDisk, err, s.current)
		}
	}
}

func TestCompiledNodelistPathRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", "..", "a/b", `a\b`, "c:x"} {
		if _, err := CompiledNodelistPath("dir", name); err == nil {
			t.Errorf("CompiledNodelistPath(%q) succeeded", name)
		}
	}
	if p, err := CompiledNodelistPath("dir", "FSXNet"); err != nil || p != filepath.Join("dir", "fsxnet.json") {
		t.Errorf("CompiledNodelistPath(FSXNet) = %q, %v", p, err)
	}
	if _, err := LoadCompiledNodelist(t.TempDir(), "fsxnet"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("LoadCompiledNodelist with none compiled = %v, want os.ErrNotExist", err)
	}
}

func TestNodelistIndexLookup(t *testing.T) {
	dir := t.TempDir()
	nl, err := ParseNodelist(strings.NewReader(datedNodelist(header275)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SaveCompiledNodelist(dir, "fsxnet", CompileNodelist(nl, "fsxnet", "FSXNET.Z75"), false); err != nil {
		t.Fatal(err)
	}
	x := NewNodelistIndex(dir)

	n, ok := x.Lookup("FSXNET", Address{Zone: 21, Net: 4, Node: 158})
	if !ok || n.Name != "My BBS" || n.Location != "Berlin DE" {
		t.Errorf("21:4/158 = %+v, %v", n, ok)
	}
	// A point finds its boss node.
	if n, ok := x.Lookup("fsxnet", Address{Zone: 21, Net: 4, Node: 158, Point: 1}); !ok || n.Address != "21:4/158" {
		t.Errorf("21:4/158.1 = %+v, %v; want the boss node", n, ok)
	}
	if _, ok := x.Lookup("fsxnet", Address{Zone: 21, Net: 4, Node: 999}); ok {
		t.Error("an unlisted node was found")
	}
	if _, ok := x.Lookup("tqwnet", Address{Zone: 21, Net: 4, Node: 158}); ok {
		t.Error("a network with no compiled nodelist found a node")
	}

	// A new week's list is picked up without a new index.
	next := compiledAt("2026-10-09")
	next.Nodes = []CompiledNode{{Address: "21:4/158", Name: "Renamed BBS"}, {Address: "21:4/160", Name: "New BBS"}}
	if _, _, err := SaveCompiledNodelist(dir, "fsxnet", next, false); err != nil {
		t.Fatal(err)
	}
	path, _ := CompiledNodelistPath(dir, "fsxnet")
	future := time.Now().Add(time.Hour) // a modification time the index has not seen
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if n, ok := x.Lookup("fsxnet", Address{Zone: 21, Net: 4, Node: 158}); !ok || n.Name != "Renamed BBS" {
		t.Errorf("after the new list, 21:4/158 = %+v, %v", n, ok)
	}
	if _, ok := x.Lookup("fsxnet", Address{Zone: 21, Net: 4, Node: 160}); !ok {
		t.Error("a node added in the new list was not found")
	}
}

// A downloaded nodediff is refused like one read from a file, plain or zipped:
// an added Zone line would otherwise pass the zone check and replace the full
// list with only the lines that changed.
func TestDownloadNodelistRefusesNodediff(t *testing.T) {
	diff := header275 + "\r\nD1\r\nA2\r\n;A fsxNet Nodelist for Friday, October 9, 2026 -- Day number 282 : 1\r\n" +
		"Zone,21,fsxNet,New_Zealand,Zone_Coordinator,-Unpublished-,300,CM\r\nC3\r\n"
	for name, body := range map[string][]byte{
		"plain":  []byte(diff),
		"zipped": zipOf(t, map[string]string{"FSXNET.D82": diff}),
	} {
		srv := serveBytes(t, body)
		_, err := DownloadNodelist(t.Context(), srv.URL)
		if err == nil || !strings.Contains(err.Error(), "nodediff") {
			t.Errorf("%s: err = %v, want a nodediff refusal", name, err)
		}
	}
}

// Text from a nodelist is shown to callers, so control characters are removed.
func TestCompileNodelistStripsControlCharacters(t *testing.T) {
	nl := &Nodelist{Entries: []NodelistEntry{{
		Address: Address{Zone: 21, Net: 1, Node: 100},
		Name:    "Evil\x1b[2JBBS", Location: "Here\x07", Sysop: "Op\u009b31m", Flags: []string{"CM", "\x1b"},
	}}}
	n := CompileNodelist(nl, "fsxnet", "x").Nodes[0]
	if n.Name != "Evil[2JBBS" || n.Location != "Here" || n.Sysop != "Op31m" || strings.Join(n.Flags, ",") != "CM" {
		t.Errorf("compiled = %+v", n)
	}
}
