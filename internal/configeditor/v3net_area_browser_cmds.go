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

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
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
		var nal protocol.NAL
		if err := json.NewDecoder(resp.Body).Decode(&nal); err != nil {
			return fetchNALMsg{err: fmt.Errorf("decode NAL: %w", err)}
		}
		return fetchNALMsg{areas: nal.Areas}
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
