package ftn

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSyncBinkdConfUpdatesNodeHostAndPassword(t *testing.T) {
	// Hub change scenario: the link's hostname/port and password now live in
	// ftn.json; sync must rewrite the matching node line in place.
	path := writeConf(t, "iport 24554\nnode 21:4/999@fsxnet oldhub.example.org:24554 oldpwd\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "newpwd", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	got := readConf(t, path)
	if !strings.Contains(got, "node 21:4/999@fsxnet pointhub.example.org:24556 newpwd") {
		t.Errorf("node line not rewritten:\n%s", got)
	}
	if strings.Contains(got, "oldhub") {
		t.Errorf("old host survived:\n%s", got)
	}
}

func TestSyncBinkdConfAppendsMissingNode(t *testing.T) {
	// A link configured in the TUI with a hostname but no node line yet
	// (e.g. address changed, or link added without the wizard) must get a
	// node line appended.
	path := writeConf(t, "iport 24554\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "s3cret", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if !strings.Contains(readConf(t, path), "node 21:4/999@fsxnet pointhub.example.org:24556 s3cret") {
		t.Errorf("missing node line not appended:\n%s", readConf(t, path))
	}
}

func TestSyncBinkdConfPasswordOnlyLinkDoesNotCreateNode(t *testing.T) {
	// A link with no hostname configured keeps the legacy behavior: update
	// an existing line's password, never invent a node line (no host known).
	path := writeConf(t, "iport 24554\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "s3cret"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if strings.Contains(readConf(t, path), "node ") {
		t.Errorf("node line invented without a hostname:\n%s", readConf(t, path))
	}
}

func TestSyncBinkdConfSurvivesOversizedLines(t *testing.T) {
	// An over-64KB line must not truncate the rewrite: content after it
	// survives and the node line past it is still found (not re-appended).
	content := "# " + strings.Repeat("x", 128*1024) + "\n" +
		"node 21:4/999@fsxnet oldhub.example.org:24554 oldpwd\n" +
		"iport 24555\n"
	path := writeConf(t, content)
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "newpwd", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	got := readConf(t, path)
	if !strings.Contains(got, "iport 24555") {
		t.Errorf("content after oversized line truncated:\n%.200s...", got)
	}
	if n := strings.Count(got, "node 21:4/999@fsxnet"); n != 1 {
		t.Errorf("want exactly 1 node line, got %d", n)
	}
	if !strings.Contains(got, "pointhub.example.org:24556 newpwd") {
		t.Errorf("node line not updated:\n%s", got)
	}
}

func TestRegenerateBinkdConfFromConfig(t *testing.T) {
	// Deleting binkd.conf must not be a dead end: everything needed to
	// rebuild it lives in ftn.json + server config.
	dir := t.TempDir()
	confPath := dir + "/binkd.conf"
	cfg := BinkdConfig{
		BBSRoot:   "/real/root",
		BoardName: "Test Board",
		SysopName: "Test Sysop",
		Location:  "Testville",
		Domains:   map[string]int{"fsxnet": 21},
		Addresses: []string{"21:4/999@fsxnet"},
	}
	nodes := []BinkdNode{{
		Address: "21:4/158@fsxnet", Hostname: "pointhub.example.org:24556",
		SessionPwd: "s3cret", NetworkName: "fsxnet",
	}}
	if err := RegenerateBinkdConf(confPath, cfg, nodes); err != nil {
		t.Fatalf("RegenerateBinkdConf: %v", err)
	}
	got := readConf(t, confPath)
	for _, want := range []string{
		"domain fsxnet " + filepath.Join("/real/root", "data/ftn/out") + " 21",
		"address 21:4/999@fsxnet",
		"sysname \"Test Board\"",
		"node 21:4/158@fsxnet pointhub.example.org:24556 s3cret",
		"log " + filepath.Join("/real/root", "data/logs/binkd.log"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("regenerated conf missing %q:\n%s", want, got)
		}
	}
	if HasPlaceholders(got, "/real/root") {
		t.Error("regenerated conf must not contain template placeholders")
	}
}

func TestSyncBinkdConfAppendsMissingNodesDeterministically(t *testing.T) {
	// Multiple appended node lines must come out in a stable (sorted) order
	// so repeated syncs don't churn the file.
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "a", HostPort: "h1:24554"},
		"1:2/3@othernet":  {SessionPwd: "b", HostPort: "h2:24554"},
	}
	for i := 0; i < 5; i++ {
		path := writeConf(t, "iport 24554\n")
		if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
			t.Fatalf("SyncBinkdConf: %v", err)
		}
		got := readConf(t, path)
		first := strings.Index(got, "node 1:2/3@othernet")
		second := strings.Index(got, "node 21:4/999@fsxnet")
		if first == -1 || second == -1 || first > second {
			t.Fatalf("appended nodes not in stable sorted order:\n%s", got)
		}
	}
}

func TestSyncBinkdConfNoChangeLeavesFileUntouched(t *testing.T) {
	content := "iport 24554\nnode 21:4/999@fsxnet pointhub.example.org:24556 s3cret\n"
	path := writeConf(t, content)
	info1, _ := os.Stat(path)
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "s3cret", HostPort: "pointhub.example.org:24556"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if readConf(t, path) != content {
		t.Errorf("file content changed on no-op sync")
	}
	info2, _ := os.Stat(path)
	if info1.ModTime() != info2.ModTime() {
		t.Errorf("file rewritten on no-op sync")
	}
}

// TestSyncBinkdConfPreservesNodeOptions covers node lines carrying binkd
// options. binkd strips any "-word" from a node line's positional stream
// wherever it appears, so "node <addr> -nomd <host> <pwd>" is ordinary — but
// reading the host and password at fixed offsets 2 and 3 wrote the host over
// the option and the password over the host, leaving the real password parked
// in the flavour slot where binkd rejects the config outright.
func TestSyncBinkdConfPreservesNodeOptions(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "option before host",
			line: "node 21:4/999@fsxnet -nomd old.example.org:24554 oldpw",
			want: "node 21:4/999@fsxnet -nomd new.example.org:24556 newpw",
		},
		{
			name: "option before address",
			line: "node -ip 21:4/999@fsxnet old.example.org:24554 oldpw",
			want: "node -ip 21:4/999@fsxnet new.example.org:24556 newpw",
		},
		{
			name: "options either side of the password",
			line: "node 21:4/999@fsxnet -nr old.example.org:24554 oldpw -nd",
			want: "node 21:4/999@fsxnet -nr new.example.org:24556 newpw -nd",
		},
		{
			// -pipe takes the next word as its argument, so that word is not
			// the host however much it looks like one.
			name: "option with an argument",
			line: "node 21:4/999@fsxnet -pipe ssh-tunnel old.example.org:24554 oldpw",
			want: "node 21:4/999@fsxnet -pipe ssh-tunnel new.example.org:24556 newpw",
		},
		{
			name: "trailing flavour is left alone",
			line: "node 21:4/999@fsxnet old.example.org:24554 oldpw c",
			want: "node 21:4/999@fsxnet new.example.org:24556 newpw c",
		},
	}

	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "newpw", HostPort: "new.example.org:24556"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConf(t, "iport 24554\n"+tt.line+"\n")
			if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
				t.Fatalf("SyncBinkdConf: %v", err)
			}
			got := readConf(t, path)
			if !strings.Contains(got, tt.want+"\n") {
				t.Errorf("node line not synced in place:\ngot:\n%s\nwant line: %s", got, tt.want)
			}
			if strings.Count(got, "node ") != 1 {
				t.Errorf("expected exactly one node line, got:\n%s", got)
			}
		})
	}
}

// TestSyncBinkdConfUpdatesShortNodeLine covers a node directive that names
// only its address — legal binkd config for a listed node with no host or
// password. Matching such a line needs the address alone, so the sync used to
// skip it entirely and the append pass then wrote a second directive for the
// same node.
func TestSyncBinkdConfUpdatesShortNodeLine(t *testing.T) {
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "newpw", HostPort: "new.example.org:24556"},
	}
	for _, line := range []string{
		"node 21:4/999@fsxnet",
		"node 21:4/999@fsxnet old.example.org:24554",
		"node -nomd 21:4/999@fsxnet",
	} {
		t.Run(line, func(t *testing.T) {
			path := writeConf(t, "iport 24554\n"+line+"\n")
			if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
				t.Fatalf("SyncBinkdConf: %v", err)
			}
			got := readConf(t, path)
			if n := strings.Count(got, "node "); n != 1 {
				t.Errorf("expected the line to be updated in place, got %d node lines:\n%s", n, got)
			}
			if !strings.Contains(got, "new.example.org:24556 newpw") {
				t.Errorf("host and password not synced:\n%s", got)
			}
		})
	}
}

// TestSyncBinkdConfKeepsHostWhenLinkHasNone covers a link configured without a
// hostname: only the password is synced, and the host already on the line is
// left as it is.
func TestSyncBinkdConfKeepsHostWhenLinkHasNone(t *testing.T) {
	path := writeConf(t, "iport 24554\nnode 21:4/999@fsxnet hub.example.org:24554 oldpw\n")
	links := map[string]BinkdLinkSync{
		"21:4/999@fsxnet": {SessionPwd: "newpw"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	got := readConf(t, path)
	want := "node 21:4/999@fsxnet hub.example.org:24554 newpw"
	if !strings.Contains(got, want+"\n") {
		t.Errorf("got:\n%s\nwant line: %s", got, want)
	}
}

// Not inventing a node line without a hostname is correct, but doing it
// silently is how a network ends up appearing configured while binkd has no
// way to call it — the mail then arrives only when the uplink calls in, or as
// a side effect of another network's poll over a shared uplink. The skip must
// be warned about.
func TestSyncBinkdConfWarnsOnLinkWithoutHostname(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	path := writeConf(t, "iport 24554\n")
	links := map[string]BinkdLinkSync{
		"1337:3/123@tqwnet": {SessionPwd: "s3cret"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}

	out := logged.String()
	if !strings.Contains(out, "no hostname") {
		t.Errorf("a hostname-less link must warn, got: %q", out)
	}
	if !strings.Contains(out, "1337:3/123@tqwnet") {
		t.Errorf("warning must name the link, got: %q", out)
	}
}

// A link that does get a node line must not warn, or the log cries wolf on
// every save of a healthy config.
func TestSyncBinkdConfNoWarningWhenHostnamePresent(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	path := writeConf(t, "iport 24554\n")
	links := map[string]BinkdLinkSync{
		"1337:3/123@tqwnet": {SessionPwd: "s3cret", HostPort: "get-ghosted.com:24555"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if strings.Contains(logged.String(), "no hostname") {
		t.Errorf("a pollable link must not warn, got: %q", logged.String())
	}
	if !strings.Contains(readConf(t, path), "node 1337:3/123@tqwnet get-ghosted.com:24555 s3cret") {
		t.Errorf("node line not appended:\n%s", readConf(t, path))
	}
}

// An existing node line already covers the link, so a missing hostname in
// ftn.json is not a problem worth warning about — the host on the line stands.
func TestSyncBinkdConfNoWarningWhenNodeLineExists(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	path := writeConf(t, "iport 24554\nnode 1337:3/123@tqwnet get-ghosted.com:24555 s3cret\n")
	links := map[string]BinkdLinkSync{
		"1337:3/123@tqwnet": {SessionPwd: "s3cret"},
	}
	if err := SyncBinkdConf(path, BinkdIdentity{}, links); err != nil {
		t.Fatalf("SyncBinkdConf: %v", err)
	}
	if strings.Contains(logged.String(), "no hostname") {
		t.Errorf("an existing node line must not warn, got: %q", logged.String())
	}
}
