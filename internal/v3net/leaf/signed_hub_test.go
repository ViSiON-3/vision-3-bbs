package leaf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// signedHub serves h as a hub that signs its replies with a key of its own,
// which it serves on /info unless h answers that itself, and which
// setupLeaf pins for a leaf pointed at it. A text/event-stream reply is
// signed on its headers, and each event h writes is given the next id and a
// signature, as the hub does; any other reply is buffered and signed over
// its status and body before it is sent. Tests that stand in for a hub with
// a handler of their own use this in place of httptest.NewServer.
func signedHub(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	hubKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "signing-hub.key"))
	if err != nil {
		t.Fatalf("load hub keystore: %v", err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		network := "testnet"
		if parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v3net/v1/"), "/"); len(parts) > 1 {
			network = parts[0]
		}
		sw := &signingWriter{
			ResponseWriter: w, ks: hubKS, network: network,
			req: reqBinding{sig: r.Header.Get("X-V3Net-Signature"), nonce: r.Header.Get(protocol.HeaderNonce)},
		}
		h.ServeHTTP(sw, r)
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info") && !sw.stream && sw.status == http.StatusNotFound {
			// h does not serve /info; serve the hub's key, unsigned as the
			// public endpoint is.
			w.Header().Del(protocol.HeaderHubSignature)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: network, HubNodeID: hubKS.NodeID(), HubPubKeyB64: hubKS.PubKeyBase64()})
			return
		}
		sw.finish()
	}))
	t.Cleanup(ts.Close)
	registerHubKey(t, ts, hubKS.PubKeyBase64())
	return ts
}

// signingWriter is the http.ResponseWriter signedHub hands its handler.
type signingWriter struct {
	http.ResponseWriter
	ks      *keystore.Keystore
	network string
	req     reqBinding
	status  int
	stream  bool
	buf     bytes.Buffer
	seq     uint64
}

func (s *signingWriter) WriteHeader(code int) {
	if s.status != 0 {
		return
	}
	s.status = code
	if strings.HasPrefix(s.Header().Get("Content-Type"), "text/event-stream") {
		s.stream = true
		sig, _ := protocol.SignResponse(s.ks, s.req.sig, s.req.nonce, code, nil)
		s.Header().Set(protocol.HeaderHubSignature, sig)
		s.ResponseWriter.WriteHeader(code)
	}
}

func (s *signingWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.WriteHeader(http.StatusOK)
	}
	s.buf.Write(b)
	if s.stream {
		s.writeEvents()
	}
	return len(b), nil
}

// writeEvents signs and sends each complete event in the buffer. A block
// with no event type or data is not an event to the leaf, so it goes as it
// is and takes no sequence number.
func (s *signingWriter) writeEvents() {
	for {
		text := s.buf.String()
		i := strings.Index(text, "\n\n")
		if i < 0 {
			return
		}
		block := text[:i]
		s.buf.Next(i + 2)
		var typ string
		var data []string
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				typ = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = append(data, strings.TrimPrefix(line, "data: "))
			}
		}
		if typ == "" || data == nil {
			_, _ = fmt.Fprint(s.ResponseWriter, block+"\n\n")
			continue
		}
		s.seq++
		sig, _ := protocol.SignEvent(s.ks, s.network, s.req.sig, s.req.nonce, s.seq, typ, []byte(strings.Join(data, "\n")))
		_, _ = fmt.Fprintf(s.ResponseWriter, "event: %s\nid: %d\nsig: %s\n", typ, s.seq, sig)
		for _, d := range data {
			_, _ = fmt.Fprintf(s.ResponseWriter, "data: %s\n", d)
		}
		_, _ = fmt.Fprint(s.ResponseWriter, "\n")
	}
}

func (s *signingWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// finish sends a buffered reply, signed.
func (s *signingWriter) finish() {
	if s.stream {
		return
	}
	if s.status == 0 {
		s.status = http.StatusOK
	}
	body := s.buf.Bytes()
	sig, _ := protocol.SignResponse(s.ks, s.req.sig, s.req.nonce, s.status, body)
	s.Header().Set(protocol.HeaderHubSignature, sig)
	s.ResponseWriter.WriteHeader(s.status)
	_, _ = s.ResponseWriter.Write(body)
}
