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
| `tokenTTLHours` | `24` | Lifetime in hours of the token a paired device gets when it logs in. |

When enabled with no cert configured, the BBS generates a self-signed certificate
(`configs/qwkapi_cert.pem` / `configs/qwkapi_key.pem`) on first start and logs its
SHA-256 fingerprint — no certificate setup required. The mobile client trusts that
fingerprint the same way an SSH client trusts a host key. Sysops who have a real
certificate (e.g. Let's Encrypt) can point `certFile`/`keyFile` at it instead.

Tokens live in memory only, so restarting the BBS logs API clients out (they
re-authenticate on their next request). This is expected for a packet API.
Paired devices are stored with the user record and survive restarts.

## Paired devices

The app never keeps the user's password. Instead:

1. The user enters their handle and password in the app **once**. That login
   only yields a short-lived (10-minute) *pair* token, which can register a
   device and manage the account's devices — it cannot fetch or upload mail.
2. The app generates a key pair on the phone (in the Secure Enclave / Android
   Keystore where available), sends the BBS the public half, and forgets the
   password.
3. From then on the app logs in by signing a one-time challenge from the BBS
   with its private key, and gets a normal *device* token for packets.

What this means for you as SysOp:

- **A lost or stolen phone holds no password.** The user can unpair it from
  the terminal (**QWK Devices** in the **K** user configuration, or
  `RUN:CFG_QWKDEVICES` on a menu of yours), or from the app on another device
  after a password login there. Any session it has ends on its next request.
- **You can unpair a device for them.** Press **V** in the in-BBS user editor, or
  choose **QWK Devices** in `./ue`, to see a user's devices (name, when paired,
  last used) and unpair one. Changes in `./ue` take effect when you save, and it
  can only remove devices. If you overwrite a newer `users.json` when `./ue`
  warns about it, a device paired since `./ue` loaded is lost too and has to
  pair again.
- **On a board without the API, none of this appears.** These screens show only
  when `qwkAPI.enabled` is on, or for a user who still has devices from when it
  was on, so they can remove them.
- **Changing a user's password unpairs all their devices** — by them, in the
  user editor, or in `./ue`. Someone who learned the password could have
  paired a phone of their own, so a password change has to cut those off. The
  user pairs their real phone again with the new password. It is also the way
  to remove all of a user's devices at once.
- **Banning, deleting or demoting a user** stops their devices logging in,
  exactly as it stops a password login.
- Each account can pair up to **10 devices**.

## Security model

- **All traffic is HTTPS.** The password is used only to pair a device (see
  [Paired devices](#paired-devices)); mail access needs a token from a login
  signed by a paired device's key — the same public-key model as SSH key
  login. Device keys must be P-256 ECDSA or Ed25519.
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
- **Accounts the terminal would turn away are turned away here too.** A
  correct password still gets `403` and no token if the account is below
  `logonLevel` — which includes a banned account (level 0) and a new user
  waiting for validation — or if it still owes the SysOp its new-user
  introduction (`requireNewUserEmail`). The API has no editor to collect that
  message, so the user must log in by terminal once to send it.
- **Account changes end API sessions straight away.** Every request re-reads
  the account, so deleting, banning or demoting a user below `logonLevel`,
  changing their password (by them or by you, including in the standalone
  `./ue` editor while nobody is online), or unpairing the device invalidates
  their API tokens on the next request instead of when the token would have
  expired. That is also
  how to kick a user off the API: change their password or ban them. Requests
  use the account as it is now, so a change to their level, tagged areas or
  signature applies to the next packet without logging in again.
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
- **Failed device logins count like failed passwords** toward the per-IP
  lockout. A device-login challenge works once, for one device, for 60
  seconds, and the signed message names this board, so a signature can't be
  replayed here or against another BBS the same phone is paired with.
- **Browsers and casual clients are filtered out** (the API is not browsable and
  returns `404` to anything that looks like a web browser or omits the client
  header). That filtering is a convenience to keep stray traffic away — it is
  **not** the security boundary. TLS plus BBS credentials are.

## Technical Reference

### Endpoints (for client developers)

All requests must send an `X-V3-Client` header. Every request except `login`,
`challenge` and `login/device` must also send `Authorization: Bearer <token>`.

Tokens have a scope, returned as `scope` in the login response:

- `pair`, from a password login: lasts 10 minutes and can only use the
  `devices` endpoints. Anything else answers `403 device_required`.
- `device`, from a device login: lasts `tokenTTLHours` and can use everything
  except registering a new device (`403 password_login_required`).

A `401` on an authenticated endpoint means the token is gone — expired,
logged out, lost in a restart, or revoked by an account change or unpairing —
and the client should log in again (with its device key; if that fails with
`401`, the device is no longer paired and needs a password login to pair
again). A `403` with a deny code (`logon_level`, `intro_pending`) means logging
in again will not help.

**Device login.** `POST /api/qwk/challenge` with `{deviceId}`, then sign the
UTF-8 bytes of

```text
ViSiON/3 QWK API device login v1\n<boardId>\n<deviceId>\n<challenge>
```

(`\n` is a newline; `boardId` is returned when pairing) and `POST
/api/qwk/login/device` with `{deviceId, challenge, signature}`, the signature
base64-encoded. ECDSA signatures are over SHA-256 of the message, as ASN.1 DER
or raw 64-byte `r||s`; Ed25519 signs the message itself. Public keys are sent
as base64 DER SubjectPublicKeyInfo.

| Method | Path | Body → Response |
|--------|------|-----------------|
| `POST` | `/api/qwk/login` | `{handle, password}` → `{token, expiresAt, scope: "pair"}` (`401` on bad credentials, `429` with `error: "locked_out"` while the IP or handle is locked out, `403` with `error: "logon_level"` or `"intro_pending"` when the account may not log in) |
| `POST` | `/api/qwk/devices` | *pair token.* `{name, publicKey}` → `201 {deviceId, boardId}` (`400 bad_key`, `409 key_exists` / `too_many_devices`) |
| `GET` | `/api/qwk/devices` | *either token.* → `[{id, name, createdAt, lastUsed, current}]`; `current` marks the device the token belongs to |
| `DELETE` | `/api/qwk/devices/{id}` | *either token.* → `204`, or `404` if not paired. Unpairing ends that device's sessions. |
| `POST` | `/api/qwk/challenge` | `{deviceId}` → `{challenge, expiresAt}`. Issued for any ID, paired or not. |
| `POST` | `/api/qwk/login/device` | `{deviceId, challenge, signature}` → `{token, expiresAt, scope: "device"}` (`401 challenge_invalid` for an expired, reused or mismatched challenge — get a new one; `401 unauthorized` for a bad signature or unpaired device; lockout and deny codes as for `login`) |
| `POST` | `/api/qwk/logout` | → `204`; the token stops working. Works even if the account may no longer log in. |
| `GET` | `/api/qwk/packet` | *device token.* → `.QWK` bytes (`application/zip`) with `X-QWK-Messages` count, or `204` when there is no new mail |
| `POST` | `/api/qwk/reply` | *device token.* `.REP` bytes (`application/zip`, max 16 MiB) → `{posted, skipped, duplicate, wrongBBS}` |

Errors are JSON: `{"error": "<code>", "message": "<text>"}`. Rate limits apply
(login and device login per client IP, challenges per client IP, packet/reply
per user); exceeding them returns `429` with a
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
| INFO | `qwk api login` | `outcome=success` / `outcome=fail`, with `handle` and `remote`; `outcome=denied` with `reason=logon_level` or `intro_pending` for a correct password on an account that may not log in. A malformed body logs `outcome=badRequest` with `remote` and `error` but **no** `handle` — on a decode failure that field may be partial or hold the password. |
| WARN | `qwk api login` | `outcome=rateLimited` — too many login attempts from one IP. |
| WARN | `qwk api login from locked IP` | A login refused because its IP is in the per-IP lockout, with `remote` and `locked_until`. |
| WARN | `qwk api login for locked handle` | A login refused because the handle is locked, with `handle`, `remote`, and `locked_until`. |
| WARN | `qwk api handle locked out after failed logins` | A handle just reached the failure limit, with `handle`, `remote`, and `minutes`. |
| ERROR | `qwk api login: token issue failed` | The server could not mint a token. |
| INFO | `qwk api auth` | `outcome=noToken` / `outcome=badToken` — a request with a missing, invalid, or expired bearer token. `outcome=revoked` with `handle` and `reason` (`accountGone`, `passwordChanged`, `deviceUnpaired`, `logon_level`, `intro_pending`) — a token ended because the account changed. |
| INFO | `qwk api logout` | A client logged out, with `handle` and `remote`. |
| INFO | `qwk api auth` | `outcome=wrongScope` — a pair token used for packets, or a device token used to pair. |
| INFO | `qwk api device login` | `outcome=success` with `handle` and `device`; `badChallenge`, `unknownDevice`, `badSignature` (the last two count as failed logins), `denied`, or `badRequest`. |
| WARN | `qwk api device login` | `outcome=rateLimited`. |
| WARN | `qwk api device paired` / `qwk api device unpaired` | A device was paired or unpaired, with `handle`, `device`, `name` (paired) and `remote`. `category=security`. |
| INFO | `qwk api pair` | A pairing refused: `outcome=badKey`, `keyExists` or `tooMany`. |
| WARN | `qwk api challenge` | `outcome=rateLimited`, or `storeFull` (too many outstanding challenges; answered `503`). |
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
