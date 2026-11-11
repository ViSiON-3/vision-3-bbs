package leaf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// redirectTestLeaf is a leaf for hubURL and network with its own key.
func redirectTestLeaf(t *testing.T, hubURL, network string) *Leaf {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("keystore: %v", err)
	}
	return New(Config{HubURL: hubURL, Network: network, Keystore: ks})
}

// A hub that redirects the leaf to another host isn't followed: the request
// fails and the other host never sees it. A redirect within the hub's own
// host is still followed.
func TestLeafRefusesRedirectToAnotherHost(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
	}))
	defer other.Close()

	var moved atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v3net/v1/away/nal", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/nal", http.StatusFound)
	})
	mux.HandleFunc("/v3net/v1/moved/nal", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere/nal", http.StatusFound)
	})
	mux.HandleFunc("/elsewhere/nal", func(w http.ResponseWriter, r *http.Request) {
		moved.Add(1)
		http.NotFound(w, r)
	})
	hub := httptest.NewServer(mux)
	defer hub.Close()

	l := redirectTestLeaf(t, hub.URL, "away")
	if _, err := l.FetchNAL(context.Background()); err == nil {
		t.Fatal("FetchNAL followed a redirect to another host without error")
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the other host got %d request(s), want 0", n)
	}

	l = redirectTestLeaf(t, hub.URL, "moved")
	_, _ = l.FetchNAL(context.Background())
	if moved.Load() != 1 {
		t.Errorf("a redirect within the hub's host wasn't followed")
	}
}

// Every client the leaf makes refuses cross-host redirects.
func TestLeafClientsCheckRedirects(t *testing.T) {
	l := redirectTestLeaf(t, "http://hub.invalid", "n")
	for name, c := range map[string]*http.Client{"client": l.client, "sseClient": l.sseClient} {
		if c.CheckRedirect == nil {
			t.Errorf("%s has no CheckRedirect", name)
		}
	}
}

// sameDialTarget compares what Go will dial: scheme, port (with the scheme's
// default) and host, ignoring ASCII case only. Unicode case folding must not
// make two hostnames match: U+212A KELVIN SIGN folds to "k" but is a
// different name once IDNA turns it into ASCII.
func TestSameDialTarget(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"http://hub.example:8765/x", "http://hub.example:8765/y", true},
		{"http://HUB.Example:8765/x", "http://hub.example:8765/y", true},
		{"http://hub.example/x", "http://hub.example:80/y", true},
		{"https://hub.example/x", "https://hub.example:443/y", true},
		{"http://hub.example:8765/x", "http://hub.example:8766/x", false},
		{"http://hub.example/x", "https://hub.example/x", false},
		{"http://hub.example:443/x", "https://hub.example/x", false},
		{"http://hub.example/x", "http://other.example/x", false},
		{"http://kub.example/x", "http://\u212Aub.example/x", false},
		{"http://\u212Aub.example/x", "http://\u212Aub.example/y", true},
		{"http://[::1]:8765/x", "http://[::1]:8765/y", true},
		{"http://127.0.0.1:8765/x", "http://[::1]:8765/x", false},
	} {
		a, errA := url.Parse(tt.a)
		b, errB := url.Parse(tt.b)
		if errA != nil || errB != nil {
			t.Fatalf("parse %q, %q: %v %v", tt.a, tt.b, errA, errB)
		}
		if got := sameDialTarget(a, b); got != tt.want {
			t.Errorf("sameDialTarget(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
