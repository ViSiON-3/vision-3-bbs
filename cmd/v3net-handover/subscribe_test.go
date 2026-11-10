package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// subscribeNode registers ks with the hub at hubURL as bbsName, signed as a
// leaf signs it, and returns the hub's status.
func subscribeNode(t *testing.T, hubURL string, ks *keystore.Keystore, bbsName string) int {
	t.Helper()
	body, _ := json.Marshal(protocol.SubscribeRequest{Network: "testnet", NodeID: ks.NodeID(),
		PubKeyB64: ks.PubKeyBase64(), BBSName: bbsName, BBSHost: "node.example.net"})
	req, err := http.NewRequest("POST", hubURL+"/v3net/v1/subscribe", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	hash := sha256.Sum256(body)
	sig, err := ks.Sign("POST", "/v3net/v1/subscribe", date, hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", date)
	req.Header.Set("X-V3Net-Node-ID", ks.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}
