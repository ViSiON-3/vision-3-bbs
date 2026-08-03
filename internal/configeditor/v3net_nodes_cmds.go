package configeditor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// fetchNodesMsg is the result of listing a hosted network's nodes.
type fetchNodesMsg struct {
	network string // network this fetch was issued for; guards stale responses
	nodes   []protocol.NodeInfo
	err     error
}

// nodeActionMsg is the result of an approve/ban/unban/remove call.
type nodeActionMsg struct {
	network string // network this action was issued for; guards stale responses
	nodeID  string
	action  string
	status  string // new status returned by the hub ("" for remove)
	err     error
}

// loadAdminKeystore loads the BBS keystore for signing admin requests.
// A missing file is an error — the TUI must never mint a new identity.
func loadAdminKeystore(path string) (*keystore.Keystore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("keystore not found at %s - has the BBS run once?", path)
	}
	ks, _, err := keystore.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load keystore: %w", err)
	}
	return ks, nil
}

// signedHubRequest builds a signed request to the local hub.
func signedHubRequest(ks *keystore.Keystore, method string, hubPort int, path string) (*http.Request, error) {
	emptyHash := sha256.Sum256(nil)
	bodySHA := hex.EncodeToString(emptyHash[:])
	dateStr := time.Now().UTC().Format(http.TimeFormat)

	sig, err := ks.Sign(method, path, dateStr, bodySHA)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", hubPort, path)
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Date", dateStr)
	req.Header.Set("X-V3Net-Node-ID", ks.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)
	return req, nil
}

// fetchHubNodes lists node registrations for a hosted network.
func fetchHubNodes(hubPort int, network, keystorePath string) tea.Cmd {
	return func() tea.Msg {
		ks, err := loadAdminKeystore(keystorePath)
		if err != nil {
			return fetchNodesMsg{network: network, err: err}
		}
		req, err := signedHubRequest(ks, "GET", hubPort, "/v3net/v1/"+network+"/nodes")
		if err != nil {
			return fetchNodesMsg{network: network, err: err}
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fetchNodesMsg{network: network, err: err}
		}
		defer func() { _ = resp.Body.Close() }() // read-only
		if resp.StatusCode != http.StatusOK {
			return fetchNodesMsg{network: network, err: fmt.Errorf("hub returned status %d", resp.StatusCode)}
		}
		var nodes []protocol.NodeInfo
		if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
			return fetchNodesMsg{network: network, err: fmt.Errorf("decode nodes: %w", err)}
		}
		return fetchNodesMsg{network: network, nodes: nodes}
	}
}

// nodeAction performs approve/ban/unban/remove on a node registration.
func nodeAction(hubPort int, network, keystorePath, nodeID, action string) tea.Cmd {
	return func() tea.Msg {
		ks, err := loadAdminKeystore(keystorePath)
		if err != nil {
			return nodeActionMsg{network: network, nodeID: nodeID, action: action, err: err}
		}
		path := "/v3net/v1/" + network + "/nodes/" + nodeID + "/" + action
		req, err := signedHubRequest(ks, "POST", hubPort, path)
		if err != nil {
			return nodeActionMsg{network: network, nodeID: nodeID, action: action, err: err}
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nodeActionMsg{network: network, nodeID: nodeID, action: action, err: err}
		}
		defer func() { _ = resp.Body.Close() }() // read-only
		if resp.StatusCode != http.StatusOK {
			return nodeActionMsg{network: network, nodeID: nodeID, action: action,
				err: fmt.Errorf("hub returned status %d", resp.StatusCode)}
		}
		var out struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nodeActionMsg{network: network, nodeID: nodeID, action: action, err: fmt.Errorf("decode response: %w", err)}
		}
		return nodeActionMsg{network: network, nodeID: nodeID, action: action, status: out.Status}
	}
}
