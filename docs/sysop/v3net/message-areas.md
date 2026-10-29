# V3Net Message Areas

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net areas are message areas that are networked using the V3Net protocol.
Messages posted locally are relayed to the hub and distributed to all
subscribed nodes. Messages posted on other nodes arrive via polling and SSE
(Server-Sent Events). For a sysop this means echomail-style conversation
across boards with none of the mailer, packet or nodelist upkeep: subscribe to
an area in the config editor or from the BBS, and the local message base, the
sending and the receiving are all handled for you.

## How V3Net Areas Differ from Other Types

| Feature | Local | FTN Echomail | V3Net |
|---------|-------|-------------|-------|
| Networking | None | FTN mailer + tosser | Built-in (REST+SSE) |
| Authentication | N/A | Packet passwords | Ed25519 signatures |
| Area discovery | Manual | AREAS.BBS | NAL (Network Area List) |
| Message dedup | N/A | MSGID CRC | UUID-based SQLite index |
| Transport | N/A | .PKT files via binkd | HTTP (JSON) |
| Real-time delivery | No | Polling only | SSE push + polling |

## Creating V3Net Areas

V3Net areas are created automatically when your BBS subscribes to network areas
via the NAL (Network Area List). After subscribing to a network, go to
**V3Net > Area Subscriptions** and press **Space** on any area — the local
message base is created for you.

You can also create areas manually through the config editor:

```
./config  →  3 — Areas and Conferences  →  Message Areas  →  [I] Insert
```

Set the area's **Tag** to match the network area tag (e.g. `fn.general`), then
add a leaf subscription via **V3Net → Subscriptions** with the same board tag.
Choose the `v3net` area type and set **Network** to the network name; an area
that is only bound by tag in `v3net.json` works too, whatever its type.

### New Areas on Your Networks

When a hub adds areas to a network your BBS is already on, each SysOp is asked
about them at their next login, one question per area:

```
New Felonynet area: Music. Add?  Yes  No
```

**Yes** does what pressing **Space** on the area in the area browser does: the
area is added to your subscriptions in `v3net.json`, its local message base is
created, and the change is applied without a restart. **No** dismisses the
question for good and reminds you how to add the area later: open the V3Net
menu, then **Area Subscriptions**, highlight the area, and press **Space**.
Either way, each area is asked about once.

- The questions come from the `SYSOPNOTICES` login step, so it must be in your
  `login.json` (the shipped one runs it first). See
  [SYSOPNOTICES](users/login-sequence.md#sysopnotices).
- Only full SysOps (`sysOpLevel` and above) are asked, since adding areas is
  the same access as the area browser. With several SysOps, whoever answers
  Yes first adds it, and the others are not asked again.
- Areas the hub added while your BBS was down, or while its connection to the
  hub was down, are caught when it reconnects.
- When your BBS first sees a network (a new join, or the first start of a
  build with this feature), only areas the hub added in the last 30 days are
  offered. Older areas are not; browse those in the area browser. The hub
  records when each area was added, so this needs a hub running a build that
  does; with an older hub, no areas are offered at this point.
- A closed area is only offered when your node is on its allow list. An
  approval-mode area is offered, and Yes sends the access request to its
  manager.
- If the hub has removed the area by the time you log in, or closed it to
  your node, the question is dropped. If you already added it from the area
  browser, it is skipped.
- If the hub cannot be reached when you log in, the question waits for a
  later login rather than asking about an area it cannot check. The same
  happens when another SysOp is answering the same question at that moment.

The areas seen on each network are kept in `data/v3net_seen_areas.json`, and
each SysOp's **No** answers in `data/v3net_declined_areas.json`. The
question's wording is the `v3netNewAreaNotice` string (`%s` is the network,
then the area name), and the reminder after **No** is `v3netNewAreaDeclined`
(`%s` is the area tag).

### Area Auto-Creation

When a leaf syncs the NAL from the hub, any network areas you are subscribed to that do not yet have a local message area are created automatically. The auto-created areas:

- Get the next available area ID and position
- Use the network area tag as the local tag
- Are placed in the default conference (ID 0)
- Have their `network` field set to the V3Net network name
- Are flagged as newscan defaults unless the subscription's **Newscan
  Default** is `N` (`autoJoinAreas: false`)

## How Messages Flow

### Outbound (local user posts)

1. User writes a message in a V3Net area via the message editor (or a sysop
   posts a file with `helper post`)
2. Message is saved to the local JAM base, marked as not yet sent
3. A tearline and origin line are appended to the local copy (by the BBS;
   a `helper post` copy has none locally, but remote nodes add them)
4. The BBS's outbound sweep sends the message to the hub via signed HTTP POST
   and marks the local copy sent ("V3NET SENT" in the reader)
5. The hub stores it and fans it out to all subscribed nodes via SSE events

The sweep runs straight away after a post from the BBS and once a minute
otherwise. If the hub can't be reached, the message stays unsent and goes out
on a later sweep, so a post made while the hub is down is not lost. Posts
written by `helper post` go out on the next minute's sweep, so the BBS has to
be running. Messages that were already in an area when it was bound to a
network are never sent, and a message that arrived from the network is never
sent back.

An area counts as a V3Net area for sending if its type is `v3net` or if a
subscription in `v3net.json` lists its tag under `boards`. That holds even
while V3Net is switched off: a post made then is kept waiting and goes out
once V3Net is enabled and the network connects, so turning V3Net off for a
while does not lose anything.

### Inbound (remote message arrives)

1. The leaf client receives a message via polling or SSE notification
2. The dedup index is checked — if the UUID has been seen, the message is skipped
3. The message (with its remote tearline/origin) is written to the local JAM base
4. The UUID is recorded in the dedup index

## Posting from the Command Line

`helper post` writes a text file, an ANSI (`.ANS`) file or standard input
into a message area without logging in — for network ads, bulletins and
scripted announcements. In a V3Net area it writes the post exactly as the BBS
editor would and leaves it for the running BBS's outbound sweep:

```bash
helper post --area fel.general --file bulletin.ans --subj "[ANSI] Weekly Bulletin"
```

After posting it tells you what happens next:

```
  It waits until V3Net is enabled and this area's network is connected; then the
  running BBS tries to send it to the hub every minute until the hub accepts it.
```

`--private` is refused in a V3Net area, since V3Net never sends private
posts and the message would sit unsent in a shared area. The full flag list
is in the [Technical Reference](v3net/message-areas.md#technical-reference)
below.

## Area Proposals

Users and sysops can propose new areas for a network. See the [NAL documentation](v3net/nal.md) for details on:

- Proposing new areas
- Access modes (open, approval, closed)
- Coordinator and area manager roles

## Troubleshooting

| Symptom | What is happening |
|---------|-------------------|
| A post shows `V3NET` but not `V3NET SENT` in the reader | It has not reached the hub yet. The sweep retries every minute; look for `v3net outbound: sending to hub; will retry` in the log. Only the first failure per message is logged at warning level, so a long outage does not flood the log. |
| `v3net outbound: sent but could not mark as sent` | The hub accepted the post but the local flag could not be written. The next sweep sends it again under the same UUID, which the hub drops as a duplicate, and tries the mark again. Nothing is posted twice. |
| `V3Net leaf: message area not found, skipping` | A subscription names a board tag with no local area. Re-subscribe from the area browser, or create the area with that tag. |
| Old messages in an area never went out | Expected. Only posts made after an area was bound to a network are sent. |
| A post from `helper post` never leaves | The BBS must be running with V3Net enabled; `helper` only writes the message. |

## Related Documentation

- [V3Net Configuration](v3net/configuration.md) — the config editor screens
- [Joining FelonyNet](v3net/felonynet.md) — step-by-step guide
- [Network Area List (NAL)](v3net/nal.md) — area management and access control
- [Message Areas](messages/message-areas.md) — general message area configuration
- [Header Placeholders](messages/placeholders.md) — `@I@` (network name) and `@O@` (node ID) codes

---

## Technical Reference

### Message Format

V3Net messages in JAM bases look like standard echomail messages with a few additions:

- A `V3NETUUID` kludge line (hidden from users) stores the message's unique identifier for deduplication
- A tearline (`--- ViSiON/3 vX.Y.Z/Platform`) identifies the posting software
- An origin line (`* Origin: BBS Name (node_id)`) identifies the posting node

### Pending Marker and Wire UUID

A local, non-private post in a V3Net area is written with JAM
`DateProcessed = 0`, the same "still to export" marker the FTN tosser uses.
Imports (a body starting with the `V3NETUUID` kludge) and messages that were
in the base before the area was bound are never marked, so they are never
sent. The sweep stamps `DateProcessed` and the `MSG_SENT` flag once the hub
accepts the post.

Each post's wire UUID is derived from the sending node ID, the network, the
area tag and the post's date, sender, recipient, subject and body (a SHA-256
digest laid out as a version-4 UUID). Every attempt at the same post carries
the same UUID, so a retry after a lost reply or a failed sent mark is dropped
by the hub's dedup index rather than delivered twice.

### `helper post`

```
helper post --area <tag|id> --file <path|-> [flags]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--area` | (required) | Message area tag or ID |
| `--file` | (required) | Text file to post as the body, or `-` for stdin |
| `--subj` | SAUCE title of an `.ANS` file | Subject |
| `--from` | `sysOpName` from `config.json` | Sender |
| `--to` | `All` | Recipient; in a netmail area `"Name@zone:net/node"` |
| `--private` | off | Mark the message private; refused in V3Net and echomail areas |
| `--reply-to` | `0` | Number of the message in the same area this replies to |
| `--format` | from the extension; `txt` for stdin | Body format: `ans`, `asc` or `txt` |
| `--charset` | `auto` | Plain-text input charset: `auto`, `cp437` or `utf8` (`ans` is always stored raw) |
| `--wrap` | `0` | Re-wrap plain text to this many columns (`asc`/`txt` only; `0` keeps lines) |
| `--dry-run` | off | Check and describe the post without writing it |
| `--config` | `configs` | Config directory |
| `--data` | `data` | Data directory |

Exit status is `0` on success, `1` on an error while checking or posting, and
`2` for a usage error.

### Menu Commands

V3Net provides several menu screens accessible to sysops and users:

| Menu Command | Description |
|-------------|-------------|
| `V3NETSTATUS` | Shows V3Net connection status, node ID, and subscribed networks |
| `V3NETAREAS` | Browse and manage area subscriptions from the NAL |
| `V3NETPROPOSE` | Submit a new area proposal to the network coordinator |
| `V3NETREGISTRY` | Browse the public network registry to discover available networks |
| `V3NETCOORDINATOR` | Coordinator panel for approving or rejecting area proposals and changing area managers (network coordinator only) |
| `V3NETACCESSREQUESTS` | Approve or deny subscription requests for the areas this node manages (area manager only) |

These commands are configured in your menu `.CFG` files. The default V3Net
menu is provided in `menus/v3/cfg/V3NETM.CFG`, reached from the Admin menu
(**3** in `ADMIN.CFG`). All of its commands are `S255` (sysop only) as shipped.

### Hub Signatures

This section is for anyone writing another V3Net implementation. A hub
signs what it sends a leaf with its own Ed25519 node key, the key its
`/info` page serves as `hub_pubkey_b64` and leaves pin. Signatures are
unpadded base64url (RFC 4648 section 5, no `=`), like the request's
`X-V3Net-Signature`. How a leaf acts on them is in
[Hub signatures](v3net/configuration.md#hub-signatures).

**Request nonce.** With every signed request an updated leaf also sends
`X-V3Net-Nonce`, a fresh random value (16 random bytes as unpadded
base64url). The request signature alone does not tell two requests apart:
`Date` has one-second resolution and Ed25519 signatures are deterministic, so
two identical requests in the same second carry the same signature. The
nonce does, so a reply captured for one cannot be passed off as the reply to
the other. The nonce is not part of the request signature, whose form older
hubs check, and older hubs ignore the header. A hub treats a request with no
`X-V3Net-Nonce`, or one longer than 128 bytes, as having an empty nonce; a
leaf that sent a nonce then fails to verify the reply, which is the safe way
round.

**Responses.** Every response to a request that carries
`X-V3Net-Signature`, refusals included, carries `X-V3Net-Hub-Signature`, the
signature over these bytes:

```
V3NET-RESP-SIG-v1\n{request signature}\n{nonce}\n{status}\n{body hash}
```

- `V3NET-RESP-SIG-v1` is literal ASCII and each `\n` is a line feed
  (`0x0A`); there is no newline at the end. The prefix keeps a response
  signature from being taken for any other kind.
- `{request signature}` is the request's `X-V3Net-Signature` header exactly
  as sent, which covers the request's method, path, `Date` and body.
- `{nonce}` is the request's `X-V3Net-Nonce` header exactly as sent, or
  empty (the line is then blank) for a request without one.
- `{status}` is the HTTP status code in decimal, such as `200` or `401`.
- `{body hash}` is the SHA-256 of the response body as sent, in lowercase
  hex. Other headers, such as `X-V3Net-Has-More`, are not covered.

**Event stream.** The `{network}/events` response is signed as above with
an empty body (its body hash is
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`). Each
event then carries two extra SSE fields, before `data` so that leaves from
before hub signatures, which act on the `data` line and skip lines they do
not know, still read every event:

```
event: logon
id: 1
sig: {signature}
data: {"handle":"Sysop","node":"21fe31dfa154a261","timestamp":"2026-10-29T12:00:00Z"}

```

`id` is the event's sequence number in this stream, starting at 1 and
rising by one with every event, pings included. `sig` is the signature over:

```
V3NET-EVENT-SIG-v1\n{network}\n{request signature}\n{nonce}\n{id}\n{event}\n{data}
```

where `{request signature}` and `{nonce}` are the `X-V3Net-Signature` and
`X-V3Net-Nonce` of the request that opened the stream (the nonce empty if it
had none), `{id}` the decimal sequence number, `{event}` the event
type and `{data}` the `data` line's JSON exactly as sent. A leaf accepts an
event only if its `id` is one more than the last and its `sig` verifies, so
an event cannot be repeated, reordered, removed from between others, or
moved into another stream or network. A hub that cannot sign an event ends
the stream rather than skip a number.

A leaf buffers an event until the blank line that ends it, so it caps each
event at 16 lines and 1 MB of field text and drops a stream that sends more,
signed or not. The hub's events are four lines each.

A hub that predates hub signatures sends neither the header nor the extra
fields, and a leaf with no hub key pinned cannot check either.

The tests `TestResponseSignedPayloadFormat`, `TestNonce` and `TestEventSignature` in
`internal/v3net/protocol/hubsig_test.go` check these payloads against the
code.

### Wire Details

- **Transport**: JSON over HTTP. A leaf makes outbound requests only; the hub
  listens on `hub.host:hub.port` (default port `8765`).
- **Authentication**: every leaf request other than the first subscribe is
  signed with the node's Ed25519 key over the method, path, `Date` header and
  body hash, sent in the `X-V3Net-Node-ID` and `X-V3Net-Signature` headers.
- **Endpoints** (under `/v3net/v1/`): `networks`, `subscribe`,
  `{network}/info`, `{network}/nal`, `{network}/messages` (GET to poll, POST
  to send), `{network}/events` (the SSE stream), `{network}/presence`, the
  `chat/rooms/...` calls, and the area, proposal, access-request, manager and
  node-management calls used by the sysop screens.
- **SSE events**: `ping`, `logon`, `logoff`, `new_message`, `nal_updated`,
  `area_proposed`, `area_access_requested`, `proposal_rejected`,
  `subscription_denied`, and the `chat_message`, `chat_private`,
  `chat_join`, `chat_leave` and `chat_topic` chat events. The leaf reconnects
  with backoff when the stream drops (`leaf: SSE reconnecting` in the log)
  and re-fetches the NAL on reconnect.
- **Dedup**: `data/v3net_dedup.sqlite` records every UUID written to a local
  base; a message whose UUID is already there is skipped.

### Message Signatures

This section is for anyone writing another V3Net implementation. A message
is signed by the node that wrote it, with the same Ed25519 key it subscribes
with, so any reader can check the author without trusting the hub. Two wire
fields carry the signature:

| Field | Meaning |
|-------|---------|
| `origin_pubkey_b64` | The author node's Ed25519 public key, 32 bytes in standard base64 |
| `signature_b64` | The Ed25519 signature over the signed payload below, 64 bytes in standard base64 |

A legacy unsigned message has neither field. A message with either one claims
a signature and must verify; it is never treated as unsigned.

**Signed payload.** The signature covers these bytes, with no trailing
newline:

1. The ASCII prefix `V3NET-MSG-SIG-v1` followed by one line feed (`0x0A`).
   The prefix keeps a message signature from being replayed as a NAL or
   request signature; the version changes if the covered fields ever do.
2. A JSON object with exactly these 17 keys, in this (sorted) order:
   `area_tag`, `attributes`, `body`, `date_utc`, `from`, `msg_uuid`,
   `network`, `origin`, `origin_board`, `origin_node`, `origin_pubkey_b64`,
   `parent_uuid`, `subject`, `tearline`, `thread_uuid`, `to`, `v3net`.

The JSON is compact: no spaces or newlines between tokens. Every key is
always present. `attributes` is a plain integer. `parent_uuid` is `null` when
the message has no parent, never left out or `""`. `tearline` and `origin`
are `""` when empty, even though the wire message leaves them out then.
`origin_pubkey_b64` is included; `signature_b64` is not. `kludges` are not
signed: they are free-form metadata a relay may add to (the
`v3net_truncated` marker, for one).

Strings are escaped with HTML escaping off, as Go's `encoding/json` does
with `SetEscapeHTML(false)`:

- `"` and `\` become `\"` and `\\`.
- Backspace, form feed, line feed, carriage return and tab become `\b`,
  `\f`, `\n`, `\r` and `\t`; any other byte below `0x20` becomes `\u00XX`
  with lowercase hex.
- U+2028 and U+2029 become `\u2028` and `\u2029`.
- Everything else, including `<`, `>`, `&`, `/` and all other non-ASCII
  text, is written as raw UTF-8.

**Before signing**, the author replaces each byte of invalid UTF-8 in the
signed text fields with U+FFFD (one per byte, as Go's JSON encoder does when
the message is sent), so the signature covers the text readers decode. It
also cuts a body over 32,768 bytes down before signing, since any later change
breaks the signature. `origin_node` must be the signing key's node ID.

**Verifying.** A reader checks that `origin_pubkey_b64` decodes to 32 bytes,
that `origin_node` is that key's node ID (the first 8 bytes of SHA-256 of the
raw key, as lowercase hex), and that `signature_b64` decodes to 64 bytes and
verifies over the payload rebuilt from the message as received. The hub also
checks that the key is the one the posting node subscribed with.

#### Worked example

The key is the secret key from RFC 8032 section 7.1, test 1. Ed25519
signatures are deterministic, so this key and message always give the same
signature.

| | |
|---|---|
| Seed (hex) | `9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60` |
| Public key (`origin_pubkey_b64`) | `11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=` |
| Node ID (`origin_node`) | `21fe31dfa154a261` |

The message, as the author has it before signing. It has no parent, and its
kludge is not signed:

```json
{
  "v3net": "1.0",
  "network": "felonynet",
  "area_tag": "fel.general",
  "msg_uuid": "6f9619ff-8b86-4d01-b42d-00cf4fc964ff",
  "thread_uuid": "6f9619ff-8b86-4d01-b42d-00cf4fc964ff",
  "parent_uuid": null,
  "origin_node": "21fe31dfa154a261",
  "origin_board": "General",
  "from": "Sysop",
  "to": "All",
  "subject": "Q&A <tonight>",
  "date_utc": "2026-10-29T12:00:00Z",
  "body": "Café opens at 8.\r\nSee you there!",
  "tearline": "--- ViSiON/3",
  "origin": "Example BBS",
  "attributes": 0,
  "kludges": {"chrs": "UTF-8"}
}
```

The signed payload is these 521 bytes (UTF-8), two lines with no newline at
the end. SHA-256 of the payload is
`3d4746d10764ef86e059b25669c591e01e62a7b4911f17dccfdbf5acfcba5c60`.

```
V3NET-MSG-SIG-v1
{"area_tag":"fel.general","attributes":0,"body":"Café opens at 8.\r\nSee you there!","date_utc":"2026-10-29T12:00:00Z","from":"Sysop","msg_uuid":"6f9619ff-8b86-4d01-b42d-00cf4fc964ff","network":"felonynet","origin":"Example BBS","origin_board":"General","origin_node":"21fe31dfa154a261","origin_pubkey_b64":"11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=","parent_uuid":null,"subject":"Q&A <tonight>","tearline":"--- ViSiON/3","thread_uuid":"6f9619ff-8b86-4d01-b42d-00cf4fc964ff","to":"All","v3net":"1.0"}
```

The `<`, `>` and `&` in the subject and the `é` in the body are written as
they are, and the body's carriage return and line feed as `\r\n`. The
signature (`signature_b64`) is:

```
buk/YQJzkqsR2N2RMdVMjKBgpXP6MYvDIFEXjQQ+NnlXYPS7+J5BrJF3YBWGmu/CG4DtSinfXoic8U1oZz6MCA==
```

The test `TestMessageSignatureDocExample` in
`internal/v3net/protocol/signature_example_test.go` checks these values
against the code.
