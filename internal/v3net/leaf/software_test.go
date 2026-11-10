package leaf

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// subscribeCapture is a hub that accepts every subscribe and keeps the last
// request body.
type subscribeCapture struct {
	mu   sync.Mutex
	last protocol.SubscribeRequest
}

func (c *subscribeCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.last = protocol.SubscribeRequest{}
	_ = json.Unmarshal(body, &c.last)
	c.mu.Unlock()
	_, _ = w.Write([]byte(`{"ok":true,"status":"active"}`))
}

// The subscribe request carries the board's software, with the features
// this leaf implements as its capabilities whatever the caller passed; a
// leaf given no software sends no report.
func TestSubscribeSendsSoftwareReport(t *testing.T) {
	hub := &subscribeCapture{}
	ts := httptest.NewServer(hub)
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	l.cfg.Software = &protocol.SoftwareReport{
		Name: protocol.SoftwareNameVision3, Version: "0.9.8", Build: "a23f12a7c0de",
		Capabilities: []string{"made.up"},
	}
	if err := l.subscribe(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := hub.last.Software
	if got == nil || got.String() != "ViSiON/3 0.9.8 (a23f12a7c0de)" {
		t.Fatalf("report = %+v", got)
	}
	if !slices.Equal(got.Capabilities, SupportedFeatures()) {
		t.Errorf("capabilities = %v, want %v", got.Capabilities, SupportedFeatures())
	}
	if err := got.Validate(); err != nil {
		t.Errorf("leaf sent a report the hub would drop: %v", err)
	}
	if len(l.cfg.Software.Capabilities) != 1 {
		t.Error("subscribe changed the caller's report")
	}

	l.cfg.Software = nil
	if err := l.subscribe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if hub.last.Software != nil {
		t.Errorf("no software configured, sent %+v", hub.last.Software)
	}
}

// Every feature the leaf names has upgrade advice to go with it.
func TestSupportedFeaturesAreAssessed(t *testing.T) {
	for _, f := range SupportedFeatures() {
		if !slices.ContainsFunc(protocol.FeatureRequirements, func(r protocol.FeatureRequirement) bool { return r.Feature == f }) {
			t.Errorf("%s is not in protocol.FeatureRequirements", f)
		}
	}
}
