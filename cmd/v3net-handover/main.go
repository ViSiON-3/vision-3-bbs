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
// When the hub's key changed more than once without a handover (A to B to
// C), -via names the keystores of the keys in between, oldest first: the
// tool then signs a chain, A to B with A's key and B to C with B's, and
// posts it as one request, which a hub running C takes whole or not at all.
//
// Before posting, the tool asks the hub which keys it recorded signing the NAL
// (GET {network}/nal/signers), prints the chain the hub needs, and refuses a
// chain that does not match. It trusts that reply only if it verifies under
// the key the operator named for the hub. A hub without the endpoint is not
// checked: the tool says so and posts, and the hub's own checks still apply.
//
// It checks the signature on the reply to the post the same way. A reply that
// does not verify proves nothing either way, so the tool reports the outcome
// as unknown and says how to find out, and exits with status 3. An older hub
// does not sign at all, and its status is taken as sent.
//
// -dry-run prints the signed handover, makes the same checks and reports what
// a real run would do, without posting. A hub it cannot reach is not an error:
// it says it could not check, so the handover can still be signed offline. A
// check that would stop a real run exits non-zero.
//
// It exits 0 on success, 1 on a failure or refusal, 2 for a flag it does not
// know, and 3 for the unknown outcome above.
//
// Usage:
//
//	go run ./cmd/v3net-handover -keystore data/v3net.key -hub http://localhost:8765 -network felonynet -new-key <base64 public key>
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
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
	via := flag.String("via", "", "comma-separated paths to the v3net.key of each key the hub ran between -keystore's and the new key, oldest first")
	omitLost := flag.Bool("omit-lost-keys", false, "post a chain that leaves out keys the hub recorded, because their keystores are lost; subscribers still on those keys re-pin by hand")
	dryRun := flag.Bool("dry-run", false, "print the signed handover and check it against the hub, if it can be reached, instead of sending it")
	flag.Parse()

	if *keystorePath == "" || *network == "" || (*newKey == "") == (*newKeystore == "") {
		fmt.Fprintln(os.Stderr, "need -keystore, -network and exactly one of -new-key or -new-keystore")
		flag.Usage()
		os.Exit(1)
	}

	signers := []*keystore.Keystore{loadKeystore(*keystorePath)}
	for _, path := range strings.Split(*via, ",") {
		if path = strings.TrimSpace(path); path != "" {
			signers = append(signers, loadKeystore(path))
		}
	}
	to := strings.TrimSpace(*newKey)
	if *newKeystore != "" {
		to = loadKeystore(*newKeystore).PubKeyBase64()
	}

	chain, err := signChain(signers, *network, to, time.Now())
	if err != nil {
		logging.Fatal("sign handover", "error", err)
	}
	body, err := chainBody(chain)
	if err != nil {
		logging.Fatal("marshal handover", "error", err)
	}
	fmt.Printf("Handover: network=%s\n", *network)
	for _, h := range chain {
		fmt.Printf("  from %s\n  to   %s\n", h.OldKeyB64, h.NewKeyB64)
	}
	if *dryRun {
		fmt.Println(string(body))
	}

	// The handover's own signature is what the hub checks; the request only
	// has to come from a node subscribed to the network. That is the old
	// key's node unless its registration was deleted, when the hub's current
	// key, which the hub registers itself, serves instead.
	sender := signers[0]
	if *authKeystore != "" {
		sender = loadKeystore(*authKeystore)
	}
	if *dryRun {
		client := &http.Client{Timeout: dryRunTimeout}
		if !dryRunCheck(os.Stdout, os.Stderr, client, *hubURL, chain, sender, *omitLost) {
			os.Exit(1)
		}
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	from, final := chain[0].OldKeyB64, chain[len(chain)-1].NewKeyB64
	late, err := checkHubKey(client, *hubURL, *network, from, final)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if late {
		fmt.Println("The hub already runs the new key. Posting the handover anyway: the hub accepts it")
		fmt.Println("and signs the NAL with the new key, so subscribers follow without re-pinning.")
	}

	// checkHubKey left the hub on one of the two keys the operator named, so
	// /info, which is unsigned, only chooses between those.
	hubKey := from
	if late {
		hubKey = final
	}
	hubSigns, err := checkHistory(os.Stdout, client, *hubURL, chain, hubKey, sender, *omitLost)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	switch publish(os.Stdout, os.Stderr, client, *hubURL, chain, body, sender, *authKeystore != "", late, hubKey, hubSigns) {
	case postSucceeded:
	case postUnknown:
		os.Exit(exitUnknownOutcome)
	default:
		os.Exit(1)
	}
}

// exitUnknownOutcome is the exit status when the post may or may not have
// taken effect, so a script does not take a post that worked for one that
// failed. It is not 2, which the flag package exits with for a bad flag.
const exitUnknownOutcome = 3

// postOutcome is what publish found out about a post.
type postOutcome int

const (
	postFailed    postOutcome = iota // it failed, or the hub refused it
	postSucceeded                    // the hub took it, and did what was asked
	postUnknown                      // nothing shows whether it took effect
)

// dryRunTimeout bounds each request of a dry run, which an operator may run
// with no hub in reach.
const dryRunTimeout = 10 * time.Second

// errHubUnreachable means no answer came from the hub at all, as opposed to
// an answer the tool would act on.
var errHubUnreachable = errors.New("the hub could not be reached")

// signChain signs the handovers of network from each signer's key to the
// next signer's, and from the last signer's to to, each with the key it
// hands over from.
func signChain(signers []*keystore.Keystore, network, to string, issued time.Time) ([]protocol.CoordHandover, error) {
	chain := make([]protocol.CoordHandover, 0, len(signers))
	for i, s := range signers {
		next := to
		if i+1 < len(signers) {
			next = signers[i+1].PubKeyBase64()
		}
		h, err := protocol.NewCoordHandover(s, network, next, issued)
		if err != nil {
			return nil, err
		}
		chain = append(chain, *h)
	}
	return chain, nil
}

// chainBody returns the request body for chain: the handover itself when
// there is one, which every hub takes, or the array of them, which only a
// hub that takes chains does (an older one answers 400).
func chainBody(chain []protocol.CoordHandover) ([]byte, error) {
	if len(chain) == 1 {
		return json.Marshal(chain[0])
	}
	return json.Marshal(chain)
}

// dryRunCheck makes the checks a real run makes before posting and reports to
// out and errOut what that run would do. It posts nothing. It returns false
// when a real run would stop, and true otherwise, including when the hub
// cannot be reached, or has no history to give: the output then says the chain
// was not checked.
func dryRunCheck(out, errOut io.Writer, client *http.Client, hubURL string, chain []protocol.CoordHandover, sender *keystore.Keystore, omitLost bool) bool {
	from, final := chain[0].OldKeyB64, chain[len(chain)-1].NewKeyB64
	late, err := checkHubKey(client, hubURL, chain[0].Network, from, final)
	if errors.Is(err, errHubUnreachable) {
		_, _ = fmt.Fprintf(out, "Could not check the chain: %v.\nDry run: nothing was posted, and the chain has not been checked.\n", err)
		return true
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Dry run: a real run would stop here: %v\nNothing was posted.\n", err)
		return false
	}
	hubKey := from
	if late {
		hubKey = final
		_, _ = fmt.Fprintln(out, "The hub already runs the new key. A real run posts the handover anyway: the hub accepts it")
		_, _ = fmt.Fprintln(out, "and signs the NAL with the new key, so subscribers follow without re-pinning.")
	}
	result, err := historyVerdict(client, hubURL, chain, hubKey, sender, omitLost)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Dry run: a real run would stop here: %v\nNothing was posted.\n", err)
		return false
	}
	if result.why != "" {
		_, _ = fmt.Fprintf(out, "Could not check the chain against the hub's signer history: %s.\nDry run: nothing was posted. A real run would post it, and the hub would still check the chain.\n", result.why)
		return true
	}
	_, _ = fmt.Fprintln(out, result.note)
	_, _ = fmt.Fprintln(out, "Dry run: the checks pass and a real run would post this chain. Nothing was posted.")
	return true
}

// reportUnknownOutcome tells the operator that nothing shows whether the post
// took effect, and how to find out.
func reportUnknownOutcome(errOut io.Writer, network string) {
	_, _ = fmt.Fprintln(errOut, "The post may or may not have taken effect. To find out, run this command again with -dry-run:")
	_, _ = fmt.Fprintln(errOut, "\"the hub already holds this chain\" means it did. A hub too old to say can be checked by looking")
	_, _ = fmt.Fprintf(errOut, "for the handover in coordinator_handovers at /v3net/v1/%s/nal, or in the hub's log.\n", network)
	_, _ = fmt.Fprintln(errOut, "Posting again is safe: the hub does not store a handover twice.")
}

// publish POSTs the handover chain (body is its JSON) to the hub as sender
// and reports the outcome to out and errOut, also returning it. For a late
// handover, success means the hub serves the NAL
// signed by the new key with a chain from the outgoing key, checked by
// fetching it, not just a 200.
//
// The reply is checked as fetchHistory checks its own: under hubKey, with a
// nonce fresh for this request. One that fails, or one with no signature from
// a hub known to sign (hubSigns), says nothing about the outcome, since the
// post went out before it came back, so publish returns postUnknown without
// calling it a success or a refusal. So does no reply at all. An unsigned reply from a
// hub not known to sign is an older hub's, and its status is taken as sent.
func publish(out, errOut io.Writer, client *http.Client, hubURL string, chain []protocol.CoordHandover, body []byte, sender *keystore.Keystore, authSet, late bool, hubKey string, hubSigns bool) postOutcome {
	network := chain[0].Network
	pub, err := decodeHubKey(hubKey)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return postFailed
	}
	path := fmt.Sprintf("/v3net/v1/%s/nal/handover", network)
	bodyHash := sha256.Sum256(body)
	dateUTC := time.Now().UTC().Format(http.TimeFormat)
	sig, err := sender.Sign(http.MethodPost, path, dateUTC, hex.EncodeToString(bodyHash[:]))
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "sign request: %v\n", err)
		return postFailed
	}
	nonce, err := protocol.NewNonce()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "make request nonce: %v\n", err)
		return postFailed
	}
	req, err := http.NewRequest(http.MethodPost, hubURL+path, bytes.NewReader(body))
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "create request: %v\n", err)
		return postFailed
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateUTC)
	req.Header.Set("X-V3Net-Node-ID", sender.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)
	req.Header.Set(protocol.HeaderNonce, nonce)

	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "POST handover: %v\n", err)
		reportUnknownOutcome(errOut, network)
		return postUnknown
	}
	defer func() { _ = resp.Body.Close() }() // response fully consumed below

	// Diagnostic output only: cap the read; truncation is acceptable.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	// Quoted when unverified: it can say anything, including what a success
	// looks like.
	respSig := resp.Header.Get(protocol.HeaderHubSignature)
	unverified := ""
	switch {
	case respSig != "":
		if protocol.VerifyResponse(pub, sig, nonce, resp.StatusCode, respBody, respSig) != nil {
			unverified = fmt.Sprintf("its signature does not verify under the hub key %s", hubKey)
		}
	case hubSigns:
		unverified = "it is unsigned, though this hub signed its reply about its signers"
	}
	if unverified != "" {
		_, _ = fmt.Fprintf(errOut, "Response (not verified): %d %q\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
		_, _ = fmt.Fprintf(errOut, "The hub's reply to the post cannot be trusted: %s (check -hub, and any proxy in front of the hub).\n", unverified)
		reportUnknownOutcome(errOut, network)
		return postUnknown
	}
	_, _ = fmt.Fprintf(out, "Response: %d %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
	if respSig == "" {
		_, _ = fmt.Fprintln(out, "The hub did not sign this response, as a hub that predates signed responses does not; its status is taken as sent.")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized && !authSet:
		_, _ = fmt.Fprintln(errOut, "The hub did not accept the request from the outgoing key's node; if its registration")
		_, _ = fmt.Fprintln(errOut, "was deleted, send it as another subscribed node with -auth-keystore (the hub's current key will do).")
		return postFailed
	case resp.StatusCode == http.StatusInternalServerError && late:
		_, _ = fmt.Fprintln(errOut, "The hub holds the handover but could not sign the NAL with the new key, so subscribers")
		_, _ = fmt.Fprintln(errOut, "are still paused. Check the hub log, fix the cause, then run this command again to retry.")
		return postFailed
	case resp.StatusCode != http.StatusOK:
		return postFailed
	}
	if !late {
		_, _ = fmt.Fprintln(out, "Handover published. Restart the hub with the new key to sign the NAL with it.")
		return postSucceeded
	}
	if err := checkServedNAL(client, hubURL, network, chain[0].OldKeyB64, chain[len(chain)-1].NewKeyB64); err != nil {
		_, _ = fmt.Fprintf(errOut, "The hub accepted the handover, but %v.\n", err)
		_, _ = fmt.Fprintln(errOut, "Subscribers may still be paused. Check the hub log, then run this command again to retry.")
		return postFailed
	}
	_, _ = fmt.Fprintln(out, "Handover published. The hub now serves the NAL signed by the new key with the handover;")
	_, _ = fmt.Fprintln(out, "subscribers move their pins at their next check (paused ones within five minutes).")
	return postSucceeded
}

// checkServedNAL fetches network's NAL and returns an error unless it
// verifies, is signed by the new key to and carries a chain of handovers to
// it from the outgoing key from.
func checkServedNAL(client *http.Client, hubURL, network, from, to string) error {
	n, err := nal.Fetch(context.Background(), fmt.Sprintf("%s/v3net/v1/%s/nal", hubURL, network), client)
	if err != nil {
		return fmt.Errorf("the NAL could not be fetched to confirm it: %w", err)
	}
	if err := nal.Verify(n); err != nil {
		return fmt.Errorf("the NAL it serves does not verify: %w", err)
	}
	if n.CoordPubKeyB64 != to {
		return fmt.Errorf("it still serves a NAL signed by %s", n.CoordPubKeyB64)
	}
	if _, err := nal.FollowHandovers(network, from, to, n.Handovers); err != nil {
		return fmt.Errorf("the NAL it serves carries no handover from the outgoing key")
	}
	return nil
}

// checkHubKey fetches the network's /info and compares the hub's key with
// the outgoing key from and the new key to. It reports late when the hub
// already runs the new key, and returns an error when the hub runs neither
// key or does not say which it runs: the hub would refuse the handover, or
// take one it can never act on, or -hub names the wrong hub.
func checkHubKey(client *http.Client, hubURL, network, from, to string) (late bool, err error) {
	url := fmt.Sprintf("%s/v3net/v1/%s/info", hubURL, network)
	resp, err := client.Get(url)
	if err != nil {
		// A response with the error is a redirect that was refused, such as a
		// loop: something answered, so this is not a hub out of reach.
		if resp != nil {
			return false, fmt.Errorf("could not check the hub's key: %w", err)
		}
		return false, fmt.Errorf("%w: %w", errHubUnreachable, err)
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
	case from:
		return false, nil
	case to:
		return true, nil
	case "":
		return false, fmt.Errorf("the hub does not publish its key at %s; refusing to post the handover", url)
	default:
		return false, fmt.Errorf("the hub runs key %s, which is neither the outgoing key %s nor the new key %s; refusing to post the handover (check -hub, -network and the keystores)",
			info.HubPubKeyB64, from, to)
	}
}

// decodeHubKey returns the public key whose base64 form is hubKey.
func decodeHubKey(hubKey string) (ed25519.PublicKey, error) {
	rawKey, err := base64.StdEncoding.DecodeString(hubKey)
	if err != nil || len(rawKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("the hub key %s is not a public key", hubKey)
	}
	return ed25519.PublicKey(rawKey), nil
}

// fetchHistory asks the hub for network's signer history as sender. It returns
// the history once its reply verifies under hubKey, one of the keys the
// operator named, with a nonce fresh for this request so an old reply cannot
// be replayed. With no history to go on it returns nil and why not: a hub
// without the endpoint answers 404, one that predates hub signatures sends no
// signature, and a sender it does not know gets 401. A signature that fails,
// or a 200 with none, which no hub with the endpoint sends, is an error: that
// reply is not from the hub the operator named. signed says the reply carried
// a signature that verified, so the hub signs its responses.
func fetchHistory(client *http.Client, hubURL, network, hubKey string, sender *keystore.Keystore) (history *protocol.SignerHistory, why string, signed bool, err error) {
	pub, err := decodeHubKey(hubKey)
	if err != nil {
		return nil, "", false, err
	}
	path := fmt.Sprintf("/v3net/v1/%s/nal/signers", network)
	bodyHash := sha256.Sum256(nil)
	dateUTC := time.Now().UTC().Format(http.TimeFormat)
	sig, err := sender.Sign(http.MethodGet, path, dateUTC, hex.EncodeToString(bodyHash[:]))
	if err != nil {
		return nil, "", false, fmt.Errorf("sign request: %w", err)
	}
	nonce, err := protocol.NewNonce()
	if err != nil {
		return nil, "", false, fmt.Errorf("make request nonce: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+path, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Date", dateUTC)
	req.Header.Set("X-V3Net-Node-ID", sender.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)
	req.Header.Set(protocol.HeaderNonce, nonce)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err.Error(), false, nil
	}
	defer func() { _ = resp.Body.Close() }() // read side
	// Small JSON; a reply cut short fails its signature.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	respSig := resp.Header.Get(protocol.HeaderHubSignature)
	switch {
	case respSig != "":
		if err := protocol.VerifyResponse(pub, sig, nonce, resp.StatusCode, body, respSig); err != nil {
			return nil, "", false, fmt.Errorf("the hub's reply about its signers does not verify under the hub key %s; refusing to post the handover (check -hub and the keystores)", hubKey)
		}
	case resp.StatusCode == http.StatusOK:
		return nil, "", false, fmt.Errorf("the hub's reply about its signers is unsigned; refusing to post the handover (check -hub, and any proxy in front of the hub)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("the hub answered %d (a hub that predates the check, or one that does not know the sending node)", resp.StatusCode), respSig != "", nil
	}
	history = &protocol.SignerHistory{}
	if err := json.Unmarshal(body, history); err != nil {
		return nil, fmt.Sprintf("its reply could not be read: %v", err), true, nil
	}
	return history, "", true, nil
}

// historyResult is what historyVerdict found: a note for the operator on a
// chain that matches, or why the hub's history could not be had. hubSigns
// says the hub signed its reply.
type historyResult struct {
	note     string
	why      string
	hubSigns bool
}

// historyVerdict checks chain against the signer history of the hub running
// hubKey. It returns an error if the chain does not match or the reply does
// not verify.
func historyVerdict(client *http.Client, hubURL string, chain []protocol.CoordHandover, hubKey string, sender *keystore.Keystore, omitLost bool) (historyResult, error) {
	history, why, signed, err := fetchHistory(client, hubURL, chain[0].Network, hubKey, sender)
	if err != nil {
		return historyResult{}, err
	}
	if history == nil {
		return historyResult{why: why, hubSigns: signed}, nil
	}
	note, err := matchChain(history, hubKey, chain, omitLost)
	if err != nil {
		return historyResult{}, err
	}
	return historyResult{note: note, hubSigns: signed}, nil
}

// checkHistory checks chain against the signer history of the hub running
// hubKey and reports to out, returning whether the hub signed its reply. It
// returns an error if the chain does not match. It says so and returns nil
// when the hub has no history to give: the post goes ahead as it always has,
// and the hub still checks the chain itself.
func checkHistory(out io.Writer, client *http.Client, hubURL string, chain []protocol.CoordHandover, hubKey string, sender *keystore.Keystore, omitLost bool) (hubSigns bool, err error) {
	result, err := historyVerdict(client, hubURL, chain, hubKey, sender, omitLost)
	if err != nil {
		return false, err
	}
	if result.why != "" {
		_, _ = fmt.Fprintf(out, "Could not check the chain against the hub's signer history: %s.\nPosting anyway; the hub still checks the chain.\n", result.why)
		return result.hubSigns, nil
	}
	_, _ = fmt.Fprintln(out, result.note)
	return result.hubSigns, nil
}

// matchChain compares chain with what the hub running hubKey needs, using the
// hub's own rules (nal.CheckChainShape, nal.CheckPostedChain), and returns a
// line for the operator, or an error naming the chain to post. Beyond those
// rules it refuses a chain the hub would take but that leaves out a recorded
// key, since subscribers still on that key would be stranded and, once a
// handover is held, the hub takes no more from recorded keys. omitLost allows
// that when the key's keystore is gone; it never lets through a chain the hub
// would refuse.
func matchChain(history *protocol.SignerHistory, hubKey string, chain []protocol.CoordHandover, omitLost bool) (string, error) {
	held := make([]protocol.CoordHandover, 0, len(history.Handovers))
	for _, link := range history.Handovers {
		held = append(held, protocol.CoordHandover{OldKeyB64: link.OldKeyB64, NewKeyB64: link.NewKeyB64})
	}
	olds := make([]string, 0, len(chain))
	for _, ho := range chain {
		olds = append(olds, ho.OldKeyB64)
	}
	to := chain[len(chain)-1].NewKeyB64

	// earlier are the recorded keys other than the NAL's own, oldest first;
	// signers adds the NAL's key when the hub has not re-signed it yet.
	earlier := make([]string, 0, len(history.PrevSigners))
	for _, key := range history.PrevSigners {
		if key != history.NALKeyB64 {
			earlier = append(earlier, key)
		}
	}
	signers := earlier
	if history.NALKeyB64 != hubKey {
		signers = append(earlier[:len(earlier):len(earlier)], history.NALKeyB64)
	}
	wentBack := slices.Contains(history.PrevSigners, hubKey)

	err := nal.CheckChainShape(chain)
	var check nal.ChainCheck
	if err == nil {
		check, err = nal.CheckPostedChain(history.NALKeyB64, held, chain, history.PrevSigners, hubKey)
	}
	if err != nil {
		msg := fmt.Sprintf("the hub would refuse this chain: %v. Refusing to post it", err)
		switch {
		case len(held) > 0:
			msg += fmt.Sprintf(".\nThe key in force is %s", history.KeyInForceB64)
		case wentBack:
			msg += fmt.Sprintf(".\nThe hub runs %s, which already signed this network's NAL, so it takes no handover back to that key. Run the hub with a new key, or re-pin subscribers by hand", hubKey)
		case len(signers) > 0 && (to == hubKey || history.NALKeyB64 != hubKey):
			msg += ".\n" + describeChain(signers, hubKey)
		}
		return "", errors.New(msg)
	}

	switch {
	case len(check.Fresh) == 0:
		return "Signer history: the hub already holds this chain; posting it again retries the re-sign.", nil
	case len(held) > 0:
		return "Signer history: the chain continues from the key in force.", nil
	case check.FromPrevSigners:
		needed := describeChain(signers, hubKey)
		if slices.Equal(olds, signers) {
			return "Signer history: the chain matches the keys the hub recorded.\n" + needed, nil
		}
		if !omitLost {
			return "", fmt.Errorf("this chain leaves out keys the hub recorded, and subscribers still on them could then only re-pin by hand. Pass all of them, or -omit-lost-keys if their keystores are lost.\n%s", needed)
		}
		return fmt.Sprintf("Signer history: leaving out %d recorded key(s); subscribers on them re-pin by hand.\n%s", len(signers)-len(olds), needed), nil
	case len(earlier) == 0:
		return "Signer history: the chain starts at the key in force.", nil
	case omitLost:
		return fmt.Sprintf("Signer history: leaving out %d recorded key(s); subscribers on them re-pin by hand.", len(earlier)), nil
	case wentBack:
		// The hub cannot bridge to a key it already used, but a hub on the
		// new key can take the whole chain through the NAL's key.
		bridge := append(earlier[:len(earlier):len(earlier)], history.NALKeyB64)
		return "", fmt.Errorf("the hub recorded earlier signers no handover covers, and it stops taking handovers from them once it holds this one. It already signed the NAL with the key it runs, so it cannot bridge them itself: restart the hub with the new key, then post the whole chain, or pass -omit-lost-keys if their keystores are lost.\n%s", describeChain(bridge, to))
	default:
		return "", fmt.Errorf("the hub recorded earlier signers no handover covers, and it stops taking handovers from them once it holds this one. First bridge them, or pass -omit-lost-keys if their keystores are lost.\n%s", describeChain(earlier, hubKey))
	}
}

// describeChain returns the flags that post the chain from signers, oldest
// first, to to, as the operator would pass them.
func describeChain(signers []string, to string) string {
	var b strings.Builder
	b.WriteString("The chain the hub needs:\n")
	fmt.Fprintf(&b, "  -keystore  the keystore of %s\n", signers[0])
	if len(signers) > 1 {
		b.WriteString("  -via       the keystores of, in this order:\n")
		for _, key := range signers[1:] {
			fmt.Fprintf(&b, "              %s\n", key)
		}
	}
	fmt.Fprintf(&b, "  -new-key   %s", to)
	return b.String()
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
