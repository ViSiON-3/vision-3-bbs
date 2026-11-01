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
| Message dedup | N/A | MSGID CRC | SQLite index of origin node + UUID |
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
2. The dedup index is checked — if this message (its origin node and UUID)
   has been seen, it is skipped
3. The message (with its remote tearline/origin) is written to the local JAM base
4. The message is recorded in the dedup index

A hub refuses a post whose subject holds a control character, with a 422. A
message a hub already holds with one is still imported, with each such character
turned into a space; its signature is checked against the original first. See
[Message Format](#message-format) for other header fields.

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

- A `V3NETUUID` kludge line (hidden from users) stores the message's UUID
- A tearline (`--- ViSiON/3 vX.Y.Z/Platform`) identifies the posting software
- An origin line (`* Origin: BBS Name (node_id)`) identifies the posting node

From, To and Subject never hold control characters. On every kind of area they
are cleaned when a message is stored and again when it is read or exported, each
control character (C0, DEL and C1) becoming a space, so messages stored by older
versions are safe to show too. The stored copy of an older message is left as it
was. Message bodies are not affected, since they may carry ANSI art.

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

### Message Identity

The sending node picks a message's `msg_uuid`, so the UUID alone does not
identify a message: another node can post under the same one, by accident
or on purpose. A message is identified by **(network, `origin_node`,
`msg_uuid`)**. The hub only accepts a post whose `origin_node` is the node
that sent it, so no node can take over another node's UUIDs.

- **Hub**: two posts with the same UUID from different nodes are both stored
  and both delivered. A node posting its own UUID again is a duplicate: the
  hub answers 200 and keeps the first copy.
- **Leaf**: the dedup index records each imported message by network, origin
  node and UUID. Entries made before this rule have no origin node and match
  that UUID from any node, so nothing already imported comes in again.

### Polling Messages

A leaf polls `GET /v3net/v1/{network}/messages` with these query
parameters:

| Parameter | Meaning |
|-----------|---------|
| `since` | `msg_uuid` of the last message the leaf has. Empty or `0` starts from the beginning. |
| `since_origin` | `origin_node` of that message. Optional. |
| `identity` | `origin` to identify messages by origin node and UUID. A current leaf sends it on every poll, the first one with no cursor included. |
| `limit` | Page size, 1 to 500 (default 100). |
| `format` | `page` to get the page as an object with `has_more` in it (below). A current leaf sends it on every poll. |

The hub returns the messages in its own order that come after the cursor, in
the leaf's subscribed areas, oldest first. With `format=page` the body is

```json
{"messages":[...],"has_more":true}
```

and `has_more: true` means another page follows. The hub's response
signature covers the body, so `has_more` cannot be changed on the way. Without
`format=page` the body is the bare array of messages and another page is
announced by the header `X-V3Net-Has-More: true`, which no signature covers. A
hub from before `format=page` ignores the parameter and answers that way, so
a leaf reads either form. `format=page` is part of the signed request and the
signature covers the body, so nothing on the way can turn a current hub's
object into an array to fall back on the header. Either way the leaf asks
again with the last message of the page as the cursor, skipped messages
included.

- With `since_origin`, the cursor is exactly that message in the network.
- Without it (a leaf from before the identity rule), or when the network has
  no message with that origin and UUID, the cursor is the earliest message
  in the network with that UUID. That may send a few messages again, which
  the leaf drops as duplicates, but never skips one.
- Only a UUID the network does not hold at all starts from the beginning.

Without `identity=origin` the hub treats the leaf as one from before the
identity rule, which dedups on the UUID alone: it serves only the earliest
message in the network with each UUID and leaves out later copies, which
such a leaf would drop anyway. Serving them could stall it for good. Once
another node posted copies of a page's worth of UUIDs the leaf already has,
every page would hold only copies, and its cursor, still on the earliest
copy, would never move past them.

A hub from before the identity rule ignores `since_origin` and `identity`.

When a post is stored, the hub sends a `new_message` event on the network's
SSE stream with `network`, `msg_uuid`, `origin_node`, `from` and `subject`.
`origin_node` was added with the identity rule; a leaf that predates it
ignores the field, and a leaf should not rely on it from an older hub.

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

### Coordinator Handovers

This section is for anyone writing another V3Net implementation. A
coordinator handover moves a network from one coordinator key to another;
how leaves act on it is in
[Signed Handovers](v3net/nal.md#signed-handovers). It is a JSON object:

| Field | Meaning |
|-------|---------|
| `network` | The network handed over |
| `old_key` | The outgoing coordinator's Ed25519 public key, 32 bytes in canonical standard base64 (44 characters with one `=`, no line breaks, unused low bits zero) |
| `new_key` | The incoming coordinator's key, in the same form; never the same as `old_key` |
| `issued` | When it was signed, RFC 3339 in UTC, such as `2026-10-29T12:00:00Z` |
| `signature_b64` | The Ed25519 signature by `old_key` over the signed payload below, 64 bytes in standard base64 |

**Signed payload.** The signature covers these bytes, with no trailing
newline:

1. The ASCII prefix `V3NET-COORD-HANDOVER-v1` followed by one line feed
   (`0x0A`), which keeps a handover signature from being taken for any other
   kind.
2. A compact JSON object with exactly the keys `issued`, `network`,
   `new_key` and `old_key`, in that (sorted) order, strings escaped as for
   [message signatures](#message-signatures) (HTML escaping off).

For example:

```
V3NET-COORD-HANDOVER-v1
{"issued":"2026-10-29T12:00:00Z","network":"felonynet","new_key":"bmV3","old_key":"b2xk"}
```

(The keys are shortened here; real ones are 44 characters.)

Keys are compared as strings, along the chain and on the hub, so each has
exactly one accepted spelling: the one encoding the decoded 32 bytes gives
back. A handover with a key spelled any other way, even one that decodes
to the same bytes, does not verify.

**Publishing.** A subscriber sends the handover as the body of a signed
`POST /v3net/v1/{network}/nal/handover`. The hub accepts it only if it
verifies, names the network in the path, and `old_key` is the coordinator
key in force: the key of the stored NAL, followed along any handovers the
hub already holds. It refuses a `new_key` the network has already used, so
the stored handovers never loop (`409`). Sending one it already holds, or
one with the same `old_key` and `new_key` signed at another time, succeeds
without storing it twice.

A hub may already run `new_key` when the handover arrives, if it was
restarted with the new key first. The rules above are unchanged, so the
handover must still be signed by the key of the stored NAL; the hub then
re-signs the NAL with its key at once and sends `nal_updated`. If it holds
the handover but cannot store the re-signed NAL, it answers `500` saying
so; sending the handover again retries the re-sign. While a hub
runs a key that neither signed the stored NAL nor appears on its
handovers, the only handover it accepts is one to its own key (`409`
otherwise), since it could never sign for any other.

Such a hub may also have re-signed the NAL with its own key before the
handover arrived: a hub-side change to a NAL never handed over is signed
with the hub's key, whatever key signed it before. When that happens with
no handover held, the hub adds the key that signed the NAL before to the
network's previous signers: an ordered list, oldest first, of at most 8
keys, kept across restarts (a key recorded again moves to the end). With no
handover held, it then also accepts a chain of handovers that ends at its
own key and whose every `old_key` is a previous signer or, when the NAL is
not yet signed by the hub's key, the NAL's own key, in the order they
signed the NAL (so the last link then starts at the NAL's key). A hub whose
own key is on the list (it went back to a key it ran before) accepts no
such chain. A previous signer counts as a key the network already used,
so outside such a chain any handover to one is refused (`409`), even when
the rules above would otherwise take it. A NAL already signed by the hub's key is served with the
chain, changed only as below; any other is re-signed at once, as for a
late handover. Either way the hub sends `nal_updated`. Once any handover
is held, the previous signers no longer count.

The body of the post is one handover, or a JSON array of at most 8 that
forms a chain: each starts at the `new_key` of the one before, and no key
appears twice (`400` otherwise). Every link must verify (`422`). The hub
checks the chain as a whole against the previous signers, or else link by
link under the rules above, as if each were posted once the ones before it
were held, and stores all of it or none (`409`). Links it already holds
are skipped; a chain of nothing else is a repeat. A late chain must be
posted in one request: posted link by link, the last link would be held
first, after which the previous signers no longer count and the earlier
links are refused. Hubs that predate chains answer `400` to an array.

When the hub signs the NAL for a key the network was handed over to (at
startup, on a late handover, or on a handover from the previous signer),
each area whose `manager_node_id` or `manager_pubkey_b64` names the node
of a key it was handed over from gets the hub's node ID and key as its
manager, in the same signed NAL update; the hub logs each move. Areas
other nodes manage keep their managers. A re-sign without a handover moves
nothing. The keys handed over from are worked out from what the hub
stores (the handovers from the NAL's key to the hub's, and the previous
signers a late chain bridged to the NAL's key), so a re-sign that failed
or was cut short by a restart moves the same areas when it is retried.
The same update clears the previous signers, so their areas are never
moved twice, over a manager chosen since. A hub-side NAL change made while
the NAL is still another key's and the handovers lead to the hub's (the
re-sign failed) counts as that update and moves the same areas, except an
area whose manager the change itself sets, compared with the stored NAL (a
new manager, or a new area): that area keeps the manager the change gave it,
even the node of a key handed over from. Any NAL
stored under a new key while handovers are held also clears the previous
signers, a NAL posted by the coordinator included; a posted NAL is signed
as it came, so nothing moves.

A posted NAL (`POST /v3net/v1/{network}/nal`) must always come from the
node of the coordinator key in force and be signed by that key, so once
the hub holds a handover only the new key may post. The hub's own NAL
changes (proposal approvals, access and manager edits) are refused with
`409` while it runs with a key other than the one in force, since signing
with the old key would undo the handover. A hub started with the new key
re-signs the stored NAL with it, and its changes are signed as before.

**Serving.** `GET /v3net/v1/{network}/nal` carries the handovers in the
NAL's `coordinator_handovers` array, oldest first, at most 32 (the most
recent). The array is left out of the NAL's canonical form, so leaves that
predate handovers verify the NAL as before and ignore the field.

**Signer history.** `GET /v3net/v1/{network}/nal/signers` tells a tool
what chain the hub needs. It is a signed request from any subscribed node,
and the reply is a [hub-signed response](#hub-signatures); act on it only
once that signature verifies. It holds public keys only:

| Field | Meaning |
|-------|---------|
| `network` | The network |
| `nal_key` | The key that signs the stored NAL |
| `key_in_force` | `nal_key` followed along `handovers` |
| `previous_signers` | The previous signers described above, oldest first; may be empty, and may include `nal_key` |
| `handovers` | Every handover held, oldest first, as `old_key` and `new_key`; may be empty |

A hub that predates this endpoint answers a subscribed node `404`. Verify
the reply against a key you already trust for the hub, not one read from
the unsigned `/info`.

**Following.** A leaf pinned to key P that is offered a NAL signed by key N
looks for a chain of handovers from P to N, each one for this network,
verifying under its own `old_key`, and starting where the one before ended.
It follows at most 8 steps, never visits a key twice, and ignores a list of
more than 32. With such a chain it moves its pin to N; without one it
rejects the NAL. A leaf moves its pinned hub key the same way when `/info`
serves the key that signs the NAL.

The tests `TestCoordHandoverSignedPayloadFormat` in
`internal/v3net/protocol/handover_test.go` and `TestFollowHandoversLimits`
in `internal/v3net/nal/handover_test.go` check this against the code.

### Wire Details

- **Transport**: JSON over HTTP. A leaf makes outbound requests only; the hub
  listens on `hub.host:hub.port` (default port `8765`).
- **Authentication**: every leaf request other than the first subscribe is
  signed with the node's Ed25519 key over the method, path, `Date` header and
  body hash, sent in the `X-V3Net-Node-ID` and `X-V3Net-Signature` headers.
- **Endpoints** (under `/v3net/v1/`): `networks`, `subscribe`,
  `{network}/info`, `{network}/nal` (and `{network}/nal/handover` and
  `{network}/nal/signers`),
  `{network}/messages` (GET to poll, POST
  to send), `{network}/events` (the SSE stream), `{network}/presence`, the
  `chat/rooms/...` calls, and the area, proposal, access-request, manager and
  node-management calls used by the sysop screens.
- **SSE events**: `ping`, `logon`, `logoff`, `new_message`, `nal_updated`,
  `area_proposed`, `area_access_requested`, `proposal_rejected`,
  `subscription_denied`, and the `chat_message`, `chat_private`,
  `chat_join`, `chat_leave` and `chat_topic` chat events. The leaf reconnects
  with backoff when the stream drops (`leaf: SSE reconnecting` in the log)
  and re-fetches the NAL on reconnect.
- **Dedup**: `data/v3net_dedup.sqlite` records every message written to a
  local base by network, origin node and UUID; a message already there is
  skipped (see [Message Identity](#message-identity)).

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
  hex. Headers are not covered. The one header a leaf acts on,
  `X-V3Net-Has-More` on a message page, is replaced by `has_more` in the
  body when the leaf polls with `format=page`
  (see [Polling Messages](#polling-messages)).

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
fields, and a leaf with no hub key pinned cannot check either. Since
removing the header makes a reply look like one from such a hub, a leaf
refuses unsigned replies and streams from a hub it has seen sign before, or
whose sysop requires hub signatures
([Refusing unsigned replies](v3net/configuration.md#refusing-unsigned-replies)).

#### Hub signature example

The hub's key is the one from the
[message signature example](#worked-example) (RFC 8032 section 7.1,
test 1), and the leaf's is the secret key from test 2 of the same section.
Ed25519 signatures are deterministic, so these values never change.

| | Hub | Leaf |
|---|---|---|
| Seed (hex) | `9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60` | `4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb` |
| Public key | `11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=` | `PUAXw+hDiVqStwqnTRt+vJyYLM8uxJaMwM1V8Sr0Zgw=` |
| Node ID | `21fe31dfa154a261` | `39f713d0a644253f` |

**A reply to a poll.** The leaf polls `felonynet` from the start:

| | |
|---|---|
| Request | `GET /v3net/v1/felonynet/messages?since=&limit=100&identity=origin&format=page` |
| `Date` | `Thu, 29 Oct 2026 12:00:00 GMT` |
| `X-V3Net-Signature` | `ZGIM6oH1yXiQb76W2fM1Oskcq9quIqOfcEz87Ts45d3D1_y-PA9kTApaeS-dfN2d4FMMbVlL8Itr9YWJ5RbLAw` |
| `X-V3Net-Nonce` | `AAECAwQFBgcICQoLDA0ODw` |

The request has no body, so the request signature covers the SHA-256 of
nothing, `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
The hub has no messages for it and answers `200` with this body:

```
{"messages":[],"has_more":false}
```

Its SHA-256 is
`f4528e8588f80a1c2a47d6468ec1ce76305c0780fccee566e4d2e13012026a65`, so the
signed payload is these five lines, with no newline at the end:

```
V3NET-RESP-SIG-v1
ZGIM6oH1yXiQb76W2fM1Oskcq9quIqOfcEz87Ts45d3D1_y-PA9kTApaeS-dfN2d4FMMbVlL8Itr9YWJ5RbLAw
AAECAwQFBgcICQoLDA0ODw
200
f4528e8588f80a1c2a47d6468ec1ce76305c0780fccee566e4d2e13012026a65
```

and the hub sends it with:

```
X-V3Net-Hub-Signature: Q3GGVka76e7yLCKTmyvRnwLePhlmTNoyusbsiV1UKLd8wnf8HA5CYCe6h2xE1ZN5cjoQ2L9iWGuII-bQUvklAA
```

**An event.** The leaf opens the event stream:

| | |
|---|---|
| Request | `GET /v3net/v1/felonynet/events` |
| `Date` | `Thu, 29 Oct 2026 12:00:05 GMT` |
| `X-V3Net-Signature` | `5CtxwqDTTWItRJcSnHzbftIFXtSKt0NGKhaOqmbqn2R0OuYfSM91EwZiBZaAjXoU7VM6piLzZulxtkXQvFHEDg` |
| `X-V3Net-Nonce` | `EBESExQVFhcYGRobHB0eHw` |

The first event on it is a logon on the leaf's board. Its signed payload is
these seven lines, with no newline at the end:

```
V3NET-EVENT-SIG-v1
felonynet
5CtxwqDTTWItRJcSnHzbftIFXtSKt0NGKhaOqmbqn2R0OuYfSM91EwZiBZaAjXoU7VM6piLzZulxtkXQvFHEDg
EBESExQVFhcYGRobHB0eHw
1
logon
{"handle":"Sysop","node":"39f713d0a644253f","timestamp":"2026-10-29T12:00:07Z"}
```

and the hub sends it as:

```
event: logon
id: 1
sig: 6KGM84CZziZt-xRrnWzvbgMaxJdJOFCMNLEHUJ4qnPRiY1uJDDJPzOH9SXrhP9UFO5svy6I0AFVktMkVxp9vAg
data: {"handle":"Sysop","node":"39f713d0a644253f","timestamp":"2026-10-29T12:00:07Z"}

```

The tests `TestResponseSignedPayloadFormat`, `TestNonce` and
`TestEventSignature` in `internal/v3net/protocol/hubsig_test.go` check these
payloads against the code, and `TestHubSignatureDocExample` in
`internal/v3net/protocol/hubsig_example_test.go` checks the example.
