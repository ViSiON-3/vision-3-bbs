// Command v3net-handover hands a V3Net network's coordinator role to a new
// key. It loads the outgoing coordinator's keystore (the hub's current key),
// signs a coordinator handover naming the new key, and POSTs it to the hub,
// which serves it with the NAL so subscribers move their pinned keys
// themselves. Run it before the hub switches to the new key.
//
// Usage:
//
//	go run ./cmd/v3net-handover -keystore data/v3net.key -hub http://localhost:8765 -network felonynet -new-key <base64 public key>
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func main() {
	keystorePath := flag.String("keystore", "", "path to the outgoing coordinator's v3net.key")
	hubURL := flag.String("hub", "http://localhost:8765", "hub URL")
	network := flag.String("network", "", "network name (e.g. felonynet)")
	newKey := flag.String("new-key", "", "the new coordinator's public key (standard base64)")
	newKeystore := flag.String("new-keystore", "", "or: path to the new coordinator's v3net.key, to read its public key from")
	dryRun := flag.Bool("dry-run", false, "print the signed handover instead of sending it")
	flag.Parse()

	if *keystorePath == "" || *network == "" || (*newKey == "") == (*newKeystore == "") {
		fmt.Fprintln(os.Stderr, "need -keystore, -network and exactly one of -new-key or -new-keystore")
		flag.Usage()
		os.Exit(1)
	}

	ks := loadKeystore(*keystorePath)
	to := strings.TrimSpace(*newKey)
	if *newKeystore != "" {
		to = loadKeystore(*newKeystore).PubKeyBase64()
	}

	h, err := protocol.NewCoordHandover(ks, *network, to, time.Now())
	if err != nil {
		logging.Fatal("sign handover", "error", err)
	}
	body, err := json.Marshal(h)
	if err != nil {
		logging.Fatal("marshal handover", "error", err)
	}
	fmt.Printf("Handover: network=%s\n  from %s\n  to   %s\n", h.Network, h.OldKeyB64, h.NewKeyB64)
	if *dryRun {
		fmt.Println(string(body))
		return
	}

	path := fmt.Sprintf("/v3net/v1/%s/nal/handover", *network)
	bodyHash := sha256.Sum256(body)
	dateUTC := time.Now().UTC().Format(http.TimeFormat)
	sig, err := ks.Sign(http.MethodPost, path, dateUTC, hex.EncodeToString(bodyHash[:]))
	if err != nil {
		logging.Fatal("sign request", "error", err)
	}
	req, err := http.NewRequest(http.MethodPost, *hubURL+path, bytes.NewReader(body))
	if err != nil {
		logging.Fatal("create request", "error", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateUTC)
	req.Header.Set("X-V3Net-Node-ID", ks.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logging.Fatal("POST handover", "error", err)
	}
	defer func() { _ = resp.Body.Close() }() // response fully consumed below

	// Diagnostic output only: cap the read; truncation is acceptable.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	fmt.Printf("Response: %d %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	fmt.Println("Handover published. Restart the hub with the new key to sign the NAL with it.")
}

// loadKeystore loads an existing keystore. keystore.Load would generate a
// missing one, so a missing file is refused first.
func loadKeystore(path string) *keystore.Keystore {
	if _, err := os.Stat(path); err != nil {
		logging.Fatal("keystore not found", "path", path, "error", err)
	}
	ks, _, err := keystore.Load(path)
	if err != nil {
		logging.Fatal("load keystore", "path", path, "error", err)
	}
	return ks
}
