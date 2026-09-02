package qwkapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
)

// captureLogs installs a DEBUG-level JSON logger as slog.Default for the test
// and returns a func decoding everything logged so far into records.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []map[string]any {
		var recs []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("log line not JSON: %q: %v", line, err)
			}
			recs = append(recs, m)
		}
		return recs
	}
}

// findRec returns the first record whose fields all match want.
func findRec(recs []map[string]any, want map[string]any) map[string]any {
	for _, r := range recs {
		match := true
		for k, v := range want {
			if r[k] != v {
				match = false
				break
			}
		}
		if match {
			return r
		}
	}
	return nil
}

func requireRec(t *testing.T, recs []map[string]any, want map[string]any) map[string]any {
	t.Helper()
	got := findRec(recs, want)
	if got == nil {
		t.Fatalf("no log record matching %v; got %v", want, recs)
	}
	return got
}

func TestLog_RateLimitedLogin(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{}, false)

	var last int
	for i := 0; i < 6; i++ { // limiter allows 5 per minute per IP
		r := clientReq("POST", "/api/qwk/login", []byte(`{"handle":"x","password":"y"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		last = rr.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("6th login status = %d, want 429", last)
	}
	rec := requireRec(t, logs(), map[string]any{"msg": "qwk api login", "outcome": "rateLimited"})
	if rec["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", rec["level"])
	}
	if rec["remote"] == nil || rec["remote"] == "" {
		t.Error("rate-limited login logged without a remote address")
	}
}

func TestLog_AuthRejections(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{}, true)

	noTok := clientReq("GET", "/api/qwk/packet", nil)
	h.ServeHTTP(httptest.NewRecorder(), noTok)

	badTok := clientReq("GET", "/api/qwk/packet", nil)
	badTok.Header.Set("Authorization", "Bearer deadbeef")
	h.ServeHTTP(httptest.NewRecorder(), badTok)

	recs := logs()
	requireRec(t, recs, map[string]any{"msg": "qwk api auth", "outcome": "noToken", "path": "/api/qwk/packet"})
	requireRec(t, recs, map[string]any{"msg": "qwk api auth", "outcome": "badToken", "path": "/api/qwk/packet"})
}

func TestLog_ProbeRejections(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{}, true)

	// httptest gives every request RemoteAddr 192.0.2.1:1234, so the logged
	// remote must be the host half of that.
	const remote = "192.0.2.1"

	cases := []struct {
		name   string
		reason string
		method string
		path   string
		agent  string
		req    func(agent string) *http.Request
	}{
		{
			name: "no client header", reason: "noClientHeader",
			method: "POST", path: "/api/qwk/login", agent: "curl/8.5.0",
			req: func(agent string) *http.Request {
				r := httptest.NewRequest("POST", "/api/qwk/login", nil) // no X-V3-Client
				r.Header.Set("User-Agent", agent)
				return r
			},
		},
		{
			name: "browser fetch", reason: "browser",
			method: "GET", path: "/api/qwk/packet", agent: "Mozilla/5.0",
			req: func(agent string) *http.Request {
				r := clientReq("GET", "/api/qwk/packet", nil)
				r.Header.Set("Sec-Fetch-Mode", "navigate")
				r.Header.Set("User-Agent", agent)
				return r
			},
		},
		{
			name: "html accept", reason: "htmlAccept",
			method: "GET", path: "/api/qwk/packet", agent: "Mozilla/5.0",
			req: func(agent string) *http.Request {
				r := clientReq("GET", "/api/qwk/packet", nil)
				r.Header.Set("Accept", "text/html,application/xhtml+xml")
				r.Header.Set("User-Agent", agent)
				return r
			},
		},
		{
			name: "unknown path", reason: "unknownPath",
			method: "GET", path: "/wp-login.php", agent: "zgrab/0.x",
			req: func(agent string) *http.Request {
				r := clientReq("GET", "/wp-login.php", nil)
				r.Header.Set("User-Agent", agent)
				return r
			},
		},
		{
			name: "wrong method", reason: "badMethod",
			method: "GET", path: "/api/qwk/login", agent: "vision3-mobile/1.0",
			req: func(agent string) *http.Request {
				r := clientReq("GET", "/api/qwk/login", nil) // login is POST-only
				r.Header.Set("User-Agent", agent)
				return r
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, tc.req(tc.agent))

			// Every field is matched exactly, so a record carrying the right
			// keys with wrong or empty values still fails.
			rec := requireRec(t, logs(), map[string]any{
				"msg":    "qwk api rejected",
				"reason": tc.reason,
				"remote": remote,
				"method": tc.method,
				"path":   tc.path,
				"agent":  tc.agent,
			})
			if rec["level"] != "DEBUG" {
				t.Errorf("level = %v, want DEBUG", rec["level"])
			}
		})
	}
}

func TestLog_PathRedirect(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{}, true)

	// ServeMux answers unclean paths with a redirect before any registered
	// handler runs, so these bypass even the "/" catch-all.
	for _, path := range []string{"/api//qwk/login", "/scan/../..//wp-admin"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, clientReq("GET", path, nil))
		if rr.Code < 300 || rr.Code > 399 {
			t.Fatalf("%s status = %d, want a 3xx redirect from the mux", path, rr.Code)
		}
		rec := requireRec(t, logs(), map[string]any{
			"msg": "qwk api rejected", "reason": "pathRedirect",
			"path": path, "method": "GET", "remote": "192.0.2.1",
		})
		if rec["level"] != "DEBUG" {
			t.Errorf("%s level = %v, want DEBUG", path, rec["level"])
		}
	}
}

func TestLog_LoginBadJSON(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{}, true)

	r := clientReq("POST", "/api/qwk/login", []byte(`{not json`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	rec := requireRec(t, logs(), map[string]any{"msg": "qwk api login", "outcome": "badRequest"})
	if _, ok := rec["handle"]; ok {
		t.Error("malformed login body must not log a handle field")
	}
}

func TestLog_ReplyTooLarge(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{}, true)
	tok := login(t, h)

	r := clientReq("POST", "/api/qwk/reply", bytes.Repeat([]byte("x"), maxREPBytes+1))
	r.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
	rec := requireRec(t, logs(), map[string]any{"msg": "qwk api reply", "outcome": "tooLarge", "handle": "felonius"})
	if rec["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", rec["level"])
	}
}

func TestLog_ReplyRateLimited(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, &fakeSvc{imp: &qwkservice.ImportResult{}}, true)
	tok := login(t, h)

	var last int
	for i := 0; i < 31; i++ { // packet limiter allows 30 per minute per handle
		r := clientReq("POST", "/api/qwk/reply", []byte("x"))
		r.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		last = rr.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("31st reply status = %d, want 429", last)
	}
	requireRec(t, logs(), map[string]any{"msg": "qwk api reply", "outcome": "rateLimited", "handle": "felonius"})
}

func TestLog_ServerErrorWriter(t *testing.T) {
	logs := captureLogs(t)

	line := "http: TLS handshake error from 1.2.3.4:5678: remote error: tls: bad certificate\r\n"
	n, err := serverErrorWriter{}.Write([]byte(line))
	if err != nil || n != len(line) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(line))
	}
	rec := requireRec(t, logs(), map[string]any{"msg": "qwk api server"})
	if rec["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", rec["level"])
	}
	// Full equality, so a handler that dropped the detail after the prefix
	// would fail rather than pass on a substring match.
	if got, _ := rec["error"].(string); got != strings.TrimRight(line, "\r\n") {
		t.Errorf("error = %q, want %q", got, strings.TrimRight(line, "\r\n"))
	}
}

// nilSvc returns (nil, nil) from both service calls — the shape that used to
// panic in the handlers.
type nilSvc struct{}

func (nilSvc) BuildPacket(qwkservice.ExportOptions) (*qwkservice.ExportResult, error) {
	return nil, nil
}
func (nilSvc) CommitExport(string, *qwkservice.ExportResult) {}
func (nilSvc) ImportREP([]byte, qwkservice.ImportOptions) (*qwkservice.ImportResult, error) {
	return nil, nil
}

func TestLog_NilServiceResult(t *testing.T) {
	logs := captureLogs(t)
	h := testServer(t, nilSvc{}, true)
	tok := login(t, h)

	pkt := clientReq("GET", "/api/qwk/packet", nil)
	pkt.Header.Set("Authorization", "Bearer "+tok)
	rrPkt := httptest.NewRecorder()
	h.ServeHTTP(rrPkt, pkt)
	if rrPkt.Code != http.StatusInternalServerError {
		t.Errorf("packet status = %d, want 500", rrPkt.Code)
	}

	rep := clientReq("POST", "/api/qwk/reply", []byte("x"))
	rep.Header.Set("Authorization", "Bearer "+tok)
	rrRep := httptest.NewRecorder()
	h.ServeHTTP(rrRep, rep)
	if rrRep.Code != http.StatusInternalServerError {
		t.Errorf("reply status = %d, want 500", rrRep.Code)
	}

	recs := logs()
	for _, msg := range []string{"qwk api build packet", "qwk api import rep"} {
		rec := requireRec(t, recs, map[string]any{"msg": msg})
		if rec["level"] != "ERROR" {
			t.Errorf("%s level = %v, want ERROR", msg, rec["level"])
		}
		if got, _ := rec["error"].(string); !strings.Contains(got, "no result and no error") {
			t.Errorf("%s error = %q, want the nil-result reason", msg, got)
		}
	}
}
