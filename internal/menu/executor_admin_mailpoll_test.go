package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestCRLFWriter(t *testing.T) {
	var out bytes.Buffer
	w := &crlfWriter{w: &out}
	// The split lands between a CR and its LF, which must not gain a second CR.
	for _, chunk := range []string{"one\ntwo\r", "\nthree\n"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := out.String(), "one\r\ntwo\r\nthree\r\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// execMailPoll runs `v3mail poll` from the BBS root with the config and data
// directories, extra menu arguments and the quiet-log variable, and reports a
// failed poll as its exit code rather than as an error.
func TestExecMailPoll(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a stand-in for v3mail")
	}
	root := t.TempDir()
	fake := filepath.Join(root, "v3mail")
	script := "#!/bin/sh\necho \"args: $*\"\necho \"quiet: $V3MAIL_NO_CONSOLE_LOG\"\necho \"cwd: $(pwd -P)\"\necho oops >&2\nexit 3\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(root, "configs")

	var out bytes.Buffer
	code, err := execMailPoll(context.Background(), fake, root, configDir, []string{"--network", "fsxnet"}, &out)
	if err != nil {
		t.Fatalf("execMailPoll: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code %d, want 3", code)
	}
	got := out.String()
	realRoot, _ := filepath.EvalSymlinks(root)
	for _, want := range []string{
		"args: poll --config " + configDir + " --data " + filepath.Join(root, "data") + " --network fsxnet",
		"quiet: 1",
		"cwd: " + realRoot,
		"oops",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// A partial line is held until its newline, so a multi-byte character split
// across two writes reaches the terminal whole; Flush sends what is left.
func TestCRLFWriterHoldsPartialLines(t *testing.T) {
	var out bytes.Buffer
	w := &crlfWriter{w: &out}
	dash := []byte("—") // three bytes in UTF-8
	_, _ = w.Write(append([]byte("a "), dash[:1]...))
	if out.Len() != 0 {
		t.Fatalf("partial line written early: %q", out.String())
	}
	_, _ = w.Write(append(dash[1:], []byte(" b\ntail")...))
	if got := out.String(); got != "a — b\r\n" {
		t.Errorf("got %q", got)
	}
	w.Flush()
	if got := out.String(); got != "a — b\r\ntail" {
		t.Errorf("after Flush got %q", got)
	}
}

// When the session ends, v3mail is asked to stop with SIGTERM, not killed, so
// its own cleanup (stopping binkd, finishing a toss) runs.
func TestExecMailPollStopsWithSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a stand-in for v3mail")
	}
	root := t.TempDir()
	fake := filepath.Join(root, "v3mail")
	script := "#!/bin/sh\ntrap 'echo got TERM; exit 5' TERM\necho started\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuffer
	go func() {
		for !strings.Contains(out.String(), "started") {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	_, err := execMailPoll(ctx, fake, root, filepath.Join(root, "configs"), nil, &out)
	if err == nil {
		t.Error("a stopped poll reported no error")
	}
	if !strings.Contains(out.String(), "got TERM") {
		t.Errorf("v3mail was not sent SIGTERM; output:\n%s", out.String())
	}
}

func TestMailPollDeadlineScalesWithHubs(t *testing.T) {
	dir := t.TempDir()
	if got := mailPollDeadline(dir); got != mailPollAllowance {
		t.Errorf("no networks: %s, want %s", got, mailPollAllowance)
	}
	ftn := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{
		"fsxnet":  {InternalTosserEnabled: true, Links: []config.FTNLinkConfig{{Address: "21:1/100", Hostname: "hub.example"}, {Address: "21:1/101"}}},
		"zeronet": {InternalTosserEnabled: true, Links: []config.FTNLinkConfig{{Address: "99:1/1", Hostname: "z.example"}}},
		"offnet":  {Links: []config.FTNLinkConfig{{Address: "1:1/1", Hostname: "off.example"}}},
	}}
	data, _ := json.Marshal(ftn)
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := mailPollDeadline(dir), mailPollAllowance+2*mailPollCallTimeout; got != want {
		t.Errorf("two callable hubs: %s, want %s", got, want)
	}
}

// syncBuffer is a bytes.Buffer safe to read while exec writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
