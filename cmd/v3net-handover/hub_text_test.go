package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const hostileHubText = "Evil\x1b[2J\x1b]0;owned\x07"

func TestCheckHubKeyErrorsDoNotCarryControls(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"error body": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, hostileHubText, http.StatusInternalServerError)
		},
		"hub key": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{HubPubKeyB64: hostileHubText})
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			ts := httptest.NewServer(handler)
			defer ts.Close()
			_, err := checkHubKey(ts.Client(), ts.URL, "testnet", "from", "to")
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.ContainsAny(err.Error(), "\x1b\x07") {
				t.Errorf("error holds control characters: %q", err)
			}
		})
	}
}

func TestCleanHistory(t *testing.T) {
	h := &protocol.SignerHistory{
		NALKeyB64: hostileHubText, KeyInForceB64: hostileHubText, PrevSigners: []string{hostileHubText},
		Handovers: []protocol.HandoverLink{{OldKeyB64: hostileHubText, NewKeyB64: hostileHubText}},
	}
	cleanHistory(h)
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	// Marshalled, a control character would show as a \u escape.
	if strings.Contains(string(data), `\u00`) {
		t.Errorf("history holds control characters: %s", data)
	}
}
