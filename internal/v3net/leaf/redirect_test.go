package leaf

import (
	"context"
	"net/http"
	"net/http/httptest"
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
