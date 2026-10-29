package hub

import (
	"bytes"
	"log/slog"
	"mime"
	"net/http"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// signResponses signs every response to a signed request with the hub key,
// in X-V3Net-Hub-Signature over the request's signature and nonce, the
// status and the body (protocol.ResponseSignedPayload), so a leaf can check it against the
// hub key it pinned. The body is buffered to hash it before anything is
// sent. An event stream is the exception: it is signed as it starts, with an
// empty body, and ServeSSE signs each event on its own.
//
// A request without a signature header is passed through unsigned: there is
// nothing to bind the response to, and authMiddleware refuses it anyway.
func (h *Hub) signResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqSig := r.Header.Get(headerSignature)
		if reqSig == "" || h.cfg.Keystore == nil {
			next.ServeHTTP(w, r)
			return
		}
		nonce := protocol.RequestNonce(r.Header.Get(protocol.HeaderNonce))
		sw := &signingWriter{w: w, signer: h.cfg.Keystore, reqSig: reqSig, nonce: nonce}
		next.ServeHTTP(sw, r)
		sw.finish()
	})
}

// signingWriter buffers a response so it can be signed once complete, or,
// for a text/event-stream response, signs the status as the stream starts
// and then passes writes and flushes straight through.
type signingWriter struct {
	w      http.ResponseWriter
	signer protocol.HubSigner
	reqSig string
	nonce  string

	status      int
	wroteHeader bool
	stream      bool
	buf         bytes.Buffer
}

func (sw *signingWriter) Header() http.Header { return sw.w.Header() }

func (sw *signingWriter) WriteHeader(status int) {
	if sw.wroteHeader {
		return
	}
	sw.wroteHeader = true
	sw.status = status
	if status == http.StatusOK && isEventStream(sw.w.Header()) {
		sw.stream = true
		sw.sign(nil)
		sw.w.WriteHeader(status)
	}
}

func (sw *signingWriter) Write(p []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	if sw.stream {
		return sw.w.Write(p)
	}
	return sw.buf.Write(p)
}

// Flush passes through for an event stream. A buffered response is sent in
// one piece by finish, so flushing it early would send it unsigned.
func (sw *signingWriter) Flush() {
	if !sw.stream {
		return
	}
	if f, ok := sw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// finish signs and sends a buffered response. A handler that wrote nothing
// sends an empty 200, as net/http would.
func (sw *signingWriter) finish() {
	if sw.stream {
		return
	}
	if !sw.wroteHeader {
		sw.status = http.StatusOK
	}
	body := sw.buf.Bytes()
	sw.sign(body)
	sw.w.WriteHeader(sw.status)
	// Best-effort: a dead connection is the client's problem.
	_, _ = sw.w.Write(body)
}

// sign sets X-V3Net-Hub-Signature for the response status and body. If the
// keystore cannot sign, the response goes out unsigned and the leaf treats
// it as from an older hub.
func (sw *signingWriter) sign(body []byte) {
	sig, err := protocol.SignResponse(sw.signer, sw.reqSig, sw.nonce, sw.status, body)
	if err != nil {
		slog.Error("hub: sign response", "error", err)
		return
	}
	sw.w.Header().Set(protocol.HeaderHubSignature, sig)
}

// isEventStream reports whether h declares a text/event-stream body.
func isEventStream(h http.Header) bool {
	mt, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	return err == nil && mt == "text/event-stream"
}
