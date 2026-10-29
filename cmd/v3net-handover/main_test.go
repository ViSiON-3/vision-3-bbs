package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// checkHubKey tells a hub on the old key (post as usual) from one already
// on the new key (post, late) and refuses one on any other key or none.
func TestCheckHubKey(t *testing.T) {
	keys := map[string]*keystore.Keystore{}
	for _, name := range []string{"old", "new", "other"} {
		ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		keys[name] = ks
	}
	h, err := protocol.NewCoordHandover(keys["old"], "testnet", keys["new"].PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var served string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3net/v1/testnet/info" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubPubKeyB64: served})
	}))
	defer ts.Close()

	cases := []struct {
		name     string
		hubKey   string
		network  string
		wantLate bool
		wantErr  bool
	}{
		{"old key", keys["old"].PubKeyBase64(), "testnet", false, false},
		{"new key", keys["new"].PubKeyBase64(), "testnet", true, false},
		{"other key", keys["other"].PubKeyBase64(), "testnet", false, true},
		{"no key", "", "testnet", false, true},
		{"unknown network", keys["old"].PubKeyBase64(), "othernet", false, true},
	}
	for _, tc := range cases {
		served = tc.hubKey
		late, err := checkHubKey(ts.Client(), ts.URL, tc.network, h)
		if late != tc.wantLate || (err != nil) != tc.wantErr {
			t.Errorf("%s: late %v, err %v; want late %v, error %v", tc.name, late, err, tc.wantLate, tc.wantErr)
		}
	}
}
