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
		AreaTags:  l.cfg.AreaTags,
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
	defer func() { _ = resp.Body.Close() }() // read side

	body, err := readBody(resp.Body, maxRespBytes)
	if err != nil {
		return fmt.Errorf("leaf: read subscribe response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leaf: subscribe returned %d: %s", resp.StatusCode, string(body))
	}

	var sr protocol.SubscribeWithAreasResponse
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
	return l.SendMessageCtx(context.Background(), msg)
}

// SendMessageCtx signs and POSTs a message to the hub with context.
func (l *Leaf) SendMessageCtx(ctx context.Context, msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("leaf: marshal message: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/messages", l.cfg.Network)
	return l.signedPostCtx(ctx, path, data)
}

// SendChat sends an inter-BBS chat message to the hub.
func (l *Leaf) SendChat(text, handle string) error {
	return l.SendChatCtx(context.Background(), text, handle)
}

// SendChatCtx sends an inter-BBS chat message to the hub with context.
// It joins the lobby room first, then posts the message.
func (l *Leaf) SendChatCtx(ctx context.Context, text, handle string) error {
	joinData, err := json.Marshal(protocol.ChatJoinRequest{Room: "lobby", Handle: handle})
	if err != nil {
		return fmt.Errorf("leaf: marshal chat join: %w", err)
	}
	joinPath := fmt.Sprintf("/v3net/v1/%s/chat/rooms/join", l.cfg.Network)
	joinResp, err := l.signedPostWithResponse(ctx, joinPath, joinData)
	if err != nil {
		return fmt.Errorf("leaf: chat join: %w", err)
	}
	drainBody(joinResp.Body)  // drain for connection reuse
	_ = joinResp.Body.Close() // read side
	if joinResp.StatusCode/100 != 2 {
		return fmt.Errorf("leaf: chat join returned %d", joinResp.StatusCode)
	}

	postData, err := json.Marshal(protocol.ChatPostRequest{Room: "lobby", Text: text})
	if err != nil {
		return fmt.Errorf("leaf: marshal chat post: %w", err)
	}
	postPath := fmt.Sprintf("/v3net/v1/%s/chat/rooms/post", l.cfg.Network)
	postResp, err := l.signedPostWithResponse(ctx, postPath, postData)
	if err != nil {
		return fmt.Errorf("leaf: chat post: %w", err)
	}
	drainBody(postResp.Body)  // drain for connection reuse
	_ = postResp.Body.Close() // read side
	if postResp.StatusCode/100 != 2 {
		return fmt.Errorf("leaf: chat post returned %d", postResp.StatusCode)
	}
	return nil
}

// SendLogon notifies the hub of a user logon.
func (l *Leaf) SendLogon(handle string) error {
	return l.sendPresenceCtx(context.Background(), protocol.EventLogon, handle)
}

// SendLogonCtx notifies the hub of a user logon with context.
func (l *Leaf) SendLogonCtx(ctx context.Context, handle string) error {
	return l.sendPresenceCtx(ctx, protocol.EventLogon, handle)
}

// SendLogoff notifies the hub of a user logoff.
func (l *Leaf) SendLogoff(handle string) error {
	return l.sendPresenceCtx(context.Background(), protocol.EventLogoff, handle)
}

// SendLogoffCtx notifies the hub of a user logoff with context.
func (l *Leaf) SendLogoffCtx(ctx context.Context, handle string) error {
	return l.sendPresenceCtx(ctx, protocol.EventLogoff, handle)
}

func (l *Leaf) sendPresenceCtx(ctx context.Context, eventType, handle string) error {
	data, err := json.Marshal(protocol.PresenceRequest{Type: eventType, Handle: handle})
	if err != nil {
		return fmt.Errorf("leaf: marshal presence: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/presence", l.cfg.Network)
	return l.signedPostCtx(ctx, path, data)
}

func (l *Leaf) signedPostCtx(ctx context.Context, path string, body []byte) error {
	url := l.cfg.HubURL + path
	bodyHash := sha256.Sum256(body)
	bodySHA := hex.EncodeToString(bodyHash[:])
	dateStr := time.Now().UTC().Format(http.TimeFormat)

	sig, err := l.cfg.Keystore.Sign("POST", path, dateStr, bodySHA)
	if err != nil {
		return fmt.Errorf("leaf: sign request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
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
	defer func() { _ = resp.Body.Close() }() // read side
	drainBody(resp.Body)                     // drain for connection reuse

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("leaf: POST %s returned %d", path, resp.StatusCode)
	}
	return nil
}

// signedPostWithResponse is like signedPost but returns the raw response
// so the caller can parse the body. The caller must close resp.Body.
func (l *Leaf) signedPostWithResponse(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := l.cfg.HubURL + path
	bodyHash := sha256.Sum256(body)
	bodySHA := hex.EncodeToString(bodyHash[:])
	dateStr := time.Now().UTC().Format(http.TimeFormat)

	sig, err := l.cfg.Keystore.Sign("POST", path, dateStr, bodySHA)
	if err != nil {
		return nil, fmt.Errorf("leaf: sign request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("leaf: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateStr)
	req.Header.Set("X-V3Net-Node-ID", l.cfg.Keystore.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)

	return l.client.Do(req)
}

// Response-body size caps. Hub responses are JSON: subscribe/join/info
// payloads are small, and a message page is at most 100 messages of ≤32KB
// body each (~4MB). Reading is capped so a misbehaving or hostile hub
// cannot exhaust leaf memory.
const (
	// maxRespBytes bounds small JSON responses (info, NAL, subscribe,
	// chat join) and diagnostic error bodies.
	maxRespBytes = 1 << 20 // 1MB
	// maxPollRespBytes bounds a message-page response.
	maxPollRespBytes = 8 << 20 // 8MB
)

// drainBody discards a response body (capped at maxRespBytes) so the
// underlying connection can be reused without letting a slow or oversized
// hub response stall the request until the client timeout.
func drainBody(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxRespBytes))
}

// readBody reads an HTTP response body capped at limit bytes, erroring if
// the body exceeds the cap (never parse truncated data as if complete).
// Errors carry no "leaf:" prefix — call sites add their own context.
func readBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds %d byte limit", limit)
	}
	return data, nil
}

// get performs an unauthenticated GET and returns the response body.
func (l *Leaf) get(path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, l.cfg.HubURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("leaf: create GET request: %w", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("leaf: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	data, err := readBody(resp.Body, maxRespBytes)
	if err != nil {
		return nil, fmt.Errorf("leaf: GET %s: %w", path, err)
	}
	return data, nil
}

func (l *Leaf) signedGetCtx(ctx context.Context, path string) (*http.Response, error) {
	return l.signedGetWith(ctx, path, l.client)
}

func (l *Leaf) signedGetSSE(ctx context.Context, path string) (*http.Response, error) {
	return l.signedGetWith(ctx, path, l.sseClient)
}

func (l *Leaf) signedGetWith(ctx context.Context, path string, c *http.Client) (*http.Response, error) {
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

	return c.Do(req)
}
