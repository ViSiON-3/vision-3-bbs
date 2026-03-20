package configeditor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fetchNALMsg is the result of fetching the NAL from a hub.
type fetchNALMsg struct {
	areas []protocol.Area
	err   error
}

// subscribeAreasMsg is the result of a subscribe call.
type subscribeAreasMsg struct {
	statuses []protocol.AreaSubscriptionStatus
	err      error
}

// fetchHubNAL returns a tea.Cmd that GETs /v3net/v1/{network}/nal from the hub.
// This endpoint is public (no auth required).
func fetchHubNAL(hubURL, network string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 10 * time.Second}
		url := strings.TrimRight(hubURL, "/") + "/v3net/v1/" + network + "/nal"
		resp, err := client.Get(url)
		if err != nil {
			return fetchNALMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fetchNALMsg{err: fmt.Errorf("hub returned status %d", resp.StatusCode)}
		}
		var nal protocol.NAL
		if err := json.NewDecoder(resp.Body).Decode(&nal); err != nil {
			return fetchNALMsg{err: fmt.Errorf("decode NAL: %w", err)}
		}
		return fetchNALMsg{areas: nal.Areas}
	}
}

// subscribeToAreas returns a tea.Cmd that POSTs /v3net/v1/subscribe with area tags.
// The subscribe endpoint is unauthenticated (bootstrap step). The keystore is
// only needed to populate node_id and pubkey_b64 in the request body.
func subscribeToAreas(hubURL, network string, areaTags []string,
	nodeID, pubKeyB64, bbsName, bbsHost string) tea.Cmd {
	return func() tea.Msg {
		req := protocol.SubscribeRequest{
			Network:   network,
			NodeID:    nodeID,
			PubKeyB64: pubKeyB64,
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

		resp, err := client.Do(httpReq)
		if err != nil {
			return subscribeAreasMsg{err: err}
		}
		defer resp.Body.Close()

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
