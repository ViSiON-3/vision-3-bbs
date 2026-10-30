package configeditor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fetchNALMsg is the result of fetching the NAL from a hub.
type fetchNALMsg struct {
	areas []protocol.Area
	// coordKey is the coordinator key that signed the NAL, once verified.
	coordKey string
	// handedOverFrom is the pinned key the NAL's coordinator handovers led
	// from to coordKey, when the network moved keys since it was pinned;
	// handoverSteps is how many handovers that took. "" and 0 otherwise.
	handedOverFrom string
	handoverSteps  int
	// hubURL and network say which subscription the NAL was fetched for.
	hubURL, network string
	err             error
}

// registryKeys returns the coordinator and hub keys the registry entry the
// wizard was filled from publishes, provided the entry is for hubURL and
// network and the keys are well formed; "" for any that is not.
func (w *wizardState) registryKeys(hubURL, network string) (coord, hub string) {
	e := w.regEntry
	if e == nil || e.HubURL != hubURL || e.Name != network {
		return "", ""
	}
	if config.ValidV3NetPubKey(e.CoordPubKeyB64) {
		coord = strings.TrimSpace(e.CoordPubKeyB64)
	}
	if config.ValidV3NetPubKey(e.HubPubKeyB64) {
		hub = strings.TrimSpace(e.HubPubKeyB64)
	}
	return coord, hub
}

// browsedKey returns the coordinator key of the NAL the area browser last
// verified, if it was for hubURL and network, else "".
func (w *wizardState) browsedKey(hubURL, network string) string {
	if w.browsedHub != hubURL || w.browsedNetwork != network {
		return ""
	}
	return w.browsedCoordKey
}

// areaBrowserPin returns the coordinator key the area browser holds the
// fetched NAL to: the registry entry's key in the join wizard, or the
// subscription's pin when browsing from its record. "" means none is known
// yet; the signature and node ID are still checked.
func (m Model) areaBrowserPin() string {
	if m.wizard != nil && m.wizard.flow == "leaf" {
		coord, _ := m.wizard.registryKeys(m.areaBrowserHub, m.areaBrowserNetwork)
		return coord
	}
	if m.areaBrowserReturn == modeRecordEdit && m.configs != nil {
		idx := m.recordEditIdx
		if idx >= 0 && idx < len(m.configs.V3Net.Leaves) {
			l := m.configs.V3Net.Leaves[idx]
			if l.HubURL == m.areaBrowserHub && l.Network == m.areaBrowserNetwork {
				return l.CoordinatorPubKey
			}
		}
	}
	return ""
}

// verifyBrowsedNAL checks a NAL fetched for the area browser the way a leaf
// would: a valid signature, the expected network, a coordinator node ID that
// belongs to the signing key, and, when pin is set, that key being the
// pinned one or one the NAL's coordinator handovers lead to from the pin
// (see nal.FollowHandovers). A NAL that fails is not shown, so a hub that is
// impersonated cannot offer areas the sysop then subscribes to.
//
// When a handover chain was followed, it returns the chain; the pin itself
// is left as it is. The running leaf follows the same chain at its next
// fetch and stores the moved pin, so the browser only says the network
// moved keys.
func verifyBrowsedNAL(n *protocol.NAL, network, pin string) ([]protocol.CoordHandover, error) {
	if err := nal.Verify(n); err != nil {
		return nil, fmt.Errorf("NAL signature check failed: %w", err)
	}
	if n.Network != network {
		return nil, fmt.Errorf("hub served the NAL for %q, not %q", n.Network, network)
	}
	if err := nal.VerifyCoordNodeID(n); err != nil {
		return nil, fmt.Errorf("NAL coordinator check failed: %w", err)
	}
	if pin = strings.TrimSpace(pin); pin == "" || n.CoordPubKeyB64 == pin {
		return nil, nil
	}
	if len(n.Handovers) > 0 {
		if chain, err := nal.FollowHandovers(network, pin, n.CoordPubKeyB64, n.Handovers); err == nil {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("%w: expected %s, NAL signed by %s", nal.ErrCoordKeyMismatch, pin, n.CoordPubKeyB64)
}

// subscribeAreasMsg is the result of a subscribe call.
type subscribeAreasMsg struct {
	statuses []protocol.AreaSubscriptionStatus
	err      error
}

// fetchHubNAL returns a tea.Cmd that GETs /v3net/v1/{network}/nal from the hub
// and verifies it with verifyBrowsedNAL against pin (the coordinator key the
// subscription pins or the registry publishes; "" checks the signature and
// node ID only), following the coordinator handovers served with it from the
// pin. This endpoint is public (no auth required).
func fetchHubNAL(hubURL, network, pin string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 10 * time.Second}
		url := strings.TrimRight(hubURL, "/") + "/v3net/v1/" + network + "/nal"
		resp, err := client.Get(url)
		if err != nil {
			return fetchNALMsg{err: err}
		}
		defer func() { _ = resp.Body.Close() }() // read-only
		if resp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
			detail := strings.TrimSpace(string(body))
			if readErr != nil && detail == "" {
				detail = fmt.Sprintf("(failed to read error body: %v)", readErr)
			}
			if detail == "" {
				detail = "(no body)"
			}
			return fetchNALMsg{err: fmt.Errorf("hub returned status %d: %s", resp.StatusCode, detail)}
		}
		var n protocol.NAL
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&n); err != nil {
			return fetchNALMsg{err: fmt.Errorf("decode NAL: %w", err)}
		}
		chain, err := verifyBrowsedNAL(&n, network, pin)
		if err != nil {
			return fetchNALMsg{err: err}
		}
		msg := fetchNALMsg{areas: n.Areas, coordKey: n.CoordPubKeyB64, hubURL: hubURL, network: network}
		if len(chain) > 0 {
			msg.handedOverFrom, msg.handoverSteps = strings.TrimSpace(pin), len(chain)
		}
		return msg
	}
}

// subscribeToAreas returns a tea.Cmd that POSTs /v3net/v1/subscribe with area tags.
// The request is signed with ks: the hub ignores area tags on an unsigned
// subscribe from a node it already knows.
func subscribeToAreas(hubURL, network string, areaTags []string,
	ks *keystore.Keystore, bbsName, bbsHost string) tea.Cmd {
	return func() tea.Msg {
		req := protocol.SubscribeRequest{
			Network:   network,
			NodeID:    ks.NodeID(),
			PubKeyB64: ks.PubKeyBase64(),
			BBSName:   bbsName,
			BBSHost:   bbsHost,
			AreaTags:  areaTags,
		}
		data, err := json.Marshal(req)
		if err != nil {
			return subscribeAreasMsg{err: fmt.Errorf("marshal subscribe: %w", err)}
		}

		client := &http.Client{Timeout: 10 * time.Second}
		url := strings.TrimRight(hubURL, "/") + "/v3net/v1/subscribe"
		httpReq, err := http.NewRequest("POST", url, bytes.NewReader(data))
		if err != nil {
			return subscribeAreasMsg{err: err}
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if err := signRequest(httpReq, ks, data); err != nil {
			return subscribeAreasMsg{err: err}
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return subscribeAreasMsg{err: err}
		}
		defer func() { _ = resp.Body.Close() }() // read-only

		if resp.StatusCode != http.StatusOK {
			return subscribeAreasMsg{err: fmt.Errorf("subscribe returned status %d", resp.StatusCode)}
		}

		var sr protocol.SubscribeWithAreasResponse
		if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
			return subscribeAreasMsg{err: fmt.Errorf("decode subscribe response: %w", err)}
		}
		return subscribeAreasMsg{statuses: sr.Areas}
	}
}
