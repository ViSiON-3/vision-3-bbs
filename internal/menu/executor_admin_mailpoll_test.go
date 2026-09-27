package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
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
	if got := mailPollDeadline(dir, nil); got != mailPollAllowance {
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
	if got, want := mailPollDeadline(dir, nil), mailPollAllowance+2*mailPollCallTimeout; got != want {
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

// ESC or Q stops the poll; other keys do not. Either way the watcher returns
// once the poll is done, so it never reads a key meant for the next prompt.
func TestWatchPollStopKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []int
		stop bool
	}{
		{"esc", []int{'x', int(editor.KeyEsc)}, true},
		{"q", []int{'Q'}, true},
		{"other keys", []int{'a', 'b'}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &scriptedInput{}
			for _, k := range tc.keys {
				in.events = append(in.events, key(k))
			}
			stopped := make(chan struct{})
			done := make(chan struct{})
			returned := make(chan struct{})
			go func() {
				watchPollStopKey(in, done, func() { close(stopped) })
				close(returned)
			}()
			select {
			case <-stopped:
				if !tc.stop {
					t.Fatal("stopped on a key that should be ignored")
				}
			case <-time.After(300 * time.Millisecond):
				if tc.stop {
					t.Fatal("did not stop")
				}
			}
			close(done)
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("watcher kept running after the poll ended")
			}
		})
	}
}

// A disconnect ends the watcher's input: it stops the poll.
func TestWatchPollStopKeyOnDisconnect(t *testing.T) {
	in := &scriptedInput{}
	in.events = append(in.events, struct {
		key int
		err error
	}{0, io.EOF})
	stopped := false
	done := make(chan struct{})
	go func() { time.Sleep(50 * time.Millisecond); close(done) }()
	watchPollStopKey(in, done, func() { stopped = true })
	if !stopped {
		t.Error("disconnect did not stop the poll")
	}
}

// The menu entry can pass --timeout to v3mail; the deadline allows for it so
// the menu does not stop a call v3mail would still let run.
func TestMailPollDeadlineUsesMenuTimeout(t *testing.T) {
	dir := t.TempDir()
	ftn := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{
		"fsxnet": {InternalTosserEnabled: true, Links: []config.FTNLinkConfig{{Address: "21:1/100", Hostname: "hub.example"}}},
	}}
	data, _ := json.Marshal(ftn)
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--timeout", "15m"},
		{"--timeout=15m"},
		{"--network", "fsxnet", "-timeout", "15m"},
	} {
		if got, want := mailPollDeadline(dir, args), mailPollAllowance+15*time.Minute; got != want {
			t.Errorf("%v: %s, want %s", args, got, want)
		}
	}
	for _, args := range [][]string{{"--timeout"}, {"--timeout", "soon"}, {"--network", "timeout"}} {
		if got, want := mailPollDeadline(dir, args), mailPollAllowance+mailPollCallTimeout; got != want {
			t.Errorf("%v: %s, want the default %s", args, got, want)
		}
	}
}
