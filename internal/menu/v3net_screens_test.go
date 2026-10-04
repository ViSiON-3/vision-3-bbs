package menu

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type v3netScreenProvider struct {
	fakeV3NetStatus
	nodeID       string
	hubActive    bool
	leafCount    int
	leafNetworks []string
	registryURL  string
}

func (f *v3netScreenProvider) NodeID() string         { return f.nodeID }
func (f *v3netScreenProvider) HubActive() bool        { return f.hubActive }
func (f *v3netScreenProvider) LeafCount() int         { return f.leafCount }
func (f *v3netScreenProvider) LeafNetworks() []string { return f.leafNetworks }
func (f *v3netScreenProvider) RegistryURL() string    { return f.registryURL }

func TestV3NetStatusScreenReportsConfiguredState(t *testing.T) {
	env := newMenuEnv(t)
	env.e.V3NetStatus = &v3netScreenProvider{
		nodeID: "NODE-42", hubActive: true, leafCount: 2,
		leafNetworks: []string{"retro-net", "bbs-test"},
	}

	r := env.runCmd("V3NETSTATUS", env.sysop, "", "\r")
	if r.err != nil {
		t.Fatalf("V3NETSTATUS: %v", r.err)
	}
	if !r.has("V3Net Status", "Node ID", "NODE-42", "Hub Mode", "Active", "Subscriptions", "2", "retro-net", "bbs-test") {
		t.Fatalf("status screen omitted configured state:\n%s", r.text())
	}
}

func TestV3NetStatusScreenReportsDisabledService(t *testing.T) {
	env := newMenuEnv(t)
	env.e.V3NetStatus = nil

	r := env.runCmd("V3NETSTATUS", env.sysop, "", "\r")
	if r.err != nil || !r.has("V3Net Status", "V3Net is disabled in server configuration") {
		t.Fatalf("disabled status screen: err=%v\n%s", r.err, r.text())
	}
}

func TestV3NetRegistryScreenListsAndMarksSubscriptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"networks":[{"name":"retro-net","description":"A retro bulletin board network","hub_url":"https://hub.example"},{"name":"new-net","description":"New network","hub_url":"https://new.example"}]}`)
	}))
	t.Cleanup(server.Close)

	env := newMenuEnv(t)
	env.e.V3NetStatus = &v3netScreenProvider{
		registryURL:  server.URL,
		leafNetworks: []string{"retro-net"},
	}

	r := env.runCmd("V3NETREGISTRY", env.sysop, "", "\r")
	if r.err != nil {
		t.Fatalf("V3NETREGISTRY: %v", r.err)
	}
	if !r.has("V3Net Network Registry", "retro-net", "A retro bulletin board net..", "https://hub.example", "new-net", "2 network(s) available", "subscribed") {
		t.Fatalf("registry screen missing entries:\n%s", r.text())
	}
	if !strings.Contains(r.text(), "* retro-net") {
		t.Fatalf("subscribed network lacks its marker:\n%s", r.text())
	}
}

func TestV3NetRegistryScreenReportsFetchFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	env := newMenuEnv(t)
	env.e.V3NetStatus = &v3netScreenProvider{registryURL: server.URL}
	r := env.runCmd("V3NETREGISTRY", env.sysop, "", "\r")
	if r.err != nil || !r.has("V3Net Network Registry", "Error fetching registry") {
		t.Fatalf("registry error screen: err=%v\n%s", r.err, r.text())
	}
}
