package scripting

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
)

// wantLimit fails the test unless msg is a limit error from call that names
// limit.
func wantLimit(t *testing.T, msg, call, limit string) {
	t.Helper()
	if !strings.Contains(msg, call+":") || !strings.Contains(msg, "over the "+limit+" limit") {
		t.Errorf("error = %q, want a %s error naming the %s limit", msg, call, limit)
	}
}

// v3.fs refuses to read, write or append past maxFSFileBytes, and to list a
// directory past maxFSDirEntries, leaving files as they were.
func TestFSLimits(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	const max = `(4 * 1024 * 1024)`

	h.eval(`v3.fs.write("full.txt", "x".repeat(` + max + `))`)
	msg := h.evalErr(`v3.fs.write("big.txt", "x".repeat(` + max + ` + 1))`)
	wantLimit(t, msg, "v3.fs.write", "4 MB")
	if !strings.Contains(msg, "4194305 bytes") {
		t.Errorf("error = %q, want the exact size when rounding would hide it", msg)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "big.txt")); !os.IsNotExist(err) {
		t.Errorf("refused write created big.txt (stat err %v)", err)
	}

	if n := h.eval(`v3.fs.read("full.txt").length`).ToInteger(); n != 4<<20 {
		t.Errorf("read a file at the limit: length %d, want %d", n, 4<<20)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, "over.txt"), make([]byte, 4<<20+1), 0o644); err != nil {
		t.Fatal(err)
	}
	wantLimit(t, h.evalErr(`v3.fs.read("over.txt")`), "v3.fs.read", "4 MB")

	wantLimit(t, h.evalErr(`v3.fs.append("full.txt", "y")`), "v3.fs.append", "4 MB")
	if info, err := os.Stat(filepath.Join(h.dataDir, "full.txt")); err != nil || info.Size() != 4<<20 {
		t.Errorf("refused append changed full.txt: %v, %v", info, err)
	}
	wantLimit(t, h.evalErr(`v3.fs.append("new.txt", "x".repeat(`+max+` + 1))`), "v3.fs.append", "4 MB")
	h.eval(`v3.fs.append("log.txt", "one\n"); v3.fs.append("log.txt", "two\n")`)
	if got := h.eval(`v3.fs.read("log.txt")`).String(); got != "one\ntwo\n" {
		t.Errorf("appends under the limit: %q", got)
	}

	dir := filepath.Join(h.dataDir, "many")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range maxFSDirEntries {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := h.eval(`v3.fs.list("many").length`).ToInteger(); n != maxFSDirEntries {
		t.Errorf("list at the limit returned %d entries, want %d", n, maxFSDirEntries)
	}
	if err := os.WriteFile(filepath.Join(dir, "one-more"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if msg := h.evalErr(`v3.fs.list("many")`); !strings.Contains(msg, "v3.fs.list:") || !strings.Contains(msg, "more than 10000 entries") {
		t.Errorf("list over the limit: error %q", msg)
	}
	if n := h.eval(`v3.fs.list("").length`).ToInteger(); n == 0 {
		t.Error("list of the sandbox root returned nothing")
	}
}

// v3.data refuses a change that would leave the store over maxDataStoreBytes
// and keeps the previous store; a store already over the cap can shrink.
func TestDataStoreLimit(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	store := filepath.Join(h.dataDir, "test.json")

	h.eval(`v3.data.set("small", 1)`)
	before, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	wantLimit(t, h.evalErr(`v3.data.set("big", "x".repeat(4 * 1024 * 1024))`), "v3.data.set", "4 MB")
	if after, _ := os.ReadFile(store); string(after) != string(before) {
		t.Errorf("refused set changed the store:\n%s", after)
	}
	if v := h.eval(`v3.data.get("small")`).ToInteger(); v != 1 {
		t.Errorf("small = %d after a refused set, want 1", v)
	}

	// A store over the cap from before it existed.
	legacy := fmt.Sprintf(`{"a": %q, "b": %q}`, strings.Repeat("a", 3<<20), strings.Repeat("b", 2<<20))
	if err := os.WriteFile(store, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := h.eval(`v3.data.get("a").length`).ToInteger(); n != 3<<20 {
		t.Errorf("read of an oversized store: length %d", n)
	}
	wantLimit(t, h.evalErr(`v3.data.set("c", 1)`), "v3.data.set", "4 MB")
	h.eval(`v3.data.delete("b")`)
	h.eval(`v3.data.set("c", 1)`) // under the cap again
	if v := h.eval(`v3.data.get("c")`).ToInteger(); v != 1 {
		t.Errorf("c = %d, want 1", v)
	}
}

// v3.message.post and postPrivate refuse a To, subject or body over its
// limit, and post nothing.
func TestPostLimits(t *testing.T) {
	h := newHarness(t, harnessOpts{providers: newMessageProviders(t)})
	post := func(fn, to, subject string, bodyLen int) string {
		return fmt.Sprintf(`v3.message.%s(1, {to: %q, subject: %q, body: "b".repeat(%d)})`, fn, to, subject, bodyLen)
	}
	name36 := strings.Repeat("n", 36)

	h.eval(post("post", name36, strings.Repeat("s", 72), 64<<10))
	for _, tc := range []struct {
		name, expr, call, limit string
	}{
		{"to", post("post", name36+"n", "Hi", 1), "v3.message.post", "36"},
		{"subject", post("post", "All", strings.Repeat("s", 73), 1), "v3.message.post", "72"},
		{"body", post("post", "All", "Hi", 64<<10+1), "v3.message.post", "64 KB"},
		{"address", post("post", name36+"@"+strings.Repeat("1", 24), "Hi", 1), "v3.message.post", "60"},
		{"private subject", post("postPrivate", "Sysop", strings.Repeat("s", 73), 1), "v3.message.postPrivate", "72"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantLimit(t, h.evalErr(tc.expr), tc.call, tc.limit)
		})
	}
	// The name before a netmail address is what counts.
	h.eval(post("post", name36+"@21:1/100", "Hi", 1))
	if n := h.eval(`v3.message.count(1)`).ToInteger(); n != 2 {
		t.Errorf("area holds %d messages, want the 2 under the limits", n)
	}
}

// v3.nodes.send and broadcast refuse a page over maxPageChars.
func TestPageLimit(t *testing.T) {
	reg := session.NewSessionRegistry()
	target := &session.BbsSession{NodeID: 1}
	reg.Register(target)
	h := newHarness(t, harnessOpts{providers: &Providers{SessionRegistry: reg}})

	if !h.eval(`v3.nodes.send(1, "p".repeat(255))`).ToBoolean() {
		t.Error("send of a page at the limit returned false")
	}
	wantLimit(t, h.evalErr(`v3.nodes.send(1, "p".repeat(256))`), "v3.nodes.send", "255")
	wantLimit(t, h.evalErr(`v3.nodes.broadcast("p".repeat(256))`), "v3.nodes.broadcast", "255")
}

// v3.util.padLeft, padRight and center refuse a width over maxPadWidth.
func TestPadWidthLimit(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	for _, fn := range []string{"padLeft", "padRight", "center"} {
		if n := h.eval(`v3.util.` + fn + `("x", 4096).length`).ToInteger(); n != 4096 {
			t.Errorf("%s at the limit: length %d, want 4096", fn, n)
		}
		wantLimit(t, h.evalErr(`v3.util.`+fn+`("x", 4097)`), "v3.util."+fn, "4096")
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int]string{
		0: "0 bytes", 512: "512 bytes", 64 << 10: "64 KB", 4 << 20: "4 MB",
		5<<20 + 200<<10: "5.2 MB", 1536: "1.5 KB",
	} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
