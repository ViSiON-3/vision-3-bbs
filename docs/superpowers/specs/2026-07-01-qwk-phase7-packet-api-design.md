# QWK Phase 7 — Packet Transport API

**Date:** 2026-07-01
**Status:** Approved design
**Branch:** `qwk-phase7-packet-api`
**Parent plan:** `docs-internal/plans/2026-06-29-qwk-rep-sync-mobile-design.md` (Phase 7)

---

## Problem

QWK download/upload is only reachable through the terminal (SSH/telnet) menu flow
(`W` → `D`/`U`). A mobile client (Phase 8) needs to authenticate, download a
`.QWK`, and upload a `.REP` over the network **without driving a terminal
session**. Phase 7 adds a small, config-gated HTTP API over the existing shared
`qwkservice`, with authentication, TLS, casual-access filtering, rate limiting,
and audit logging. It is **disabled by default** and introduces the BBS's first
HTTP surface.

## Goal

A packet-only HTTP API: `login` → `download .QWK` → `upload .REP`, reusing the
shared service and real BBS credentials, frictionless for the sysop to enable
(no reverse proxy, web server, domain, or manual cert handling).

## Scope decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Authentication | `POST /login` (BBS handle+password via `UserMgr.Authenticate`) → time-limited opaque **bearer token**; sent as `Authorization: Bearer <token>` on later requests. Same "authenticate once, session carries you" model as SSH. |
| Token store | **In-memory**, with expiry (default TTL 24h). A restart forces re-login — acceptable; no new database. |
| TLS | App terminates TLS. On enable, if no cert exists it **auto-generates a self-signed cert/key** next to the config and serves HTTPS; prints the fingerprint on startup. Optional `certFile`/`keyFile` override with a real cert. Client pins the fingerprint (SSH-style trust). |
| Casual/browser blocking | Layered filter (not a security boundary): auth required everywhere; binary responses; required `X-V3-Client` header (else 404); reject browser-signature requests (`Sec-Fetch-*` / `Accept: text/html` → 404). No mTLS. |
| Transport | Raw binary packet bodies (`application/zip`); JSON only for metadata and errors. |
| Endpoints | `POST /login`, `GET /packet`, `POST /reply`. Packet-only. |
| Rate limiting | In-memory: login throttled per-IP (5/min); packet/reply throttled per-user (30/min). |
| Config | New `qwkAPI` block mirroring the existing `{Enabled, Host, Port}` + `ListenAddr()` pattern. Disabled by default; port 8666. |

## Non-goals

- Online message browsing or reading endpoints.
- Message composition outside REP import.
- A `/status` (new-message count) endpoint — deferred until the mobile client
  needs it (YAGNI).
- Mutual TLS / per-user client certs.
- Persisted tokens, refresh tokens, or OAuth.
- The mobile client itself (Phase 8).
- Reverse-proxy / ACME / domain-based certificates (escape-hatch cert only).

---

## Architecture

A new package `internal/qwkapi` holds the HTTP layer; it depends on the existing
`qwkservice` (build/import) and `user.UserMgr` (authentication), and reimplements
neither. `cmd/vision3/main.go` starts it alongside the SSH/telnet servers when
`config.ServerConfig.QWKAPI.Enabled` is true.

```
cmd/vision3/main.go
   └─ (if QWKAPI.Enabled) qwkapi.NewServer(deps).ListenAndServeTLS()
internal/qwkapi/
   ├─ server.go     — http.Server wiring, routes, TLS bootstrap, start/stop
   ├─ handlers.go   — login / packet / reply handlers
   ├─ auth.go       — token store (issue/validate/expire), bearer middleware
   ├─ filter.go     — client-header + browser-signature middleware
   ├─ ratelimit.go  — in-memory per-IP / per-user limiter
   ├─ tlscert.go    — self-signed cert generate-or-load
   └─ types.go      — request/response JSON shapes, Deps interface
```

### Dependencies (consuming interface)

`qwkapi` defines a small interface it needs, satisfied by the real services (and
fakeable in tests):

```go
type Authenticator interface {
    Authenticate(handle, password string) (*user.User, bool)
}

type PacketService interface {
    BuildPacket(opts qwkservice.ExportOptions) (*qwkservice.ExportResult, error)
    CommitExport(handle string, res *qwkservice.ExportResult)
    ImportREP(data []byte, opts qwkservice.ImportOptions) (*qwkservice.ImportResult, error)
}
```

`NewServer` takes these plus the `QWKAPIConfig`. Note: `qwkservice.New` currently
takes a `MessageStore`; the API constructs (or is handed) a `*qwkservice.Service`
the same way the menu handler does (`qwkservice.New(e.MessageMgr, bbsID, …)`).

---

## Endpoints

All paths are under `/api/qwk/`. Every non-login request passes the filter and
bearer-auth middleware first.

### `POST /api/qwk/login`

- Request JSON: `{"handle": "...", "password": "..."}`
- Runs `Authenticator.Authenticate`. On success, issues a token:
  `{"token": "<opaque>", "expiresAt": "<RFC3339>"}` (200).
- On failure: 401 `{"error": "unauthorized", "message": "invalid credentials"}`.
- Rate limited per client IP.

### `GET /api/qwk/packet`

- Requires a valid bearer token; the user is resolved from the token.
- Calls `BuildPacket` with the authenticated user's handle (same
  `ExportOptions` the menu path builds: tagged areas / fallback, personal-to,
  lastread advance committed only after success — mirror the menu handler).
- **200**: `Content-Type: application/zip`, body is the `.QWK` bytes, header
  `X-QWK-Messages: <count>`.
- **204 No Content**: no new messages.
- Lastread pointers are advanced only after the bytes are produced (the service
  returns pending `LastRead` updates; commit them as the menu path does).

### `POST /api/qwk/reply`

- Requires a valid bearer token.
- Request body: the raw `.REP` zip (`Content-Type: application/zip`), size-capped
  (proposed 16 MiB).
- Calls `ImportREP` with the authenticated user's handle.
- **200** JSON: `{"posted": n, "skipped": n, "duplicate": n, "wrongBBS": false}`.
  A wrong-BBS packet (`ErrWrongBBS`) maps to `{"wrongBBS": true, ...}` with 200
  (it is a normal, expected outcome, not a server error).
- Parse/read failure: 400 `{"error": "bad_packet", "message": "..."}`.

---

## Authentication and token store (`auth.go`)

- Tokens are opaque, 256-bit, from `crypto/rand`, hex/base64url-encoded.
- Store: `map[string]tokenEntry` under a mutex; `tokenEntry{user *user.User,
  expiresAt time.Time}` — the login-time user snapshot travels with the token so
  the packet/ACS steps have `TaggedMessageAreaTags`, `AutoSignature`, and
  `AccessLevel` without a re-fetch. A background sweeper prunes expired entries
  periodically; validation also rejects (and deletes) expired tokens lazily.
- `Issue(u *user.User) (token, expiresAt, error)`, `Resolve(token) (*user.User, ok)`.
- Bearer middleware: read `Authorization: Bearer <token>`; on miss/expired →
  401. On success, attach the handle to the request context for handlers.
- TTL from config (`tokenTTLHours`, default 24). Restart clears all tokens.

---

## TLS bootstrap (`tlscert.go`)

- If `certFile` and `keyFile` are both set, load them (real cert path). If
  exactly one is set, fail closed with a clear error (do not silently fall back).
- Otherwise, look for an auto-managed pair next to the config
  (`configs/qwkapi_cert.pem` / `configs/qwkapi_key.pem`). If absent, generate a
  self-signed cert (ECDSA P-256, `crypto/x509`, long validity, SANs for the
  configured host plus `localhost`/loopback), persist both files (key `0600`),
  and use them.
- Log the SHA-256 fingerprint on startup so the sysop can convey it to the app.
- Server uses `http.Server{TLSConfig}.ListenAndServeTLS`.

Generation uses Go stdlib only (`crypto/ecdsa`, `crypto/x509`, `crypto/rand`,
`encoding/pem`) — no new dependency.

---

## Casual/browser filtering (`filter.go`)

Middleware applied to all routes, in order, before auth:

1. Require header `X-V3-Client` to be present and non-empty; else **404**
   (don't confirm the endpoint exists).
2. Reject obvious browser requests: if the request carries `Sec-Fetch-Mode`,
   `Sec-Fetch-Site`, or an `Accept` that prefers `text/html`, respond **404**.

This is a filter for casual/browser/scanner traffic, explicitly **not** a
security boundary; TLS + authenticated BBS credentials are the real control.

---

## Rate limiting (`ratelimit.go`)

- In-memory token-bucket (or fixed-window) limiter keyed by:
  - client IP for `/login` (default 5/min),
  - resolved user handle for `/packet` and `/reply` (default 30/min).
- Exceeding the limit → 429 `{"error": "rate_limited"}` with a `Retry-After`
  header.
- Buckets pruned by the same sweeper as tokens (or lazily on access).

---

## Audit logging

Every security-relevant event is logged via `slog` with structured fields:

- login attempt: `handle`, `remote` (IP), `outcome` (success/fail).
- packet download: `handle`, `remote`, `messages` count, or `empty`.
- reply upload: `handle`, `remote`, `posted`/`skipped`/`duplicate`/`wrongBBS`.
- rejected requests (filter/rate-limit/auth) at `WARN`/`INFO` with reason.

Reuse the existing logging conventions; no new logging library.

---

## Configuration

New `QWKAPIConfig` on `config.ServerConfig`, mirroring `V3NetHubConfig`:

```go
type QWKAPIConfig struct {
    Enabled       bool   `json:"enabled"`
    Host          string `json:"host"`          // blank = all interfaces
    Port          int    `json:"port"`          // default 8666
    CertFile      string `json:"certFile"`      // blank = auto self-signed
    KeyFile       string `json:"keyFile"`
    TokenTTLHours int    `json:"tokenTTLHours"`  // default 24
}
func (c *QWKAPIConfig) ListenAddr() string // host:port, default port 8666
```

Defaults (in the config defaults + `templates/configs`):
```json
"qwkAPI": { "enabled": false, "host": "0.0.0.0", "port": 8666,
            "certFile": "", "keyFile": "", "tokenTTLHours": 24 }
```

`cmd/vision3/main.go`: when `Enabled`, build the `qwkservice.Service` (as the menu
handler does), construct `qwkapi.NewServer`, and run it in a goroutine with the
same start/stop/`defer cleanup` shape as the SSH and telnet servers. When
disabled, log "QWK API disabled" and start nothing.

---

## Error handling

- All error responses are JSON `{"error": "<code>", "message": "<human text>"}`,
  never terminal UI strings.
- Codes: `unauthorized` (401), `bad_packet` (400), `rate_limited` (429),
  `not_found` (404, filter/unknown route), `internal` (500).
- Handlers never leak internal error detail into `message`; details go to the
  logs.

---

## Testing

All via `net/http/httptest` against fake `Authenticator` and `PacketService`
implementations (no real network, no real SQLite):

**auth.go**
- issue → resolve returns the handle; expired token → not resolved; unknown
  token → not resolved.

**handlers.go / server.go** (through the full middleware chain)
- `POST /login`: valid creds → 200 + token; bad creds → 401; missing
  `X-V3-Client` → 404; browser-signature headers → 404.
- `GET /packet`: no/invalid/expired bearer → 401; valid → 200 `application/zip`
  with `X-QWK-Messages`; empty export → 204.
- `POST /reply`: valid → 200 with the mapped counts; `ErrWrongBBS` →
  200 `{"wrongBBS": true}`; unparseable body → 400; oversized body → 400/413.
- rate limiting: N+1 logins from one IP → 429; N+1 packet requests for one user
  → 429.

**tlscert.go**
- absent cert → generates and persists both files (key mode `0600`), second call
  loads the same cert; explicit `certFile`/`keyFile` are used when set.

**config**
- `ListenAddr()` default port; JSON round-trip of `QWKAPIConfig`.

Integration smoke: start the server on `127.0.0.1:0` with a self-signed cert and
a fake service, then run a real client through login → packet → reply over TLS
(client configured to trust the generated cert).

---

## Risks / notes

- **In-memory tokens** vanish on restart (users re-login). Acceptable for a
  packet API; documented. Persistence is a later option.
- **Self-signed cert** is not browser-trusted; intended for the pinning mobile
  client. The `certFile`/`keyFile` override exists for sysops who want a real
  cert. Documented.
- **Filtering is not authentication.** The `X-V3-Client` / browser-signature
  checks stop casual and browser traffic but are bypassable; security rests on
  TLS + `UserMgr.Authenticate`.
- **Lastread commit ordering** must match the terminal path exactly (advance
  only after the packet is produced) so a failed/aborted download does not lose
  messages. Mirror the menu handler.
- First HTTP surface in the app — keep the dependency footprint to stdlib
  `net/http` (no web framework), consistent with the project's minimal-deps
  stance.

## Acceptance criteria

- With `qwkAPI.enabled=false` (default), no HTTP listener starts.
- With it enabled and no cert configured, the app generates a self-signed cert,
  serves HTTPS, and logs the fingerprint — no other sysop setup required.
- A client with a valid `X-V3-Client` header can `POST /login`, `GET /packet`
  (200/204), and `POST /reply` (structured counts) over TLS.
- Browsers and requests without the client header receive 404; bad credentials
  401; rate-limit trips 429 — all logged.
- Packet semantics (tagged areas, lastread advance, dedup, wrong-BBS, threading,
  HEADERS.DAT) are identical to the terminal path because both call the same
  `qwkservice`.
