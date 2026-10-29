// Command v3net-handover hands a V3Net network's coordinator role to a new
// key. It loads the outgoing coordinator's keystore (the hub's current key),
// signs a coordinator handover naming the new key, and POSTs it to the hub,
// which serves it with the NAL so subscribers move their pinned keys
// themselves. Run it before the hub switches to the new key if you can; a
// hub already running the new key also takes it, and signs the NAL with
// that key if it has not already. Before posting, it checks which key the
// hub runs and refuses a hub running neither key. The request is sent as
// the outgoing key's node, or as the node of -auth-keystore.
//
// Usage:
//
//	go run ./cmd/v3net-handover -keystore data/v3net.key -hub http://localhost:8765 -network felonynet -new-key <base64 public key>
package main

import (
	"bytes"
	"context"
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
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func main() {
	keystorePath := flag.String("keystore", "", "path to the outgoing coordinator's v3net.key")
	hubURL := flag.String("hub", "http://localhost:8765", "hub URL")
	network := flag.String("network", "", "network name (e.g. felonynet)")
	newKey := flag.String("new-key", "", "the new coordinator's public key (standard base64)")
	newKeystore := flag.String("new-keystore", "", "or: path to the new coordinator's v3net.key, to read its public key from")
	authKeystore := flag.String("auth-keystore", "", "path to the v3net.key of the subscribed node to send the request as (default: -keystore)")
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

	client := &http.Client{Timeout: 30 * time.Second}
	late, err := checkHubKey(client, *hubURL, *network, h)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if late {
		fmt.Println("The hub already runs the new key. Posting the handover anyway: the hub accepts it")
		fmt.Println("and signs the NAL with the new key, so subscribers follow without re-pinning.")
	}

	// The handover's own signature is what the hub checks; the request only
	// has to come from a node subscribed to the network. That is the old
	// key's node unless its registration was deleted, when the hub's current
	// key, which the hub registers itself, serves instead.
	sender := ks
	if *authKeystore != "" {
		sender = loadKeystore(*authKeystore)
	}
	if !publish(os.Stdout, os.Stderr, client, *hubURL, h, body, sender, *authKeystore != "", late) {
		os.Exit(1)
	}
}

// publish POSTs the handover h (body is its JSON) to the hub as sender and
// reports the outcome to out and errOut, returning whether it succeeded.
// For a late handover, success means the hub serves the NAL signed by the
// new key with the handover, checked by fetching it, not just a 200.
func publish(out, errOut io.Writer, client *http.Client, hubURL string, h *protocol.CoordHandover, body []byte, sender *keystore.Keystore, authSet, late bool) bool {
	path := fmt.Sprintf("/v3net/v1/%s/nal/handover", h.Network)
	bodyHash := sha256.Sum256(body)
	dateUTC := time.Now().UTC().Format(http.TimeFormat)
	sig, err := sender.Sign(http.MethodPost, path, dateUTC, hex.EncodeToString(bodyHash[:]))
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "sign request: %v\n", err)
		return false
	}
	req, err := http.NewRequest(http.MethodPost, hubURL+path, bytes.NewReader(body))
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "create request: %v\n", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateUTC)
	req.Header.Set("X-V3Net-Node-ID", sender.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)

	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "POST handover: %v\n", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }() // response fully consumed below

	// Diagnostic output only: cap the read; truncation is acceptable.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_, _ = fmt.Fprintf(out, "Response: %d %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
	switch {
	case resp.StatusCode == http.StatusUnauthorized && !authSet:
		_, _ = fmt.Fprintln(errOut, "The hub did not accept the request from the outgoing key's node; if its registration")
		_, _ = fmt.Fprintln(errOut, "was deleted, send it as another subscribed node with -auth-keystore (the hub's current key will do).")
		return false
	case resp.StatusCode == http.StatusInternalServerError && late:
		_, _ = fmt.Fprintln(errOut, "The hub holds the handover but could not sign the NAL with the new key, so subscribers")
		_, _ = fmt.Fprintln(errOut, "are still paused. Check the hub log, fix the cause, then run this command again to retry.")
		return false
	case resp.StatusCode != http.StatusOK:
		return false
	}
	if !late {
		_, _ = fmt.Fprintln(out, "Handover published. Restart the hub with the new key to sign the NAL with it.")
		return true
	}
	if err := checkServedNAL(client, hubURL, h); err != nil {
		_, _ = fmt.Fprintf(errOut, "The hub accepted the handover, but %v.\n", err)
		_, _ = fmt.Fprintln(errOut, "Subscribers may still be paused. Check the hub log, then run this command again to retry.")
		return false
	}
	_, _ = fmt.Fprintln(out, "Handover published. The hub now serves the NAL signed by the new key with the handover;")
	_, _ = fmt.Fprintln(out, "subscribers move their pins at their next check (paused ones within five minutes).")
	return true
}

// checkServedNAL fetches the network's NAL and returns an error unless it
// verifies, is signed by h's new key and carries h's old-to-new step.
func checkServedNAL(client *http.Client, hubURL string, h *protocol.CoordHandover) error {
	n, err := nal.Fetch(context.Background(), fmt.Sprintf("%s/v3net/v1/%s/nal", hubURL, h.Network), client)
	if err != nil {
		return fmt.Errorf("the NAL could not be fetched to confirm it: %w", err)
	}
	if err := nal.Verify(n); err != nil {
		return fmt.Errorf("the NAL it serves does not verify: %w", err)
	}
	if n.CoordPubKeyB64 != h.NewKeyB64 {
		return fmt.Errorf("it still serves a NAL signed by %s", n.CoordPubKeyB64)
	}
	if _, err := nal.FollowHandovers(h.Network, h.OldKeyB64, h.NewKeyB64, n.Handovers); err != nil {
		return fmt.Errorf("the NAL it serves carries no handover from the outgoing key")
	}
	return nil
}

// checkHubKey fetches the network's /info and compares the hub's key with
// the handover's keys. It reports late when the hub already runs the new
// key, and returns an error when the hub runs neither key or does not say
// which it runs: the hub would refuse the handover, or take one it can
// never act on, or -hub names the wrong hub.
func checkHubKey(client *http.Client, hubURL, network string, h *protocol.CoordHandover) (late bool, err error) {
	url := fmt.Sprintf("%s/v3net/v1/%s/info", hubURL, network)
	resp, err := client.Get(url)
	if err != nil {
		return false, fmt.Errorf("could not check the hub's key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read side
	// Small JSON; cap the read.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("could not check the hub's key: GET %s: %d %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var info protocol.NetworkInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return false, fmt.Errorf("could not check the hub's key: decode %s: %w", url, err)
	}
	switch info.HubPubKeyB64 {
	case h.OldKeyB64:
		return false, nil
	case h.NewKeyB64:
		return true, nil
	case "":
		return false, fmt.Errorf("the hub does not publish its key at %s; refusing to post the handover", url)
	default:
		return false, fmt.Errorf("the hub runs key %s, which is neither the outgoing key %s nor the new key %s; refusing to post the handover (check -hub, -network and the keystores)",
			info.HubPubKeyB64, h.OldKeyB64, h.NewKeyB64)
	}
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
