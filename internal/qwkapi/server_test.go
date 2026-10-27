package qwkapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
)

// --- fakes ---

type fakeSvc struct {
	packet    []byte
	msgCount  int
	imp       *qwkservice.ImportResult
	impErr    error
	committed bool
}

func (f *fakeSvc) BuildPacket(o qwkservice.ExportOptions) (*qwkservice.ExportResult, error) {
	return &qwkservice.ExportResult{Packet: f.packet, MessageCount: f.msgCount}, nil
}
func (f *fakeSvc) CommitExport(h string, r *qwkservice.ExportResult) { f.committed = true }
func (f *fakeSvc) ImportREP(d []byte, o qwkservice.ImportOptions) (*qwkservice.ImportResult, error) {
	return f.imp, f.impErr
}

// testServer serves felonius/pw, or no accounts at all when authOK is false.
func testServer(t *testing.T, svc PacketService, authOK bool) http.Handler {
	t.Helper()
	accounts := newAccountStore()
	if authOK {
		accounts = newAccountStore(testAccount())
	}
	return newTestServer(t, testDeps(t, accounts, svc))
}

func clientReq(method, path string, body []byte) *http.Request {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("X-V3-Client", "vision3-mobile")
	return r
}

func TestAPI_LoginBadCreds(t *testing.T) {
	h := testServer(t, &fakeSvc{}, false)
	r := clientReq("POST", "/api/qwk/login", []byte(`{"handle":"x","password":"y"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAPI_MissingClientHeader404(t *testing.T) {
	h := testServer(t, &fakeSvc{}, true)
	r := httptest.NewRequest("POST", "/api/qwk/login", nil) // no X-V3-Client
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestAPI_PacketRequiresAuth(t *testing.T) {
	h := testServer(t, &fakeSvc{}, true)
	r := clientReq("GET", "/api/qwk/packet", nil) // no bearer
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAPI_PacketDownloadAndEmpty(t *testing.T) {
	svc := &fakeSvc{packet: []byte("PK\x03\x04zip"), msgCount: 2}
	h := testServer(t, svc, true)
	tok := login(t, h)

	r := clientReq("GET", "/api/qwk/packet", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/zip" {
		t.Errorf("content-type = %q", rr.Header().Get("Content-Type"))
	}
	if rr.Header().Get("X-QWK-Messages") != "2" {
		t.Errorf("X-QWK-Messages = %q, want 2", rr.Header().Get("X-QWK-Messages"))
	}
	if got, _ := io.ReadAll(rr.Body); !bytes.Equal(got, svc.packet) {
		t.Error("body != packet")
	}
	if !svc.committed {
		t.Error("lastread not committed after successful download")
	}

	// Empty export -> 204.
	empty := &fakeSvc{msgCount: 0}
	h2 := testServer(t, empty, true)
	tok2 := login(t, h2)
	r2 := clientReq("GET", "/api/qwk/packet", nil)
	r2.Header.Set("Authorization", "Bearer "+tok2)
	rr2 := httptest.NewRecorder()
	h2.ServeHTTP(rr2, r2)
	if rr2.Code != http.StatusNoContent {
		t.Errorf("empty export status = %d, want 204", rr2.Code)
	}
}

func TestAPI_ReplyResultAndWrongBBS(t *testing.T) {
	svc := &fakeSvc{imp: &qwkservice.ImportResult{Posted: 3, Skipped: 1, Duplicate: 0}}
	h := testServer(t, svc, true)
	tok := login(t, h)
	r := clientReq("POST", "/api/qwk/reply", []byte("PK\x03\x04rep"))
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("Content-Type", "application/zip")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp replyResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Posted != 3 || resp.Skipped != 1 {
		t.Errorf("resp = %+v", resp)
	}

	// ErrWrongBBS -> 200 with wrongBBS=true.
	svc2 := &fakeSvc{impErr: qwkservice.ErrWrongBBS}
	h2 := testServer(t, svc2, true)
	tok2 := login(t, h2)
	r2 := clientReq("POST", "/api/qwk/reply", []byte("x"))
	r2.Header.Set("Authorization", "Bearer "+tok2)
	rr2 := httptest.NewRecorder()
	h2.ServeHTTP(rr2, r2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("wrongBBS status = %d, want 200", rr2.Code)
	}
	var resp2 replyResponse
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp2)
	if !resp2.WrongBBS {
		t.Error("wrongBBS not set")
	}
}
