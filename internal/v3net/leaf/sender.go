package leaf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// subscribe registers this leaf with the hub. This is the bootstrap step that
// must succeed before any authenticated requests will work.
func (l *Leaf) subscribe(ctx context.Context) error {
	req := protocol.SubscribeRequest{
		Network:   l.cfg.Network,
		NodeID:    l.cfg.Keystore.NodeID(),
		PubKeyB64: l.cfg.Keystore.PubKeyBase64(),
		BBSName:   l.cfg.BBSName,
		BBSHost:   l.cfg.BBSHost,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("leaf: marshal subscribe: %w", err)
	}

	url := l.cfg.HubURL + "/v3net/v1/subscribe"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("leaf: create subscribe request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("leaf: subscribe POST: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leaf: subscribe returned %d: %s", resp.StatusCode, string(body))
	}

	var sr protocol.SubscribeResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return fmt.Errorf("leaf: parse subscribe response: %w", err)
	}

	if sr.Status != "active" {
		return fmt.Errorf("leaf: subscription status %q (not active, hub may require manual approval)", sr.Status)
	}

	return nil
}

// SendMessage signs and POSTs a message to the hub.
func (l *Leaf) SendMessage(msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("leaf: marshal message: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/messages", l.cfg.Network)
	return l.signedPost(path, data)
}

// SendChat sends an inter-BBS chat message to the hub.
func (l *Leaf) SendChat(text, handle string) error {
	data, err := json.Marshal(protocol.ChatRequest{From: handle, Text: text})
	if err != nil {
		return fmt.Errorf("leaf: marshal chat: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/chat", l.cfg.Network)
	return l.signedPost(path, data)
}

// SendLogon notifies the hub of a user logon.
func (l *Leaf) SendLogon(handle string) error {
	return l.sendPresence(protocol.EventLogon, handle)
}

// SendLogoff notifies the hub of a user logoff.
func (l *Leaf) SendLogoff(handle string) error {
	return l.sendPresence(protocol.EventLogoff, handle)
}

func (l *Leaf) sendPresence(eventType, handle string) error {
	data, err := json.Marshal(protocol.PresenceRequest{Type: eventType, Handle: handle})
	if err != nil {
		return fmt.Errorf("leaf: marshal presence: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/presence", l.cfg.Network)
	return l.signedPost(path, data)
}

func (l *Leaf) signedPost(path string, body []byte) error {
	url := l.cfg.HubURL + path
	bodyHash := sha256.Sum256(body)
	bodySHA := hex.EncodeToString(bodyHash[:])
	dateStr := time.Now().UTC().Format(http.TimeFormat)

	sig, err := l.cfg.Keystore.Sign("POST", path, dateStr, bodySHA)
	if err != nil {
		return fmt.Errorf("leaf: sign request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("leaf: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateStr)
	req.Header.Set("X-V3Net-Node-ID", l.cfg.Keystore.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)

	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("leaf: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leaf: POST %s returned %d", path, resp.StatusCode)
	}
	return nil
}

func (l *Leaf) signedGet(path string) (*http.Response, error) {
	return l.signedGetWithContext(context.Background(), path)
}

func (l *Leaf) signedGetWithContext(ctx context.Context, path string) (*http.Response, error) {
	url := l.cfg.HubURL + path
	emptyHash := sha256.Sum256(nil)
	bodySHA := hex.EncodeToString(emptyHash[:])
	dateStr := time.Now().UTC().Format(http.TimeFormat)

	sig, err := l.cfg.Keystore.Sign("GET", path, dateStr, bodySHA)
	if err != nil {
		return nil, fmt.Errorf("leaf: sign request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("leaf: create request: %w", err)
	}
	req.Header.Set("Date", dateStr)
	req.Header.Set("X-V3Net-Node-ID", l.cfg.Keystore.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)

	return l.client.Do(req)
}
