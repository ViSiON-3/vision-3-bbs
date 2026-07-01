# QWK Phase 7 — Packet Transport API — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A config-gated, TLS, token-authenticated HTTP API (`login` → download `.QWK` → upload `.REP`) that reuses the shared `qwkservice`, enabling a future mobile client without terminal automation.

**Architecture:** A new `internal/qwkapi` package holds an `http.Server` shell — self-signed TLS bootstrap, bearer-token auth over `UserMgr.Authenticate`, a casual/browser filter, an in-memory rate limiter, and three packet-only handlers that call the existing `*qwkservice.Service`. It is wired into `cmd/vision3/main.go` beside the SSH/telnet servers and is disabled by default.

**Tech Stack:** Go stdlib only — `net/http`, `crypto/tls`, `crypto/ecdsa`, `crypto/x509`, `crypto/rand`, `encoding/pem`, `encoding/json`, `net/http/httptest`, `testing`. No web framework, no new dependency.

## Global Constraints

- No new dependencies; Go files under 300 lines; `slog` for structured logging; stdlib `testing` (no testify); Go doc comments on exported symbols.
- Spec: `docs/superpowers/specs/2026-07-01-qwk-phase7-packet-api-design.md`. Every task's requirements implicitly include it.
- **Disabled by default.** With `qwkAPI.enabled=false`, no listener starts.
- **Auth:** `POST /login` runs `UserMgr.Authenticate`; success issues a 256-bit opaque bearer token (in-memory, TTL default 24h). Later requests send `Authorization: Bearer <token>`. The token entry stores the login-time `*user.User` snapshot.
- **TLS:** app terminates TLS. If `certFile`+`keyFile` set, load them; else generate-or-load a self-signed ECDSA P-256 cert at `configs/qwkapi_cert.pem` / `configs/qwkapi_key.pem` (key mode `0600`) and log its SHA-256 fingerprint.
- **Filter (not security):** every route requires header `X-V3-Client` (else 404); reject browser-signature requests (`Sec-Fetch-Mode`/`Sec-Fetch-Site` present, or `Accept` preferring `text/html` → 404).
- **Transport:** binary packet bodies (`application/zip`); JSON only for metadata/errors `{"error":"<code>","message":"<text>"}`. Upload cap 16 MiB.
- **Rate limits:** login 5/min per client IP; packet+reply 30/min per user handle; over → 429 + `Retry-After`.
- **Endpoints:** `POST /api/qwk/login`, `GET /api/qwk/packet`, `POST /api/qwk/reply`. Packet-only — no browsing, no compose.
- **Packet semantics must match the terminal path:** `BuildPacket(ExportOptions{Handle, TaggedTags})`, commit lastread via `CommitExport` **only after** the bytes are produced; `ImportREP(data, ImportOptions{Handle, Signature, Authorize})` with the ACS write gate; `ErrWrongBBS` is a normal 200 outcome, not a 500.

---

## Reference: existing signatures (from the codebase)

```go
// internal/qwkservice
func New(store MessageStore, bbsID, bbsName, sysOpName, dataPath string) *Service
func (s *Service) BuildPacket(opts ExportOptions) (*ExportResult, error)
func (s *Service) CommitExport(handle string, res *ExportResult)
func (s *Service) ImportREP(data []byte, opts ImportOptions) (*ImportResult, error)
type ExportOptions struct { Handle string; TaggedTags []string; MaxPerArea int }
type ExportResult  struct { BBSID string; Packet []byte; MessageCount int; LastRead []LastReadUpdate }
type ImportOptions struct { Handle string; Signature string; Authorize func(*message.MessageArea) bool; Notify func(*message.MessageArea) }
type ImportResult  struct { Posted int; Skipped int; Duplicate int }
var ErrWrongBBS error

// internal/user
func (um *UserMgr) Authenticate(handle, password string) (*user.User, bool)
type User struct { Handle string; AutoSignature string; TaggedMessageAreaTags []string; AccessLevel int; /* ... */ }

// internal/menu (unexported today)
func checkACS(acsString string, u *user.User, s ssh.Session, terminal *term.Terminal, startTime time.Time) bool
// checkACS is nil-safe for s/terminal: session-only conditions (L, A) evaluate false; S/F/SYSOP work headless.

// internal/config
type ServerConfig struct { /* ... */ }  // add QWKAPI field
// V3NetHubConfig is the {Enabled,Host,Port}+ListenAddr() pattern to mirror.
```

---

## File Structure

- Create: `internal/qwkapi/types.go` — `Deps`, `Authenticator` interface, JSON request/response shapes, error helpers.
- Create: `internal/qwkapi/tlscert.go` — self-signed cert generate-or-load + fingerprint.
- Create: `internal/qwkapi/auth.go` — token store, `IssueToken`/`Resolve`, bearer middleware.
- Create: `internal/qwkapi/filter.go` — `X-V3-Client` + browser-signature middleware.
- Create: `internal/qwkapi/ratelimit.go` — in-memory fixed-window limiter.
- Create: `internal/qwkapi/server.go` — `Server`, route table, middleware chain, `Start`/`Shutdown`.
- Create: `internal/qwkapi/handlers.go` — `login`, `packet`, `reply` handlers.
- Create: `internal/qwkapi/*_test.go` — per-unit tests + full-chain httptest.
- Modify: `internal/config/config.go` — add `QWKAPIConfig` + field on `ServerConfig` + defaults.
- Modify: `templates/configs/config.json` (and any default writer) — add the `qwkAPI` block.
- Create: `internal/menu/qwk_authorizer.go` — exported headless `QWKWriteAuthorizer`.
- Modify: `cmd/vision3/main.go` — start/stop the API when enabled.
- Create: `docs/sysop/messages/qwk-api.md` — sysop documentation (experimental warning).
- Modify: `docs/sysop/messages/qwk.md` — one cross-link to the API doc.

---

## Task 1: Config — `QWKAPIConfig`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `templates/configs/config.json`
- Test: `internal/config/config_test.go` (add cases; create if absent)

**Interfaces:**
- Produces: `config.QWKAPIConfig{Enabled bool; Host string; Port int; CertFile string; KeyFile string; TokenTTLHours int}` with `func (c *QWKAPIConfig) ListenAddr() string` and `func (c *QWKAPIConfig) TokenTTL() time.Duration`; field `ServerConfig.QWKAPI QWKAPIConfig json:"qwkAPI"`.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestQWKAPIConfig_Defaults(t *testing.T) {
	var c QWKAPIConfig
	if got := c.ListenAddr(); got != ":8666" {
		t.Errorf("ListenAddr default = %q, want :8666", got)
	}
	c.Host, c.Port = "127.0.0.1", 9000
	if got := c.ListenAddr(); got != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:9000", got)
	}
	if got := (&QWKAPIConfig{}).TokenTTL(); got != 24*time.Hour {
		t.Errorf("TokenTTL default = %v, want 24h", got)
	}
	if got := (&QWKAPIConfig{TokenTTLHours: 2}).TokenTTL(); got != 2*time.Hour {
		t.Errorf("TokenTTL = %v, want 2h", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run TestQWKAPIConfig_Defaults -v`
Expected: FAIL — `QWKAPIConfig` undefined.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add the type (near `V3NetHubConfig`) and a field on `ServerConfig`:

```go
// QWKAPIConfig configures the optional QWK packet transport API (Phase 7).
// Disabled by default. See docs/sysop/messages/qwk-api.md.
type QWKAPIConfig struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`          // blank = all interfaces
	Port          int    `json:"port"`          // default 8666
	CertFile      string `json:"certFile"`      // blank = auto self-signed
	KeyFile       string `json:"keyFile"`       // blank = auto self-signed
	TokenTTLHours int    `json:"tokenTTLHours"` // default 24
}

// ListenAddr returns host:port for http.Server, defaulting the port to 8666.
func (c *QWKAPIConfig) ListenAddr() string {
	port := c.Port
	if port == 0 {
		port = 8666
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

// TokenTTL returns the bearer-token lifetime, defaulting to 24h.
func (c *QWKAPIConfig) TokenTTL() time.Duration {
	if c.TokenTTLHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.TokenTTLHours) * time.Hour
}
```

Add the field to `ServerConfig` (alongside `SSHEnabled` etc.):

```go
	QWKAPI QWKAPIConfig `json:"qwkAPI"`
```

Ensure `time` is imported (it is used elsewhere in the file; confirm).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/ -run TestQWKAPIConfig_Defaults -v`
Expected: PASS.

- [ ] **Step 5: Add the template default**

In `templates/configs/config.json`, add to the server config object:

```json
"qwkAPI": { "enabled": false, "host": "0.0.0.0", "port": 8666, "certFile": "", "keyFile": "", "tokenTTLHours": 24 }
```

Verify it parses: `go test ./internal/config/ 2>&1 | tail -3` (all config tests pass).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/config/config.go
go vet ./internal/config/
git add internal/config/config.go internal/config/config_test.go templates/configs/config.json
git commit -m "feat(qwkapi): add QWKAPIConfig (disabled by default)"
```

---

## Task 2: TLS cert bootstrap — `tlscert.go`

**Files:**
- Create: `internal/qwkapi/tlscert.go`
- Test: `internal/qwkapi/tlscert_test.go`

**Interfaces:**
- Produces: `func loadOrCreateCert(cfg config.QWKAPIConfig, dir string) (tls.Certificate, string, error)` — returns the cert, its SHA-256 fingerprint (hex, colon-separated), and any error. `dir` is where auto certs live.

- [ ] **Step 1: Write the failing test**

```go
package qwkapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestLoadOrCreateCert_GeneratesAndReloads(t *testing.T) {
	dir := t.TempDir()
	cfg := config.QWKAPIConfig{Host: "127.0.0.1"}

	cert1, fp1, err := loadOrCreateCert(cfg, dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(cert1.Certificate) == 0 || fp1 == "" {
		t.Fatal("expected a generated cert and fingerprint")
	}
	// Files persisted with a locked-down key.
	keyInfo, err := os.Stat(filepath.Join(dir, "qwkapi_key.pem"))
	if err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Errorf("key mode = %o, want 600", keyInfo.Mode().Perm())
	}

	// Second call loads the same cert (same fingerprint).
	_, fp2, err := loadOrCreateCert(cfg, dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprint changed on reload: %s != %s", fp1, fp2)
	}
}

func TestLoadOrCreateCert_ExplicitFiles(t *testing.T) {
	dir := t.TempDir()
	// Generate a pair via the auto path, then point cfg at those files.
	if _, _, err := loadOrCreateCert(config.QWKAPIConfig{Host: "127.0.0.1"}, dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := config.QWKAPIConfig{
		CertFile: filepath.Join(dir, "qwkapi_cert.pem"),
		KeyFile:  filepath.Join(dir, "qwkapi_key.pem"),
	}
	if _, fp, err := loadOrCreateCert(cfg, t.TempDir()); err != nil || fp == "" {
		t.Fatalf("explicit files: fp=%q err=%v", fp, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/qwkapi/ -run TestLoadOrCreateCert -v`
Expected: FAIL — package/function undefined.

- [ ] **Step 3: Implement `internal/qwkapi/tlscert.go`**

```go
package qwkapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// loadOrCreateCert resolves the API's TLS certificate. If cfg.CertFile and
// cfg.KeyFile are both set they are loaded; otherwise a self-signed ECDSA cert
// is generated at dir/qwkapi_{cert,key}.pem (created once, then reused). It
// returns the certificate and its SHA-256 fingerprint (hex, colon-separated).
func loadOrCreateCert(cfg config.QWKAPIConfig, dir string) (tls.Certificate, string, error) {
	certPath, keyPath := cfg.CertFile, cfg.KeyFile
	if certPath == "" || keyPath == "" {
		certPath = filepath.Join(dir, "qwkapi_cert.pem")
		keyPath = filepath.Join(dir, "qwkapi_key.pem")
		if !fileExists(certPath) || !fileExists(keyPath) {
			if err := generateSelfSigned(cfg.Host, certPath, keyPath); err != nil {
				return tls.Certificate{}, "", fmt.Errorf("generate self-signed cert: %w", err)
			}
		}
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("load cert: %w", err)
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return cert, fingerprintHex(sum[:]), nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func fingerprintHex(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, ":")
}

func generateSelfSigned(host, certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "ViSiON/3 QWK API"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	if host != "" && host != "0.0.0.0" {
		if ip := net.ParseIP(host); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/qwkapi/ -run TestLoadOrCreateCert -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwkapi/tlscert.go internal/qwkapi/tlscert_test.go
go vet ./internal/qwkapi/
git add internal/qwkapi/tlscert.go internal/qwkapi/tlscert_test.go
git commit -m "feat(qwkapi): self-signed TLS cert generate-or-load"
```

---

## Task 3: Token store + bearer middleware — `auth.go`, `types.go`

**Files:**
- Create: `internal/qwkapi/types.go`
- Create: `internal/qwkapi/auth.go`
- Test: `internal/qwkapi/auth_test.go`

**Interfaces:**
- Produces: `type Authenticator interface { Authenticate(handle, password string) (*user.User, bool) }`; `type tokenStore` with `newTokenStore(ttl time.Duration) *tokenStore`, `(*tokenStore) Issue(u *user.User) (string, time.Time)`, `(*tokenStore) Resolve(token string) (*user.User, bool)`, `(*tokenStore) sweep()`. Context key + `userFromContext(ctx) *user.User`.

- [ ] **Step 1: Write the failing test**

```go
package qwkapi

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestTokenStore_IssueResolveExpire(t *testing.T) {
	ts := newTokenStore(50 * time.Millisecond)
	u := &user.User{Handle: "felonius"}

	tok, exp := ts.Issue(u)
	if tok == "" || !exp.After(time.Now()) {
		t.Fatalf("bad issue: tok=%q exp=%v", tok, exp)
	}
	got, ok := ts.Resolve(tok)
	if !ok || got.Handle != "felonius" {
		t.Fatalf("resolve = %v, %v; want felonius,true", got, ok)
	}
	if _, ok := ts.Resolve("nope"); ok {
		t.Error("unknown token resolved")
	}

	// After TTL, the token is rejected.
	ts.expireForTest(tok)
	if _, ok := ts.Resolve(tok); ok {
		t.Error("expired token still resolves")
	}
}
```

(`expireForTest` backdates a token's expiry; include it in `auth.go` guarded as a test helper — or set the entry's `expiresAt` via an unexported method used only by the test in the same package.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/qwkapi/ -run TestTokenStore -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `types.go` then `auth.go`**

`internal/qwkapi/types.go`:

```go
package qwkapi

import (
	"encoding/json"
	"net/http"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Authenticator verifies BBS credentials (satisfied by *user.UserMgr).
type Authenticator interface {
	Authenticate(handle, password string) (*user.User, bool)
}

type loginRequest struct {
	Handle   string `json:"handle"`
	Password string `json:"password"`
}
type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}
type replyResponse struct {
	Posted    int  `json:"posted"`
	Skipped   int  `json:"skipped"`
	Duplicate int  `json:"duplicate"`
	WrongBBS  bool `json:"wrongBBS"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}
```

`internal/qwkapi/auth.go`:

```go
package qwkapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

type tokenEntry struct {
	user      *user.User
	expiresAt time.Time
}

type tokenStore struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]tokenEntry
}

func newTokenStore(ttl time.Duration) *tokenStore {
	return &tokenStore{ttl: ttl, m: make(map[string]tokenEntry)}
}

// Issue creates a random bearer token bound to u, returning it and its expiry.
func (ts *tokenStore) Issue(u *user.User) (string, time.Time) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	tok := hex.EncodeToString(raw)
	exp := time.Now().Add(ts.ttl)
	ts.mu.Lock()
	ts.m[tok] = tokenEntry{user: u, expiresAt: exp}
	ts.mu.Unlock()
	return tok, exp
}

// Resolve returns the user for a live token; ok is false if unknown or expired.
func (ts *tokenStore) Resolve(tok string) (*user.User, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	e, ok := ts.m[tok]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(ts.m, tok)
		return nil, false
	}
	return e.user, true
}

func (ts *tokenStore) sweep() {
	now := time.Now()
	ts.mu.Lock()
	for k, e := range ts.m {
		if now.After(e.expiresAt) {
			delete(ts.m, k)
		}
	}
	ts.mu.Unlock()
}

// expireForTest backdates a token (same-package test helper).
func (ts *tokenStore) expireForTest(tok string) {
	ts.mu.Lock()
	if e, ok := ts.m[tok]; ok {
		e.expiresAt = time.Now().Add(-time.Minute)
		ts.m[tok] = e
	}
	ts.mu.Unlock()
}

type ctxKey int

const userCtxKey ctxKey = 0

func userFromContext(ctx context.Context) *user.User {
	u, _ := ctx.Value(userCtxKey).(*user.User)
	return u
}

// requireBearer wraps next, admitting only requests with a live token.
func (ts *tokenStore) requireBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		tok := strings.TrimPrefix(auth, "Bearer ")
		if tok == auth || tok == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		u, ok := ts.Resolve(tok)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, u)))
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/qwkapi/ -run TestTokenStore -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwkapi/types.go internal/qwkapi/auth.go internal/qwkapi/auth_test.go
go vet ./internal/qwkapi/
git add internal/qwkapi/types.go internal/qwkapi/auth.go internal/qwkapi/auth_test.go
git commit -m "feat(qwkapi): in-memory bearer token store + auth middleware"
```

---

## Task 4: Filter + rate-limit middleware — `filter.go`, `ratelimit.go`

**Files:**
- Create: `internal/qwkapi/filter.go`
- Create: `internal/qwkapi/ratelimit.go`
- Test: `internal/qwkapi/filter_test.go`, `internal/qwkapi/ratelimit_test.go`

**Interfaces:**
- Produces: `func requireClient(next http.HandlerFunc) http.HandlerFunc`; `type limiter` with `newLimiter(max int, window time.Duration) *limiter` and `(*limiter) allow(key string) bool`; `func clientIP(r *http.Request) string`.

- [ ] **Step 1: Write the failing tests**

`internal/qwkapi/filter_test.go`:

```go
package qwkapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRequireClient(t *testing.T) {
	h := requireClient(okHandler)

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"missing header", map[string]string{}, http.StatusNotFound},
		{"present", map[string]string{"X-V3-Client": "vision3-mobile"}, http.StatusOK},
		{"browser sec-fetch", map[string]string{"X-V3-Client": "x", "Sec-Fetch-Mode": "navigate"}, http.StatusNotFound},
		{"browser accept html", map[string]string{"X-V3-Client": "x", "Accept": "text/html,application/xhtml+xml"}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/qwk/packet", nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			rr := httptest.NewRecorder()
			h(rr, req)
			if rr.Code != c.want {
				t.Errorf("status = %d, want %d", rr.Code, c.want)
			}
		})
	}
}
```

`internal/qwkapi/ratelimit_test.go`:

```go
package qwkapi

import (
	"testing"
	"time"
)

func TestLimiter_AllowsThenBlocks(t *testing.T) {
	l := newLimiter(2, time.Minute)
	if !l.allow("a") || !l.allow("a") {
		t.Fatal("first two should be allowed")
	}
	if l.allow("a") {
		t.Error("third should be blocked")
	}
	if !l.allow("b") {
		t.Error("different key should be allowed")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/qwkapi/ -run 'TestRequireClient|TestLimiter' -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/qwkapi/filter.go`:

```go
package qwkapi

import (
	"net"
	"net/http"
	"strings"
)

// requireClient rejects requests that lack the X-V3-Client header or that look
// like they came from a web browser. It returns 404 (not 403) so the endpoint's
// existence is not confirmed. This filters casual/browser traffic; it is NOT an
// authentication control.
func requireClient(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-V3-Client")) == "" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Mode") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// clientIP extracts the remote IP (host part) for rate-limit keying.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

`internal/qwkapi/ratelimit.go`:

```go
package qwkapi

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter: at most max events per window per key.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	start time.Time
	count int
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: make(map[string]*window)}
}

// allow records an event for key and reports whether it is within the limit.
func (l *limiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &window{start: now, count: 1}
		return true
	}
	if w.count >= l.max {
		return false
	}
	w.count++
	return true
}
```

Note: name the struct field type `window` and the local `w` — the package has no other `window`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/qwkapi/ -run 'TestRequireClient|TestLimiter' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwkapi/filter.go internal/qwkapi/ratelimit.go internal/qwkapi/filter_test.go internal/qwkapi/ratelimit_test.go
go vet ./internal/qwkapi/
git add internal/qwkapi/filter.go internal/qwkapi/ratelimit.go internal/qwkapi/filter_test.go internal/qwkapi/ratelimit_test.go
git commit -m "feat(qwkapi): client-header/browser filter + rate limiter"
```

---

## Task 5: Server, handlers, and the full middleware chain — `server.go`, `handlers.go`

**Files:**
- Create: `internal/qwkapi/server.go`
- Create: `internal/qwkapi/handlers.go`
- Test: `internal/qwkapi/server_test.go`

**Interfaces:**
- Consumes: `loadOrCreateCert`, `tokenStore`/`requireBearer`, `requireClient`, `limiter`, `Authenticator`, the JSON types, and `*qwkservice.Service` methods (`BuildPacket`, `CommitExport`, `ImportREP`).
- Produces:
  ```go
  type PacketService interface {
      BuildPacket(opts qwkservice.ExportOptions) (*qwkservice.ExportResult, error)
      CommitExport(handle string, res *qwkservice.ExportResult)
      ImportREP(data []byte, opts qwkservice.ImportOptions) (*qwkservice.ImportResult, error)
  }
  type Deps struct {
      Config       config.QWKAPIConfig
      ConfigDir    string
      Users        Authenticator
      Service      PacketService
      AuthorizeFor func(u *user.User) func(area *message.MessageArea) bool
  }
  func NewServer(deps Deps) (*Server, error)
  func (s *Server) Handler() http.Handler   // for tests
  func (s *Server) Start() error             // ListenAndServeTLS (blocking)
  func (s *Server) Shutdown(ctx context.Context) error
  ```

- [ ] **Step 1: Write the failing test (full chain via httptest)**

```go
package qwkapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// --- fakes ---
type fakeAuth struct{ ok bool }
func (f fakeAuth) Authenticate(h, p string) (*user.User, bool) {
	if f.ok && h == "felonius" && p == "pw" {
		return &user.User{Handle: "felonius"}, true
	}
	return nil, false
}

type fakeSvc struct {
	packet   []byte
	msgCount int
	imp      *qwkservice.ImportResult
	impErr   error
	committed bool
}
func (f *fakeSvc) BuildPacket(o qwkservice.ExportOptions) (*qwkservice.ExportResult, error) {
	return &qwkservice.ExportResult{Packet: f.packet, MessageCount: f.msgCount}, nil
}
func (f *fakeSvc) CommitExport(h string, r *qwkservice.ExportResult) { f.committed = true }
func (f *fakeSvc) ImportREP(d []byte, o qwkservice.ImportOptions) (*qwkservice.ImportResult, error) {
	return f.imp, f.impErr
}

func testServer(t *testing.T, svc PacketService, authOK bool) http.Handler {
	t.Helper()
	s, err := NewServer(Deps{
		Config:    config.QWKAPIConfig{},
		ConfigDir: t.TempDir(),
		Users:     fakeAuth{ok: authOK},
		Service:   svc,
		AuthorizeFor: func(u *user.User) func(*message.MessageArea) bool {
			return func(*message.MessageArea) bool { return true }
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s.Handler()
}

func clientReq(method, path string, body []byte) *http.Request {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("X-V3-Client", "vision3-mobile")
	return r
}

func login(t *testing.T, h http.Handler) string {
	t.Helper()
	r := clientReq("POST", "/api/qwk/login", []byte(`{"handle":"felonius","password":"pw"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("login status = %d", rr.Code)
	}
	var resp loginResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Token == "" {
		t.Fatal("no token")
	}
	return resp.Token
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/qwkapi/ -run TestAPI_ -v`
Expected: FAIL — `NewServer`/`Server`/`PacketService`/`Deps` undefined.

- [ ] **Step 3: Implement `server.go`**

```go
package qwkapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// PacketService is the subset of *qwkservice.Service the API needs.
type PacketService interface {
	BuildPacket(opts qwkservice.ExportOptions) (*qwkservice.ExportResult, error)
	CommitExport(handle string, res *qwkservice.ExportResult)
	ImportREP(data []byte, opts qwkservice.ImportOptions) (*qwkservice.ImportResult, error)
}

// Deps are the collaborators the API server needs.
type Deps struct {
	Config       config.QWKAPIConfig
	ConfigDir    string // where auto TLS certs live
	Users        Authenticator
	Service      PacketService
	AuthorizeFor func(u *user.User) func(area *message.MessageArea) bool
}

// Server is the QWK packet transport API.
type Server struct {
	deps        Deps
	tokens      *tokenStore
	loginLimit  *limiter
	packetLimit *limiter
	cert        tls.Certificate
	fingerprint string
	http        *http.Server
}

const maxREPBytes = 16 << 20 // 16 MiB

// NewServer builds the server and resolves its TLS certificate.
func NewServer(deps Deps) (*Server, error) {
	cert, fp, err := loadOrCreateCert(deps.Config, deps.ConfigDir)
	if err != nil {
		return nil, err
	}
	return &Server{
		deps:        deps,
		tokens:      newTokenStore(deps.Config.TokenTTL()),
		loginLimit:  newLimiter(5, time.Minute),
		packetLimit: newLimiter(30, time.Minute),
		cert:        cert,
		fingerprint: fp,
	}, nil
}

// Fingerprint returns the TLS cert SHA-256 (for logging).
func (s *Server) Fingerprint() string { return s.fingerprint }

// Handler builds the routed, middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qwk/login", requireClient(s.handleLogin))
	mux.HandleFunc("/api/qwk/packet", requireClient(s.tokens.requireBearer(s.handlePacket)))
	mux.HandleFunc("/api/qwk/reply", requireClient(s.tokens.requireBearer(s.handleReply)))
	return mux
}

// Start serves HTTPS until Shutdown; blocking.
func (s *Server) Start() error {
	s.http = &http.Server{
		Addr:      s.deps.Config.ListenAddr(),
		Handler:   s.Handler(),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{s.cert}},
	}
	slog.Info("QWK API listening", "addr", s.deps.Config.ListenAddr(), "fingerprint", s.fingerprint)
	if err := s.http.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("qwk api serve: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}
```

- [ ] **Step 4: Implement `handlers.go`**

```go
package qwkapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	ip := clientIP(r)
	if !s.loginLimit.allow(ip) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	u, ok := s.deps.Users.Authenticate(req.Handle, req.Password)
	if !ok {
		slog.Info("qwk api login", "handle", req.Handle, "remote", ip, "outcome", "fail")
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	tok, exp := s.tokens.Issue(u)
	slog.Info("qwk api login", "handle", u.Handle, "remote", ip, "outcome", "success")
	writeJSON(w, http.StatusOK, loginResponse{Token: tok, ExpiresAt: exp.UTC().Format("2006-01-02T15:04:05Z07:00")})
}

func (s *Server) handlePacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method", "GET required")
		return
	}
	u := userFromContext(r.Context())
	if !s.packetLimit.allow(u.Handle) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	res, err := s.deps.Service.BuildPacket(qwkservice.ExportOptions{
		Handle:     u.Handle,
		TaggedTags: u.TaggedMessageAreaTags,
	})
	if err != nil {
		slog.Error("qwk api build packet", "handle", u.Handle, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "failed to build packet")
		return
	}
	if res.MessageCount == 0 {
		slog.Info("qwk api packet", "handle", u.Handle, "remote", clientIP(r), "messages", 0)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Commit lastread only after producing the bytes (mirror the terminal path).
	s.deps.Service.CommitExport(u.Handle, res)
	slog.Info("qwk api packet", "handle", u.Handle, "remote", clientIP(r), "messages", res.MessageCount)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-QWK-Messages", strconv.Itoa(res.MessageCount))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(res.Packet)
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	u := userFromContext(r.Context())
	if !s.packetLimit.allow(u.Handle) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxREPBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "read error")
		return
	}
	if len(data) > maxREPBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "packet exceeds 16 MiB")
		return
	}
	res, err := s.deps.Service.ImportREP(data, qwkservice.ImportOptions{
		Handle:    u.Handle,
		Signature: u.AutoSignature,
		Authorize: s.deps.AuthorizeFor(u),
	})
	if err != nil {
		if errors.Is(err, qwkservice.ErrWrongBBS) {
			slog.Info("qwk api reply", "handle", u.Handle, "remote", clientIP(r), "outcome", "wrongBBS")
			writeJSON(w, http.StatusOK, replyResponse{WrongBBS: true})
			return
		}
		slog.Error("qwk api import rep", "handle", u.Handle, "error", err)
		writeError(w, http.StatusBadRequest, "bad_packet", "could not process packet")
		return
	}
	slog.Info("qwk api reply", "handle", u.Handle, "remote", clientIP(r),
		"posted", res.Posted, "skipped", res.Skipped, "duplicate", res.Duplicate)
	writeJSON(w, http.StatusOK, replyResponse{Posted: res.Posted, Skipped: res.Skipped, Duplicate: res.Duplicate})
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/qwkapi/ -v 2>&1 | tail -20`
Expected: PASS (all qwkapi tests, including Tasks 2–4).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/qwkapi/server.go internal/qwkapi/handlers.go internal/qwkapi/server_test.go
go vet ./internal/qwkapi/
git add internal/qwkapi/server.go internal/qwkapi/handlers.go internal/qwkapi/server_test.go
git commit -m "feat(qwkapi): server, routes, and login/packet/reply handlers"
```

---

## Task 6: Headless ACS authorizer + `main.go` wiring

**Files:**
- Create: `internal/menu/qwk_authorizer.go`
- Test: `internal/menu/qwk_authorizer_test.go`
- Modify: `cmd/vision3/main.go`

**Interfaces:**
- Produces: `func QWKWriteAuthorizer(u *user.User) func(area *message.MessageArea) bool` in package `menu` — a headless per-area write-ACS gate (nil session/terminal).
- Consumes (main.go): `qwkapi.NewServer`, `qwkapi.Deps`, `qwkservice.New`, `menu.QWKWriteAuthorizer`, `resolveQWKID`.

- [ ] **Step 1: Write the failing test**

`internal/menu/qwk_authorizer_test.go`:

```go
package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestQWKWriteAuthorizer(t *testing.T) {
	lowUser := &user.User{Handle: "newbie", AccessLevel: 10}
	auth := QWKWriteAuthorizer(lowUser)

	// Empty ACS => allowed.
	if !auth(&message.MessageArea{ACSWrite: ""}) {
		t.Error("empty ACS should allow")
	}
	// Security-level gate the user fails.
	if auth(&message.MessageArea{ACSWrite: "s50"}) {
		t.Error("s50 should deny AccessLevel 10")
	}
	// Security-level gate the user passes.
	if !auth(&message.MessageArea{ACSWrite: "s5"}) {
		t.Error("s5 should allow AccessLevel 10")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/menu/ -run TestQWKWriteAuthorizer -v`
Expected: FAIL — `QWKWriteAuthorizer` undefined.

- [ ] **Step 3: Implement `internal/menu/qwk_authorizer.go`**

```go
package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// QWKWriteAuthorizer returns a per-area write-ACS gate for use outside a
// terminal session (e.g. the QWK packet API). It evaluates ACS headlessly:
// session-only conditions (L, A) are false because there is no ssh.Session, so
// only user-intrinsic conditions (security level, flags, SYSOP) can grant
// access. An empty ACSWrite allows the area, matching the terminal path.
func QWKWriteAuthorizer(u *user.User) func(area *message.MessageArea) bool {
	return func(area *message.MessageArea) bool {
		if area == nil {
			return false
		}
		return area.ACSWrite == "" || checkACS(area.ACSWrite, u, nil, nil, time.Now())
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/menu/ -run TestQWKWriteAuthorizer -v`
Expected: PASS.

- [ ] **Step 5: Wire the server into `cmd/vision3/main.go`**

After the telnet-server block (near `cmd/vision3/main.go:1975`), add — following the same goroutine + `defer` shape:

```go
	// Start QWK packet API if enabled (Phase 7 — experimental).
	if serverConfig.QWKAPI.Enabled {
		qwkBBSID := resolveQWKIDForAPI(serverConfig) // see note below
		qwkSvc := qwkservice.New(messageManager, qwkBBSID, serverConfig.BoardName, serverConfig.SysOpName, messageManager.DataPath())
		apiSrv, apiErr := qwkapi.NewServer(qwkapi.Deps{
			Config:       serverConfig.QWKAPI,
			ConfigDir:    rootConfigPath,
			Users:        userManager,
			Service:      qwkSvc,
			AuthorizeFor: menu.QWKWriteAuthorizer,
		})
		if apiErr != nil {
			logging.Fatal("failed to create QWK API server", "error", apiErr)
		}
		go func() {
			if err := apiSrv.Start(); err != nil {
				slog.Error("QWK API server error", "error", err)
			}
		}()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = apiSrv.Shutdown(ctx)
		}()
		slog.Info("QWK API enabled (experimental)", "addr", serverConfig.QWKAPI.ListenAddr())
	} else {
		slog.Info("QWK API disabled")
	}
```

Notes for the implementer:
- `resolveQWKID` lives in package `menu` (unexported). Either export a thin `menu.ResolveQWKID(cfg config.ServerConfig) string` wrapper (add it next to `QWKWriteAuthorizer`) and call it here as `menu.ResolveQWKID(serverConfig)`, or replicate the two-line logic (`config.NormalizeQWKID(cfg.QWKID)` else `menu`'s `qwkBBSID`). Prefer exporting `menu.ResolveQWKID` for DRY — add it in Step 3's file and a one-line test.
- Confirm the exact identifiers in `main.go` for the user manager (`userManager`), message manager (`messageManager`), and config-dir (`rootConfigPath`) variables; adjust to the real names in scope (they appear in the SSH/telnet wiring above).
- Add imports: `"github.com/ViSiON-3/vision-3-bbs/internal/qwkapi"`, `"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"`, and `context` if not already imported.

Update the test in Step 1/3 to also cover `menu.ResolveQWKID` if you add it (assert explicit ID wins over derived).

- [ ] **Step 6: Verify build + tests**

Run: `go build ./... && go test ./internal/menu/ ./internal/qwkapi/ 2>&1 | tail -5`
Expected: build succeeds; tests PASS.

Manual smoke (optional): set `qwkAPI.enabled=true` in a scratch config, start the BBS, confirm the log line `QWK API listening ... fingerprint=...` and that `curl -k https://127.0.0.1:8666/api/qwk/login` without `X-V3-Client` returns 404.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/menu/qwk_authorizer.go internal/menu/qwk_authorizer_test.go cmd/vision3/main.go
go vet ./internal/menu/ ./cmd/vision3/
git add internal/menu/qwk_authorizer.go internal/menu/qwk_authorizer_test.go cmd/vision3/main.go
git commit -m "feat(qwkapi): headless ACS authorizer and main.go wiring"
```

---

## Task 7: Sysop documentation

**Files:**
- Create: `docs/sysop/messages/qwk-api.md`
- Modify: `docs/sysop/messages/qwk.md` (one cross-link)

- [ ] **Step 1: Write `docs/sysop/messages/qwk-api.md`**

Content must cover, in this order, and MUST lead with the experimental warning:

```markdown
# QWK Packet API (Experimental)

> ⚠️ **Experimental — do not enable in production yet.** This HTTP API exists to
> support a future ViSiON/3 mobile client (offline QWK mail on a phone). Until
> that client ships there is nothing to connect to it, and its security surface
> and behavior may still change. Leave `qwkAPI.enabled` set to `false` unless you
> are actively developing or testing against it.

## What it is

A small, optional HTTPS API that lets an authenticated user download a QWK mail
packet and upload a REP reply packet **without a terminal session** — the same
offline read/reply cycle as the SSH/telnet QWK menu, over HTTP. It is packet-only:
no message browsing, no online composition.

## Why it exists

ViSiON/3's goal is to modernize the classic BBS experience without abandoning its
model. Offline mail (QWK/REP) is how BBS users have always read and replied in
bulk; this API is the transport that lets a phone app do the same thing natively.
It deliberately reuses the exact same packet engine as the terminal path, so mail
behaves identically however you connect.

## How it aligns with ViSiON/3's goals

- **Same engine, new door.** It is a thin shell over the existing QWK service —
  no separate message logic, no web frontend, no browsing UI.
- **Sysop stays in control.** Off by default; one switch to enable; standard BBS
  credentials and per-area write ACS still apply.
- **Frictionless.** No reverse proxy, web server, or domain required.

## How to configure

In `configs/config.json`, the server config has a `qwkAPI` block:

| Field | Default | Meaning |
|-------|---------|---------|
| `enabled` | `false` | Master switch. Leave false until a mobile client exists. |
| `host` | `"0.0.0.0"` | Listen interface (use `127.0.0.1` to restrict to local). |
| `port` | `8666` | HTTPS port. |
| `certFile` / `keyFile` | `""` | Optional real TLS cert/key. Blank = auto self-signed. |
| `tokenTTLHours` | `24` | Login token lifetime. |

When enabled with no cert configured, the BBS generates a self-signed certificate
(`configs/qwkapi_cert.pem` / `qwkapi_key.pem`) on first start and logs its SHA-256
fingerprint — no certificate setup required. The mobile client trusts that
fingerprint the same way an SSH client trusts a host key. Sysops who have a real
certificate (e.g. Let's Encrypt) can point `certFile`/`keyFile` at it instead.

## Security model

- All traffic is HTTPS. Users authenticate with their normal BBS handle/password
  and receive a time-limited token.
- Browsers and casual clients are filtered out (the API is not browsable), but
  that filtering is a convenience, not the security boundary — TLS plus BBS
  credentials are.
- Tokens live in memory only; restarting the BBS logs API clients out.

## Endpoints (for client developers)

- `POST /api/qwk/login` → `{token, expiresAt}`
- `GET /api/qwk/packet` → `.QWK` bytes (or `204` when there is no new mail)
- `POST /api/qwk/reply` → `{posted, skipped, duplicate, wrongBBS}`

All requests must send the `X-V3-Client` header and, except login, an
`Authorization: Bearer <token>` header.
```

- [ ] **Step 2: Cross-link from the main QWK doc**

In `docs/sysop/messages/qwk.md`, under the "Files and data" or a new short section, add:

```markdown
## Packet API (experimental)

An optional HTTPS API can expose QWK download/upload to a future mobile client.
It is **off by default and experimental** — see [QWK Packet API](qwk-api.md).
```

- [ ] **Step 3: Commit**

```bash
git add docs/sysop/messages/qwk-api.md docs/sysop/messages/qwk.md
git commit -m "docs(qwkapi): sysop documentation for the experimental packet API"
```

---

## Task 8: Full verification

- [ ] **Step 1: Format, vet, and test the whole tree**

Run: `gofmt -l internal/qwkapi internal/menu internal/config cmd/vision3`
Expected: no output.

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -8`
Expected: all packages PASS.

Run: `go test -race ./internal/qwkapi 2>&1 | tail -5`
Expected: PASS, no race warnings (the token store, limiter, and handlers are concurrent).

- [ ] **Step 2: Commit any cleanup**

```bash
git add -A
git commit -m "chore(qwkapi): Phase 7 cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** config gate → T1; TLS self-signed + override + fingerprint → T2; token auth + bearer middleware → T3; browser/casual filter + rate limiting → T4; endpoints + transport + structured results + wrong-BBS-as-200 → T5; headless ACS gate + disabled-by-default wiring + fingerprint log → T6; experimental sysop docs → T7; race/format/vet verification → T8. All spec sections mapped.
- **Placeholder scan:** every code step shows full code. The only implementer-judgment note is in T6 (the real `main.go` variable names for the user/message managers and config-dir must be read from that large file and matched — flagged explicitly, not a silent TODO).
- **Type consistency:** `Deps`, `PacketService`, `Authenticator`, `tokenStore`(`Issue`/`Resolve`/`requireBearer`), `requireClient`, `limiter.allow`, `loadOrCreateCert`, `QWKWriteAuthorizer`, and the JSON shapes (`loginResponse`, `replyResponse`) are used consistently across tasks. `PacketService` matches the real `*qwkservice.Service` method set (`BuildPacket`/`CommitExport`/`ImportREP`), so the concrete service satisfies it and the fake in T5 mirrors it.
- **Scope:** single cohesive subsystem (`internal/qwkapi`) plus minimal integration (config, one menu helper, main.go, docs). No mobile client (Phase 8), no persistence, no mTLS — all deferred per spec.
