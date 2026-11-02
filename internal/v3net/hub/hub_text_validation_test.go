package hub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// #843 tests: a hub validates the BBS name a node subscribes with and the area
// text a coordinator posts in a NAL, without locking out data it already holds.

func postSubscribeAs(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, bbsName string) int {
	t.Helper()
	body, _ := json.Marshal(protocol.SubscribeRequest{
		Network: "testnet", NodeID: ks.NodeID(), PubKeyB64: ks.PubKeyBase64(),
		BBSName: bbsName, BBSHost: "bbs.example.net",
	})
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, "POST", ts.URL+"/v3net/v1/subscribe", string(body)))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func TestSubscribeValidatesBBSName(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	for name, want := range map[string]int{
		"Cool BBS":              http.StatusOK,
		"":                      http.StatusOK, // the hub falls back to the node ID
		"|09Cool BBS":           http.StatusUnprocessableEntity,
		"Bad\x1b[2JBBS":         http.StatusUnprocessableEntity,
		"C1 \u009b BBS":         http.StatusUnprocessableEntity,
		strings.Repeat("x", 65): http.StatusUnprocessableEntity,
		strings.Repeat("é", 64): http.StatusOK,
	} {
		ks := newValidationKeystore(t)
		if got := postSubscribeAs(t, ts, ks, name); got != want {
			t.Errorf("subscribe with bbs_name %q: status %d, want %d", name, got, want)
		}
	}
}

// A node the hub already holds under a name from before the check can still
// re-subscribe under that same name, but cannot change to another bad one.
func TestSubscribeKeepsAnExistingBBSName(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	ks := newValidationKeystore(t)
	if got := postSubscribeAs(t, ts, ks, "Old BBS"); got != http.StatusOK {
		t.Fatalf("first subscribe: %d", got)
	}
	// Store a name from before the check directly.
	if _, err := h.subscribers.SetProfileUnlessBanned(ks.NodeID(), "testnet", "|12Old BBS", "bbs.example.net"); err != nil {
		t.Fatal(err)
	}
	if got := postSubscribeAs(t, ts, ks, "|12Old BBS"); got != http.StatusOK {
		t.Errorf("re-subscribe under the stored name: %d, want 200", got)
	}
	if got := postSubscribeAs(t, ts, ks, "|14New BBS"); got != http.StatusUnprocessableEntity {
		t.Errorf("change to a new bad name: %d, want 422", got)
	}
	if got := postSubscribeAs(t, ts, ks, "New BBS"); got != http.StatusOK {
		t.Errorf("change to a good name: %d, want 200", got)
	}
}

func postNALAs(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, areas []protocol.Area) (int, string) {
	t.Helper()
	n := &protocol.NAL{
		V3NetNAL: "1.0", Network: "testnet",
		CoordNodeID: ks.NodeID(), CoordPubKeyB64: ks.PubKeyBase64(),
		Areas: areas,
	}
	if err := nal.Sign(n, ks); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(n)
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, "POST", ts.URL+"/v3net/v1/testnet/nal", string(data)))
	if err != nil {
		t.Fatalf("POST /nal: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func validationArea(tag, name, desc string) protocol.Area {
	return protocol.Area{
		Tag: tag, Name: name, Description: desc, Language: "en",
		Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
		Policy: protocol.AreaPolicy{MaxBodyBytes: 65536},
	}
}

func TestPostNALValidatesAreaText(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	if got := postSubscribeAs(t, ts, hubKS, "Hub BBS"); got != http.StatusOK {
		t.Fatalf("subscribe hub: %d", got)
	}
	seedTestNAL(t, h, hubKS)
	general := validationArea("gen.general", "General", "General discussion")

	for name, area := range map[string]protocol.Area{
		"pipe in name":        validationArea("gen.new", "|CLNew", "x"),
		"control in desc":     validationArea("gen.new", "New", "a\x1b[2Jb"),
		"C1 in language":      func() protocol.Area { a := validationArea("gen.new", "New", "x"); a.Language = "e\u009bn"; return a }(),
		"bad tag":             validationArea("Gen.New", "New", "x"),
		"pipe in changed old": validationArea("gen.general", "General", "now |CL bad"),
	} {
		code, body := postNALAs(t, ts, hubKS, []protocol.Area{general, area})
		if name == "pipe in changed old" {
			code, body = postNALAs(t, ts, hubKS, []protocol.Area{area})
		}
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d (%s), want 422", name, code, body)
		}
	}
	if code, body := postNALAs(t, ts, hubKS, []protocol.Area{general, validationArea("gen.new", "New", "A new area")}); code != http.StatusOK {
		t.Errorf("good NAL: status %d (%s), want 200", code, body)
	}
}

// An area the hub already holds with text from before the check can stay in
// the NAL as it is, so a coordinator is not locked out of every other change.
func TestPostNALKeepsUnchangedOldAreaText(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	if got := postSubscribeAs(t, ts, hubKS, "Hub BBS"); got != http.StatusOK {
		t.Fatalf("subscribe hub: %d", got)
	}
	old := validationArea("gen.old", "|09Old", "from before")
	stored := &protocol.NAL{
		V3NetNAL: "1.0", Network: "testnet",
		CoordNodeID: hubKS.NodeID(), CoordPubKeyB64: hubKS.PubKeyBase64(),
		Areas: []protocol.Area{old},
	}
	if err := nal.Sign(stored, hubKS); err != nil {
		t.Fatal(err)
	}
	if err := h.nalStore.Put("testnet", stored); err != nil {
		t.Fatal(err)
	}
	code, body := postNALAs(t, ts, hubKS, []protocol.Area{old, validationArea("gen.new", "New", "x")})
	if code != http.StatusOK {
		t.Errorf("NAL keeping the old area as it was: status %d (%s), want 200", code, body)
	}
}

func newValidationKeystore(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(fmt.Sprintf("%s/node.key", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return ks
}
