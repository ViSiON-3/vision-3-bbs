# QWK Packet API (Experimental)

> ⚠️ **Experimental — do not enable in production yet.** This HTTP API exists to
> support the [ViSiON/3 QWK Mobile](https://github.com/ViSiON-3/vision-3-qwk-mobile)
> companion client (offline QWK mail on a phone). That app is **not yet
> available for download**, so there is currently nothing end-users can
> connect to this API with, and its security surface and behavior may still
> change. Leave `qwkAPI.enabled` set to `false` unless you are actively
> developing or testing against it.

## What it is

A small, optional HTTPS API that lets an authenticated user download a QWK mail
packet and upload a REP reply packet **without a terminal session** — the same
offline read/reply cycle as the SSH/telnet QWK menu, over HTTP. It is
packet-only: no message browsing, no online composition.

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
- **Frictionless.** No reverse proxy, web server, or domain required — the BBS
  generates its own TLS certificate.

## How to configure

In `configs/config.json`, the server config has a `qwkAPI` block:

| Field | Default | Meaning |
|-------|---------|---------|
| `enabled` | `false` | Master switch. Leave `false` until a mobile client exists. |
| `host` | `"0.0.0.0"` | Listen interface. Use `127.0.0.1` to restrict to the local machine. |
| `port` | `8666` | HTTPS port. |
| `certFile` / `keyFile` | `""` | Optional real TLS cert/key paths. Blank = auto self-signed. |
| `tokenTTLHours` | `24` | Login-token lifetime in hours. |

When enabled with no cert configured, the BBS generates a self-signed certificate
(`configs/qwkapi_cert.pem` / `configs/qwkapi_key.pem`) on first start and logs its
SHA-256 fingerprint — no certificate setup required. The mobile client trusts that
fingerprint the same way an SSH client trusts a host key. Sysops who have a real
certificate (e.g. Let's Encrypt) can point `certFile`/`keyFile` at it instead.

Tokens live in memory only, so restarting the BBS logs API clients out (they
re-authenticate on their next request). This is expected for a packet API.

## Security model

- **All traffic is HTTPS.** Users authenticate with their normal BBS
  handle/password and receive a time-limited bearer token — the same "authenticate
  once, then a session carries you" model as an SSH login.
- **Per-area write ACS still applies** on upload, exactly as in the terminal path.
- **Browsers and casual clients are filtered out** (the API is not browsable and
  returns `404` to anything that looks like a web browser or omits the client
  header). That filtering is a convenience to keep stray traffic away — it is
  **not** the security boundary. TLS plus BBS credentials are.

## Endpoints (for client developers)

All requests must send an `X-V3-Client` header; every request except `login` must
also send `Authorization: Bearer <token>`.

| Method | Path | Body → Response |
|--------|------|-----------------|
| `POST` | `/api/qwk/login` | `{handle, password}` → `{token, expiresAt}` (`401` on bad credentials) |
| `GET` | `/api/qwk/packet` | → `.QWK` bytes (`application/zip`) with `X-QWK-Messages` count, or `204` when there is no new mail |
| `POST` | `/api/qwk/reply` | `.REP` bytes (`application/zip`, max 16 MiB) → `{posted, skipped, duplicate, wrongBBS}` |

Errors are JSON: `{"error": "<code>", "message": "<text>"}`. Rate limits apply
(login per client IP, packet/reply per user); exceeding them returns `429` with a
`Retry-After` header.
