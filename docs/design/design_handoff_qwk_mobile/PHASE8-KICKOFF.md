# Phase 8 Kickoff — QWK Mobile Client (React Native prototype)

This document lets a **fresh session** start Phase 8 without the context of the
sessions that built Phases 0–7. Read it, then follow the process below.

> Phase 8 is the React Native mobile client that consumes the **Phase 7 QWK
> Packet API** (shipped, off by default). It implements the offline
> read/reply/upload loop against real `.QWK`/`.REP` packets — no live browsing,
> no terminal scraping.

## Where this is built (separate repo)

The mobile client lives in its **own repository**, not in `vision-3-bbs`:

> **https://github.com/ViSiON-3/vision-3-qwk-mobile.git**

`vision-3-bbs` keeps only (a) the **Phase 7 server API** the app talks to and
(b) this **design handoff** as the canonical design reference. All Phase 8
application code — React Native app, offline store, QWK/REP parsing on the
device — is built and committed in `vision-3-qwk-mobile`.

Practical setup for the Phase 8 session:

- Work in the `vision-3-qwk-mobile` repo.
- Copy this design-handoff bundle (`docs/design/design_handoff_qwk_mobile/`,
  including this file) into the mobile repo — e.g. under `docs/design/` there —
  so the app's designs and API contract live with the code. (Or keep
  `vision-3-bbs` checked out alongside and reference it.)
- The **Phase 7 API contract below is the cross-repo interface** — the only thing
  the mobile app needs from `vision-3-bbs`. It is reproduced here so the mobile
  repo is self-sufficient once the bundle is copied over.

## How to kick off (paste into the new session, working in `vision-3-qwk-mobile`)

> "Start Phase 8 of the ViSiON/3 QWK roadmap — the React Native mobile client,
> built in this repo (`vision-3-qwk-mobile`). Read the design handoff in
> `docs/design/design_handoff_qwk_mobile/` — `PHASE8-KICKOFF.md` first (it has the
> Phase 7 API contract and the design source), then the `README.md` and
> screenshots. Use the superpowers brainstorming → writing-plans →
> subagent-driven-development flow. Begin by brainstorming the app architecture
> and the first sub-project's spec."

(If the design bundle hasn't been copied into this repo yet, add `vision-3-bbs`
as a source and read the bundle from
`docs/design/design_handoff_qwk_mobile/` there.)

Phase 8 is large enough that it should be **decomposed into sub-projects** during
brainstorming (e.g. app shell + navigation; auth/connect; sync/download; offline
store + reader; compose/outbox/upload; settings/boards/storage). Each sub-project
gets its own spec → plan → implementation cycle. Do **not** try to spec the whole
app at once.

## Design source (in this folder)

- `README.md` — the authoritative design handoff: overview, design tokens
  (colors, typography — `IBM Plex Mono` + the bundled `IBMVGA` CP437 face),
  spacing/radius, and per-screen notes. **High fidelity** — recreate closely.
- `QWK Offline Reader.dc.html` — the interactive HTML prototype (visual +
  behavioral spec). **Do not port** its `.dc`/`support.js` runtime; rebuild the
  screens as React Native components.
- `screenshots/01..15` — the full flow: Connect → Sync → Private Mail →
  Conferences → Areas → Message List → Reader → Compose Reply → Outbox
  (pending / import report) → Settings → Storage → Boards → Edit/Add Board.
- `fonts/WebPlus_IBM_VGA_8x16.woff` — the terminal face; load as an RN font asset.
- `assets/ViSiON3.png` — logo.

Treat the HTML as the look/copy/interaction spec; wire it to the real packet API
(below) and a real offline store. The device bezels, OS status bar, keyboard
mock, and iOS/Android toggle in the prototype are presentation scaffolding — the
OS provides those; don't build them.

## Phase 8 scope (from the roadmap)

See `docs-internal/plans/2026-06-29-qwk-rep-sync-mobile-design.md` → "Phase 8:
React Native Prototype". Summary:

- **Build:** login → packet download → local packet storage → read/reply offline
  → REP upload → import report.
- **Decide during brainstorming:** whether the app embeds a QWK engine or wraps
  an existing reader core; RN navigation + styling libraries; the offline store
  (packet cache + parsed-message store + outbox of pending replies).
- **Validate** offline round trips against a real ViSiON/3 instance, and capture
  mobile-specific failure cases: interrupted downloads, duplicate uploads, stale
  packet state.
- **Acceptance:** a user completes an offline read/reply/upload cycle on mobile;
  no web frontend is involved; packet retry behavior is safe.
- **Non-goals:** online message browsing, terminal scraping, composing outside
  REP import.

---

## The Phase 7 API contract (what the client talks to)

The mobile client is a pure consumer of the **QWK Packet API** in
`internal/qwkapi` (see `docs/sysop/messages/qwk-api.md` and the Phase 7 spec
`docs/superpowers/specs/2026-07-01-qwk-phase7-packet-api-design.md`). It is
**HTTPS, off by default**, and enabled per-BBS via the `qwkAPI` config block.

### Enabling it on a test BBS

In `configs/config.json`, set the server config's `qwkAPI` block:

```json
"qwkAPI": { "enabled": true, "host": "0.0.0.0", "port": 8666,
            "certFile": "", "keyFile": "", "tokenTTLHours": 24 }
```

On first start with no cert configured, the BBS generates a self-signed cert
(`configs/qwkapi_cert.pem` / `configs/qwkapi_key.pem`) and **logs its SHA-256
fingerprint**. The mobile client pins that fingerprint (see "Trust" below).

### Transport & required headers

- Base URL: `https://<bbs-host>:8666/api/qwk/`
- **Every** request must send header `X-V3-Client: vision3-mobile` (any non-empty
  value works today; requests without it — and browser-signature requests — get
  `404`). Sending a stable client identifier is the convention.
- Every request except `login` must send `Authorization: Bearer <token>`.
- Errors are JSON: `{"error": "<code>", "message": "<text>"}`. Rate limits return
  `429` with a `Retry-After` header (login 5/min per IP; packet/reply 30/min per
  user).

### Endpoints

| Method | Path | Request | Response |
|--------|------|---------|----------|
| `POST` | `/api/qwk/login` | JSON `{"handle","password"}` | `200` `{"token","expiresAt"}` · `401` bad creds · `500` `{"error":"internal"}` if token issue fails |
| `GET` | `/api/qwk/packet` | (bearer) | `200` `application/zip` body = the `.QWK`, header `X-QWK-Messages: <n>` · `204` when no new mail |
| `POST` | `/api/qwk/reply` | (bearer) `.REP` bytes, `application/zip`, ≤ 16 MiB | `200` `{"posted","skipped","duplicate","wrongBBS"}` · `400` `{"error":"bad_packet"}` on unparseable body |

Notes for the client:
- **Auth model = "log in once, carry a token"** — the HTTP analog of an SSH
  session. Store the token (not the password); it expires (default 24h) and is
  invalidated by a BBS restart (in-memory) — on `401`, re-run `login`.
- **Download**: `204` means "nothing new" (not an error). On `200`, the body is a
  standard QWK zip (CONTROL.DAT, MESSAGES.DAT, HEADERS.DAT, NNN.NDX,
  PERSONAL.NDX, DOOR.ID). The server advances the user's newscan pointers only
  after producing the packet — so a **failed/interrupted download can lose
  messages**; the client should treat a download as committed only once the bytes
  are fully received, and be prepared to not re-see them.
- **Upload**: send the `.REP` zip (a `BBSID.MSG` inside a zip). `wrongBBS:true`
  means the packet was addressed to a different BBS ID (a normal `200`, not an
  error). Uploads are de-duplicated server-side, so **retrying the same `.REP`
  after a dropped connection is safe** — a duplicate reports `duplicate>0` and
  posts nothing. Design the outbox around this: retry is idempotent.
- **Long fields & threading** already work in the packets: HEADERS.DAT carries
  full-length To/From/Subject (beyond the 25-char base limit) and a Message-ID;
  the reference field carries reply-parent numbers. The client's QWK
  parser/writer must read/preserve HEADERS.DAT and the reference field.

### Trust (TLS)

The server cert is self-signed by default. The client should **pin the
fingerprint** the BBS logs on startup (trust-on-first-use or sysop-entered),
exactly like an SSH host key — not rely on system CA trust. A sysop who
configures a real cert (`certFile`/`keyFile`) can be trusted normally; support
both.

---

## Dependencies & open items

- **Requires a running ViSiON/3 BBS with `qwkAPI.enabled=true`** to test against.
- Two QWK validation gaps are still open (tracked separately) and are worth
  closing alongside mobile work, since the client is the natural way to exercise
  them:
  1. **Third-party HEADERS.DAT interop** is unverified — no real
     MultiMail/Synchronet fixture captured yet.
  2. **The live REP-upload round-trip** has not been exercised end-to-end on a
     running BBS — the mobile upload path is the first real consumer.

## Process reminder

Follow the same discipline used for Phases 0–7:
**brainstorming** (decompose into sub-projects; design each) → **writing-plans**
(bite-sized TDD tasks) → **subagent-driven-development** (implement + review per
task, whole-branch review, PR). Keep the app a thin client over the packet API —
no BBS logic reimplemented on the device beyond QWK/REP parsing.
