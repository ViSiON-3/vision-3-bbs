package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// runMainEnv makes the test binary run main instead of the tests, so a test
// can see the exit status main gives a script.
const runMainEnv = "V3NET_HANDOVER_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) != "" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runTool runs main with args in a process of its own and returns its exit
// status and everything it printed.
func runTool(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...) //nolint:gosec // the test binary itself, re-run
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("run the tool: %v", err)
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

// A script reads the outcome from main's exit status: against a real hub, 0
// for a post that worked and 1 for one refused, or stopped before posting, and
// 3 when the reply to the post proves nothing or never comes, even if the hub
// took the post. Flags it cannot parse are 2, from the flag package.
func TestMainExitStatus(t *testing.T) {
	dir := t.TempDir()
	load := func(name string) (*keystore.Keystore, string) {
		t.Helper()
		path := filepath.Join(dir, name+".key")
		ks, _, err := keystore.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return ks, path
	}
	a, aPath := load("a")
	b, _ := load("b")
	stranger, strangerPath := load("stranger")
	hubTS := switchedHubServer(t, a, a)
	forward := passTo(t, hubTS)

	// behind serves the hub, with the reply to the post replaced by reply.
	behind := func(reply http.HandlerFunc) *httptest.Server {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				reply(w, r)
				return
			}
			forward.ServeHTTP(w, r)
		}))
		t.Cleanup(ts.Close)
		return ts
	}
	forged := behind(func(w http.ResponseWriter, r *http.Request) {
		forward.ServeHTTP(&forgedStatus{ResponseWriter: w, status: http.StatusConflict}, r)
	})
	dropped := behind(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	})
	// hubOnOtherKey answers as a hub running neither key the tool is given.
	hubOnOtherKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubPubKeyB64: stranger.PubKeyBase64()})
	}))
	t.Cleanup(hubOnOtherKey.Close)

	post := func(hubURL string, extra ...string) []string {
		return append([]string{"-keystore", aPath, "-hub", hubURL, "-network", "testnet", "-new-key", b.PubKeyBase64()}, extra...)
	}
	cases := []struct {
		name string
		args []string
		want int
		said string // in the output
	}{
		{"a post that worked", post(hubTS.URL), 0, "Handover published"},
		{"a refused post", post(hubTS.URL, "-auth-keystore", strangerPath), 1, "Response: 401"},
		{"a hub on neither key", post(hubOnOtherKey.URL), 1, "refusing to post"},
		{"options missing", []string{"-network", "testnet"}, 1, "need -keystore"},
		{"a flag it does not know", []string{"-no-such-flag"}, 2, "no-such-flag"},
		{"a flag with its value missing", []string{"-keystore"}, 2, "keystore"},
		{"a reply that does not verify", post(forged.URL), 3, "may or may not"},
		{"no reply at all", post(dropped.URL), 3, "may or may not"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, out := runTool(t, tc.args...)
			if got != tc.want {
				t.Errorf("exit status %d, want %d; output %q", got, tc.want, out)
			}
			if !strings.Contains(out, tc.said) {
				t.Errorf("output %q does not contain %q", out, tc.said)
			}
		})
	}
}
