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

There is no config editor screen for the API yet. In `configs/config.json`,
the server config has a `qwkAPI` block:

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
- **The BBS's login protections cover the API too.** An IP on the
  `ipBlocklistPath` blocklist gets `403` on every endpoint (the allowlist still
  exempts an address, as it does for telnet/SSH). Failed API logins count toward
  the same per-IP `maxFailedLogins` / `lockoutMinutes` lockout as the terminal
  login prompt, so guesses can't be split between the two to dodge it — and an
  IP that trips it is added to the blocklist just as it would be from telnet.
  See [IP Blocklist/Allowlist Files](configuration/configuration.md#ip-blocklistallowlist-files).
- **Each handle also locks after 10 failed API logins** within 15 minutes, for
  15 minutes, whichever IPs the attempts came from — this is what stops
  guessing spread across many addresses. While locked, even the correct
  password is refused. The lock only applies to the API, so someone who
  deliberately locks another user's handle pauses that user's mobile sync but
  does not keep them off the BBS. Handles are counted whether or not the
  account exists, so a lockout doesn't reveal which handles are real.
- **Per-area write ACS still applies** on upload, exactly as in the terminal path.
- **The caller's Twit List still applies** on download. A packet built by the
  API leaves out the same authors the terminal would, using the list as last
  saved (not as it was when the token was issued), and honours the board's
  **Twit Lists** switch, the exemption levels, `twitListMax` and
  `anonymousName`. The BBS refuses to start the API without that hook, so
  there is no unfiltered path. See [Twit Filter List](messages/twit-list.md).
- **Auto-signatures** are added to uploaded replies as in the terminal, except
  for callers who have **Ask each post** on: the API cannot ask, so their
  replies go out unsigned (see [Auto-Signature](messages/autosig.md#asking-before-each-post)).
- **Browsers and casual clients are filtered out** (the API is not browsable and
  returns `404` to anything that looks like a web browser or omits the client
  header). That filtering is a convenience to keep stray traffic away — it is
  **not** the security boundary. TLS plus BBS credentials are.

## Technical Reference

### Endpoints (for client developers)

All requests must send an `X-V3-Client` header; every request except `login` must
also send `Authorization: Bearer <token>`.

| Method | Path | Body → Response |
|--------|------|-----------------|
| `POST` | `/api/qwk/login` | `{handle, password}` → `{token, expiresAt}` (`401` on bad credentials, `429` with `error: "locked_out"` while the IP or handle is locked out) |
| `GET` | `/api/qwk/packet` | → `.QWK` bytes (`application/zip`) with `X-QWK-Messages` count, or `204` when there is no new mail |
| `POST` | `/api/qwk/reply` | `.REP` bytes (`application/zip`, max 16 MiB) → `{posted, skipped, duplicate, wrongBBS}` |

Errors are JSON: `{"error": "<code>", "message": "<text>"}`. Rate limits apply
(login per client IP, packet/reply per user); exceeding them returns `429` with a
`Retry-After` header. A lockout also answers `429`, with `Retry-After` set to the
seconds left on it. A request from a blocklisted IP gets `403` on any endpoint.

### What gets logged

Everything the API does goes to the normal BBS log (`data/logs/vision3.log` by
default, JSON, one record per line) — there is no separate API log file. QWK API
sessions do **not** appear in Last Callers; that list is for terminal logins.

Apart from the `QWK API listening` line at startup, every record's `msg` starts
with `qwk api`, so `grep -i '"qwk api'` on the log gives you the full picture of
API activity.

| Level | `msg` | When |
|-------|-------|------|
| INFO | `QWK API listening` | Startup: listen address and TLS cert fingerprint. |
| INFO | `qwk api login` | `outcome=success` / `outcome=fail`, with `handle` and `remote`. A malformed body logs `outcome=badRequest` with `remote` and `error` but **no** `handle` — on a decode failure that field may be partial or hold the password. |
| WARN | `qwk api login` | `outcome=rateLimited` — too many login attempts from one IP. |
| WARN | `qwk api login from locked IP` | A login refused because its IP is in the per-IP lockout, with `remote` and `locked_until`. |
| WARN | `qwk api login for locked handle` | A login refused because the handle is locked, with `handle`, `remote`, and `locked_until`. |
| WARN | `qwk api handle locked out after failed logins` | A handle just reached the failure limit, with `handle`, `remote`, and `minutes`. |
| ERROR | `qwk api login: token issue failed` | The server could not mint a token. |
| INFO | `qwk api auth` | `outcome=noToken` / `outcome=badToken` — a request with a missing, invalid, or expired bearer token. |
| INFO | `qwk api packet` | A download, with `messages` count (including the `0`/`204` case), or `outcome=rateLimited`. |
| INFO | `qwk api reply` | An upload, with `posted`/`skipped`/`duplicate`, or `outcome=wrongBBS`, `readError`, or `rateLimited`. |
| WARN | `qwk api reply` | `outcome=tooLarge` — an upload over the 16 MiB cap. |
| ERROR | `qwk api build packet` / `qwk api import rep` | A packet failed to build or import. |
| WARN | `qwk api server` | Connection-level trouble from Go's HTTP server, e.g. `http: TLS handshake error from ...`. |
| DEBUG | `qwk api rejected` | A request turned away without doing any work: `reason=noClientHeader`, `browser`, `htmlAccept`, `unknownPath`, `badMethod`, `pathRedirect`, or `blocked` (a blocklisted IP). Each carries `remote`, `method`, `path`, and `agent`. |

Failed logins, bad tokens, oversized uploads, lockouts, and rate-limit trips are
all recorded at INFO or WARN, so they are visible with the default log level.
The lockout records carry `category=security`. A failed API login also produces
the BBS's usual `failed login attempt` / `IP locked out after failed login
attempts` records from the shared per-IP lockout; those don't start with
`qwk api`.

Rejected probes are the exception: an open port on the public internet collects
constant scanner traffic (`/wp-login.php` and friends), and logging every one of
those at INFO would bury real activity. They are recorded at DEBUG instead — set
`logging.level` to `"debug"` in `configs/config.json` when you want to see who is
knocking, then set it back.
