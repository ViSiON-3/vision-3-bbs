package ftn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateBinkdConfUpdatesExistingNode covers re-running the FTN wizard to
// change a hub's details. UpdateBinkdConf used to return early when the node
// address was already present, so an edited hostname or session password never
// reached binkd.conf and binkd kept dialling the old host.
func TestUpdateBinkdConfUpdatesExistingNode(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "binkd.conf")

	existing := strings.Join([]string{
		"domain fsxnet /home/bbs/data/ftn/out 21",
		"address 21:4/158@fsxnet",
		"",
		"# --- fsxnet (added by FTN Setup Wizard) ---",
		"node 21:1/100@fsxnet agency.bbs.nz:24554 oldpassword",
		"",
		"# --- othernet (added by FTN Setup Wizard) ---",
		"node 99:1/1@othernet other.example:24554 otherpw",
		"",
	}, "\n")
	if err := os.WriteFile(conf, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := BinkdConfig{
		BBSRoot:   dir,
		BoardName: "Test BBS",
		Domains:   map[string]int{"fsxnet": 21},
		Addresses: []string{"21:4/158@fsxnet"},
		Node: BinkdNode{
			Address:     "21:1/100@fsxnet",
			Hostname:    "new.agency.example:24554",
			SessionPwd:  "newpassword",
			NetworkName: "fsxnet",
		},
	}
	if err := UpdateBinkdConf(conf, cfg); err != nil {
		t.Fatalf("UpdateBinkdConf: %v", err)
	}

	out, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.Contains(got, "node 21:1/100@fsxnet new.agency.example:24554 newpassword") {
		t.Errorf("edited node line missing from:\n%s", got)
	}
	if strings.Contains(got, "oldpassword") || strings.Contains(got, "agency.bbs.nz:24554 ") {
		t.Errorf("stale hub details left behind in:\n%s", got)
	}
	if strings.Count(got, "node 21:1/100@fsxnet") != 1 {
		t.Errorf("expected exactly one line for the hub, got:\n%s", got)
	}
	// A second network's link must survive an edit to the first.
	if !strings.Contains(got, "node 99:1/1@othernet other.example:24554 otherpw") {
		t.Errorf("unrelated network's node line was lost from:\n%s", got)
	}
}

// TestUpdateBinkdConfNoOpWhenUnchanged keeps a re-save from rewriting a file
// that already says the right thing.
func TestUpdateBinkdConfNoOpWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "binkd.conf")

	existing := "address 21:4/158@fsxnet\nnode 21:1/100@fsxnet agency.bbs.nz:24554 pw\n"
	if err := os.WriteFile(conf, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := BinkdConfig{
		BBSRoot: dir,
		Domains: map[string]int{"fsxnet": 21},
		Node: BinkdNode{
			Address:     "21:1/100@fsxnet",
			Hostname:    "agency.bbs.nz:24554",
			SessionPwd:  "pw",
			NetworkName: "fsxnet",
		},
	}
	if err := UpdateBinkdConf(conf, cfg); err != nil {
		t.Fatalf("UpdateBinkdConf: %v", err)
	}

	out, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != existing {
		t.Errorf("unchanged config was rewritten:\ngot:\n%s\nwant:\n%s", out, existing)
	}
}

// TestReplaceNodeLinePreservesIndentation keeps an indented directive indented.
func TestReplaceNodeLinePreservesIndentation(t *testing.T) {
	content := "  node 1:2/3 old.host pw\n"
	got, changed := replaceNodeLine(content, "1:2/3", "node 1:2/3 new.host pw2")
	if !changed {
		t.Fatal("expected a change")
	}
	if got != "  node 1:2/3 new.host pw2\n" {
		t.Errorf("got %q", got)
	}
}
