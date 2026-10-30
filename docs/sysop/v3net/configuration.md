# V3Net Configuration

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net is ViSiON/3's own inter-BBS network. A **leaf** node (your BBS) talks
to a **hub** over plain HTTP: it posts local messages to the hub, and the hub
pushes everyone else's posts back with Server-Sent Events (SSE) and a periodic
poll. Every message and every area list is signed with your node's Ed25519
key, so there are no packet passwords, no mailer, no nodelist and no open
inbound ports on a leaf. You would want it to carry networked message areas
and real-time chat between boards with a few screens in the config editor
and a restart.

V3Net settings live in two places in the TUI config editor (`./config`):

- **1 — System Setup → 2. Server Setup** — master enable/disable, file paths,
  and hub server settings
- **6 — ViSiON/3 Networking (V3Net)** — subscriptions (leaf setup), hosted
  networks (hub setup), and node identity

> For a step-by-step guide to joining FelonyNet, see [Joining FelonyNet](v3net/felonynet.md).
> To edit `configs/v3net.json` by hand, see the
> [Manual Configuration Reference](v3net/manual-config.md).

---

## System Setup — Server Setup

```
./config  →  1 — System Setup  →  2. Server Setup
```

```
┌──────────────────────────────────────────────────────────────────────┐
│                           Server Setup                               │
│                                                                      │
│  V3Net           : Y                                                 │
│  Keystore Path   : data/v3net.key                                    │
│  Dedup DB Path   : data/v3net_dedup.sqlite                           │
│  Registry URL    : https://raw.githubusercontent.com/...             │
│                                                                      │
│  V3Net Hub       : N                                                 │
│  Hub Host        :                                                   │
│  Hub Port        : 8765                                              │
│  Hub Data Dir    :                                                   │
│  Auto Approve    : N                                                 │
│  Auto Approve Areas : N                                              │
│  Signed Subscribe : N                                                │
│  Signed Messages : N                                                 │
│                                                                      │
│                          Screen 2 of 8                               │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  PgUp/PgDn - Screens  |  ESC - Return
```

| Field | Description |
|-------|-------------|
| **V3Net** | Master on/off switch (`Y`/`N`). Must be `Y` for V3Net to start. |
| **Keystore Path** | Path to the Ed25519 keypair file. Auto-generated on first start if absent. Recommended: `data/v3net.key` |
| **Dedup DB Path** | Path to the SQLite message deduplication database. Recommended: `data/v3net_dedup.sqlite` |
| **Registry URL** | Central registry URL for network discovery. Default: `https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json` |
| **V3Net Hub** | Enable the built-in hub server (`Y`/`N`). Only needed if you are hosting your own network. |
| **Hub Host** | Listen address for the hub. Blank means all interfaces. Set to `127.0.0.1` if behind a reverse proxy. IPv6 literals work with or without brackets: `::1` and `[::1]` both listen on IPv6 loopback. |
| **Hub Port** | Listen port for the hub HTTP server. Default: `8765`. |
| **Hub Data Dir** | Directory for hub database and NAL files. Recommended: `data/v3net_hub` |
| **Auto Approve** | When `Y`, new leaf subscriptions are approved automatically. |
| **Auto Approve Areas** | When `Y`, area proposals are added to the network straight away instead of waiting in the Coordinator Panel. If `autoApproveAreas` has never been set, it follows **Auto Approve**, which used to cover proposals too. Changing **Auto Approve** in the editor saves the area setting as it stood, so it no longer follows. |
| **Signed Subscribe** | Default `N` preserves compatibility with older leaves. Set `Y` to reject unsigned subscriptions after leaves have upgraded. Save and restart the BBS to apply. See [Independent hub and leaf upgrades](v3net/configuration.md#upgrading-hub-and-leaf-nodes-independently). |
| **Signed Messages** | Default `N` accepts unsigned message posts from older leaves. Set `Y` to reject them once every leaf signs its messages. Save and restart the BBS to apply. See [Requiring signed messages](v3net/configuration.md#requiring-signed-messages). |

Press **S** to save after making changes.

---

## V3Net Networking Menu

```
./config  →  6 — ViSiON/3 Networking (V3Net)
```

```
┌──────────────────────────────────────┐
│    ViSiON/3 Networking (V3Net)       │
│                                      │
│  1. Node Identity                    │
│  2. Subscriptions                    │
│  3. Networks                         │
│  Q. Return                           │
│                                      │
└──────────────────────────────────────┘
Enter - Select  |  ESC/Q - Return
```

| Item | Description |
|------|-------------|
| **Node Identity** | View your node ID and public key; show, export, or recover your seed phrase. |
| **Subscriptions** | Add, edit, and remove leaf connections to remote hubs. Press **I** for the guided setup wizard. |
| **Networks** | Add, edit, and remove hosted network definitions (hub mode only). Press **I** for the hub setup wizard. |

---

## Subscriptions (Joining a Network)

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  2. Subscriptions
```

```
┌──────────────────────────────────────────────────────────────────────┐
│                        V3Net Subscriptions                           │
│   #  Hub URL                          Network        Board           │
│──────────────────────────────────────────────────────────────────────│
│   1  http://felonynet.org:8765       felonynet      fn.general      │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  I - New (Wizard)  |  B - Registry  |  D - Delete  |  S - Save  |  ESC - Return
```

Press **B** to open the **Network Registry** and browse available networks, or
press **I** to open the **Leaf Setup Wizard** manually:

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Leaf Setup — Join a Network                      │
│                                                                      │
│  Registry        : (press Enter to browse available networks)        │
│  Hub URL         : http://felonynet.org:8765                         │
│  Network         : felonynet                                         │
│  Areas           : (none — press Enter to browse)                    │
│  Poll Interval   : 5m                                                │
│  Origin          : My BBS - bbs.example.com                          │
│                                                                      │
│                         S - Save  |  ESC - Cancel                    │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  S - Save  |  ESC - Back
```

| Field | Description |
|-------|-------------|
| **Registry** | Opens the network registry browser. Selecting a network fills in Hub URL and Network automatically. |
| **Hub URL** | Full URL of the hub (e.g. `http://felonynet.org:8765`) |
| **Network** | Network name to subscribe to (e.g. `felonynet`) |
| **Areas** | Press **Enter** to open the area browser and choose which areas to subscribe to |
| **Poll Interval** | How often to check for new messages. Accepts Go durations: `30s`, `5m`, `1h` |
| **Origin** | Origin line appended to outbound messages. Identifies your BBS to readers on other nodes. Blank defaults to BBS name. |

Press **Enter** on **Areas** to browse and subscribe. Press **S** to save.

### Editing an Existing Subscription

Press **Enter** on a subscription in the list to open its edit form. It has
the same Hub URL, Network, Poll Interval and Origin fields as the wizard, plus:

| Field | Description |
|-------|-------------|
| **Boards** | Comma-separated local area tags this subscription feeds (e.g. `fel.general,fel.tech`). Normally filled by the area browser. |
| **Newscan Default** | `Y` (the default) flags areas created for this network as newscan defaults for your users, so new callers see them in their newscan without opting in. |
| **Signed Messages** | Default `N` imports unsigned messages from older nodes. Set `Y` to drop them once every node on the network signs its messages. See [Requiring signed messages](v3net/configuration.md#requiring-signed-messages). |
| **Browse Areas** | Press **Enter** to open the area browser for this hub. |
| **Signed Hub** | Default `N` accepts unsigned replies from a hub that predates hub signing, until the leaf has seen it sign. Set `Y` to refuse them from the start. See [Refusing unsigned replies](v3net/configuration.md#refusing-unsigned-replies). |
| **Hub Signs** | Set to `Y` by the leaf once the hub's signatures check out; unsigned replies from it are refused from then on. Set it to `N` only if the hub really stopped signing. |

---

## Hosting a Hub

To host your own V3Net network:

1. Enable the hub server in **System Setup → Server Setup** (set **V3Net Hub** to `Y`, fill in **Hub Port** and **Hub Data Dir**)
2. Define at least one network via **V3Net Networking → 3. Networks**

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  3. Networks
```

```
Enter - Edit  |  I - New (Wizard)  |  N - Nodes  |  D - Delete  |  S - Save  |  ESC - Return
```

Press **I** to open the **Hub Setup Wizard**:

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Hub Setup — Host a Network                       │
│                                                                      │
│  Network Name    : mynet                                             │
│  Description     : My BBS Network                                    │
│  Listen Port     : 8765                                              │
│  Auto-Approve    : N                                                 │
│  Auto-Approve Areas : N                                              │
│                                                                      │
│  Initial Areas   : (none — press Enter to add)                       │
│                                                                      │
│                         S - Save  |  ESC - Cancel                    │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  S - Save  |  ESC - Back
```

Press **Enter** on **Initial Areas** to add the message areas that will seed
the initial NAL. After saving and restarting, the BBS automatically creates
the data directory, registers the hub, and builds the NAL — no additional
bootstrap steps needed.

If you need transport encryption, place a reverse proxy (nginx, Caddy) with a
valid CA-signed certificate in front of the hub. The hub itself always serves
plain HTTP. See [V3Net Hub TLS](v3net/hub-tls.md).

---

### Managing Subscriber Nodes

From the Networks list, press **N** on a hosted network to open Node
Management. The screen lists every BBS registered with your hub:

- **A** — approve a `pending` node (required when Auto-Approve is off)
- **B** — ban a node (its requests are rejected) or unban it
- **D** — delete a registration entirely; the node may re-subscribe fresh
- **R** — refresh the list

The BBS (and its hub) must be running: the screen talks to the live hub
so changes take effect immediately, with no restart. With **Auto-Approve**
set to `Y`, new nodes activate on registration and approval is never
needed; ban and delete still apply.

---

## Node Identity

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  1. Node Identity
```

```
┌────────────────────────────────────────────────────────────┐
│                    V3Net Node Identity                     │
│                                                            │
│  Node ID:    a3f9e1b2c4d5e6f7                              │
│  Public Key: MCowBQYDK2VdAyEA...                           │
│  Key File:   data/v3net.key                                │
│                                                            │
│  [S] Show recovery seed phrase                             │
│  [E] Export recovery seed phrase to file                   │
│  [R] Recover identity from seed phrase                     │
│                                                            │
└────────────────────────────────────────────────────────────┘
S - Show  |  E - Export  |  R - Recover  |  Q - Return
```

Your node ID is derived from an Ed25519 keypair. It is your permanent identity
on all V3Net networks. **Back up your seed phrase immediately after first startup**
— if you lose both the key file and the seed phrase, your identity is gone permanently.

See [V3Net Key Recovery](v3net/recovery.md) for full details.

---

## Day-to-Day Operation

### Which Changes Need a Restart

- **Subscriptions and area bindings apply live.** Subscribing or unsubscribing
  from the in-BBS area browser, answering the login question about a new area,
  or saving a subscription change in the config editor is picked up by the
  running BBS. Unchanged networks quietly re-subscribe to their hub, and a
  caller mid-chat on that network has the chat stream reconnected.
- **The master switch, hub settings, keystore and dedup paths need a restart.**
  Changing **V3Net**, anything in the hub block, or either file path takes
  effect the next time the BBS starts.

### Checking Your Setup

`vision3 doctor` reads `configs/v3net.json` and reports on it without
starting the board: a file that will not parse, a hub network with a blank or
duplicated name, a leaf whose hub URL is not `http://` or `https://`, a bad
poll interval, a subscription with no boards, a board tag that matches no
local message area, and a `v3net` area whose `network` is not listed. It
also checks that the identity key file is a valid Ed25519 keypair and that
nobody but the BBS user can read it. It never generates a replacement key.
See [vision3 doctor](configuration/configuration.md#doctor).

### Troubleshooting

| Symptom | What to check |
|---------|---------------|
| `v3net.json not found, V3Net disabled` in the log | The BBS could not find `configs/v3net.json`. Save once from the config editor to create it. |
| `V3Net leaf: message area not found, skipping` | A board tag in a subscription matches no local area. Open the subscription, press **Enter** on **Browse Areas**, and re-subscribe; `vision3 doctor` lists these too. |
| `leaf: poll failed` | The hub is unreachable or rejecting you. Check the hub URL and that the hub is up. If the error says the sync is paused, see the next row. |
| `leaf: pausing sync with the hub` | The hub's key, or the key it signs with, is not the pinned **Hub Key**. Polling, the event stream and sending stop for that network until you accept the key; see [hub signatures](v3net/configuration.md#hub-signatures). |
| `leaf: hub does not sign its responses (older hub)` | Logged once. The hub predates hub signatures; the leaf works with it as before, unverified. |
| `leaf: subscribe failed, retrying` with `subscription status "pending"` | The hub has not approved your node. The leaf retries on its own; ask the hub sysop to approve you in Node Management. |
| `V3Net is disabled in server configuration` on a V3Net screen in the BBS | The master switch is `N`. Set **V3Net** to `Y` in Server Setup and restart. |
| `v3net hub: rejected unsigned message` on your hub | **Signed Messages** is `Y` and an older leaf posted without a signature. The leaf gets HTTP 401 and must upgrade; see [requiring signed messages](v3net/configuration.md#requiring-signed-messages). |
| `v3net hub: rejected message with bad signature` on your hub | A post's author signature did not verify, or its `origin_node` is not derived from its key. Always rejected (HTTP 403). |
| `leaf: unsigned message from hub, dropping` | **Signed Messages** is `Y` on this subscription and the hub relayed a message without a signature, from a node that has not upgraded. Set it back to `N` if that node's messages should still arrive; see [requiring signed messages](v3net/configuration.md#on-a-leaf). |
| `leaf: message failed author signature check, dropping` | The hub relayed a message whose author signature does not verify: it was changed after the author signed it, or claims another node's identity. The leaf skips it and carries on. It is not marked as seen, so a genuine copy of the message can still arrive: if it was the newest message, the leaf fetches it again on each poll until a newer message arrives. Each refused message is logged at this level once, then at debug level. If it keeps happening, raise it with the hub sysop. |
| `v3net hub: unsigned subscribe` on your hub | An older leaf registered without a signature. Harmless in compatibility mode; see [upgrading nodes independently](v3net/configuration.md#upgrading-hub-and-leaf-nodes-independently). |
| `NEW IDENTITY CREATED` on startup | No key file was found, so a new identity was made. If you meant to keep the old one, stop, restore the key or recover from the seed phrase, and restart. |

---

## Upgrading hub and leaf nodes independently

Existing hubs can be rebuilt and restarted before their leaves upgrade.
**Signed Subscribe defaults to N** (`hub.requireSignedSubscribe: false`),
including when the setting is absent from an older configuration. There is
no automatic deadline or switch to strict mode.

1. Stop the BBS and back up its configuration, node key (normally
   `data/v3net.key`), and configured V3Net data directory.
2. Update, run `./build.sh`, and restart using the same configuration, key,
   and data. Leave **Signed Subscribe** at **N** in the config editor's
   **System Setup → Server Setup** screen.
3. Leaf sysops can update, rebuild, and restart independently, keeping their
   existing configuration, node key, and data. Updated BBS binaries and config
   editors sign subscriptions automatically. No re-registration or re-approval
   is needed; existing node statuses and area approvals remain in place.

In compatibility mode, unsigned requests retain the older hub behavior: they
can register a new node but cannot change an existing node's name or host.
An unsigned re-subscribe can apply only area tags saved in the first
registration, allowing manually approved legacy nodes to finish subscribing.
Changing an existing node's requested areas requires an updated, signing client.
Malformed or invalid signatures are rejected in both modes.

A signed re-subscribe is how a node updates its own BBS name and host on the
hub. Node keys are public, so without the signature anyone could have filed a
subscribe request in another node's name; a signed request proves the caller
holds the key. A node ID that is already registered under a different key is
refused with HTTP 409.

The hub logs **every unsigned subscription**, even without area tags, as
`v3net hub: unsigned subscribe`, with `claimed_node`, `network`, `known_node`,
and `rejected`. These logs identify claimed legacy identities, not verified
callers. A quiet log alone does not prove all nodes have upgraded: an offline
node may return later. Coordinate with those sysops before enforcing signatures.

When you choose to require signatures, set **Signed Subscribe** to **Y**
(or `hub.requireSignedSubscribe` to `true` in `configs/v3net.json`) and restart
the BBS. All unsigned subscriptions then receive HTTP 401 without changing
hub data, with a response explaining how to upgrade while preserving the key.
Signed first registrations work without prior approval; normal hub approval
policy still determines whether the registration becomes active.

If an older leaf is still needed, set the option back to **N** and restart.
Its registration remains intact. Neither enabling nor disabling enforcement
requires database cleanup, key rotation, or a binary rollback.

---

## Hub signatures

An updated hub signs everything it sends a leaf in answer to a signed
request (message pages, replies to posts, chat and sysop calls, and each
event on the live event stream) with its node key. That is the key its
`/info` page serves and the leaf pins as **Hub Key** (`hubPubKey`); see
[Coordinator Key Pinning](v3net/nal.md#coordinator-key-pinning). The leaf
checks every signature against the pinned key, so a reply altered on the way,
or one from a server that is not your hub, is refused. Each signature is tied
to the request it answers (through a random nonce the leaf sends with each
request) and each event to its place in the stream, so an old reply or event
cannot be passed off as a new one. A refusal of the event stream is checked
too, so a hub that changed its key is caught there as well. This matters most for
hubs on plain HTTP or behind a proxy that terminates TLS, and the key stays
the same when the hub's certificate changes.

**When the hub's key changes**, the leaf stops talking to the hub for that
network: it no longer polls, drops the event stream, and refuses to send
messages, chat or logon notices (these fail with
`sync with the hub is paused until its key matches the pin`). It logs
`leaf: pausing sync with the hub` and reports the hub's new key the same way
as a changed coordinator key: it is recorded as **Offered Hub**
(`pendingHubPubKey`) and every SysOp is warned at login.

A hub whose operator moved the network to its new key with a signed
handover is the exception: the leaf checks the handover, moves **Hub Key**
itself and carries on (see
[Signed Handovers](v3net/nal.md#signed-handovers)).

To resume, confirm the new key with the network's operator through a channel
you already trust, then copy it into **Hub Key**, as in
[Accepting a New Key by Hand](v3net/nal.md#accepting-a-new-key-by-hand).
The running BBS picks the edit up without a restart, checks `/info` against
the new pin and resumes at once (`leaf: hub key matches the pin; resuming
sync`). Messages posted locally meanwhile wait in the message base and are
sent then. While paused, the leaf also re-checks `/info` every five minutes,
so a hub that goes back to the pinned key resumes on its own.

A single reply whose signature fails while `/info` still serves the pinned
key is refused on its own (the poll or send fails and is retried as usual);
the leaf does not stay paused.

**Older hubs** do not sign. The leaf logs
`leaf: hub does not sign its responses (older hub)` once and carries on
unverified, as before, so leaves and hubs can be upgraded in any order. A
leaf with no **Hub Key** pinned (a hub that does not publish one) cannot
check signatures either. The signature format is in
[Hub Signatures](v3net/message-areas.md#hub-signatures).

### Refusing unsigned replies

A reply with its signature removed looks just like one from an older hub,
and anything between your BBS and the hub (a plain-HTTP hop, a proxy) can
remove it. So the leaf refuses unsigned replies from a hub in two cases:

- **Once it has seen the hub sign.** The first time a hub signature checks
  out against **Hub Key**, the leaf logs `leaf: hub signs its responses;
  unsigned replies from it are refused from now on` and sets **Hub Signs**
  (`hubSigned`) on the subscription in `v3net.json`. From then on it refuses
  that hub's unsigned replies, after a restart too. You do not need to do
  anything for this.
- **When you turn on Signed Hub** (`requireSignedHub`) on the subscription.
  The leaf then refuses unsigned replies from the start, before it has seen
  the hub sign, and a hub that does not sign at all keeps the network paused.
  It defaults to **N** while hubs upgrade; turn it on once you know your hub
  signs (**Hub Signs** shows **Y**). A later release will turn it on by
  default.

An unsigned reply is handled like one whose signature fails: the leaf logs
`leaf: refusing an unsigned reply from the hub`, pauses, checks the hub's
`/info`, and, if it still serves the pinned key, resumes, having dropped
only that reply. If replies keep arriving unsigned, nothing gets through. An
event stream that was opened unsigned before the leaf required signatures is
dropped the same way before its next event is used. If the hub's `/info`
serves no key at all, the pause holds until you clear **Hub Signs** as below;
a pause because the hub's key changed holds until you accept the key, as
above, whatever you do with **Hub Signs**.

**If your hub really stopped signing**, for example because its operator
went back to an older version, every reply is refused until you clear the
record. Check with the hub's operator first: if the hub still signs,
something on the way is removing its signatures. Then, in the subscription's
edit form, set **Hub Signs** to **N** (or remove `"hubSigned": true` from the
entry in `v3net.json`) and make sure **Signed Hub** is **N**. The running BBS
picks the change up without a restart and accepts the hub's unsigned
replies again. When the hub signs again, the leaf notices and sets **Hub
Signs** back on its own.

If the leaf could not write **Hub Signs** to `v3net.json` (it logs `leaf:
could not record that the hub signs; will retry at the next reply`), it
still refuses unsigned replies while it runs, and the field shows **N**.
Set **Hub Signs** to **N** anyway and save: the file is unchanged, but the
save still makes the running BBS re-read `v3net.json`. If you edit the file
by hand, saving it unchanged (or `touch configs/v3net.json`) does the same.
The leaf then drops the record and does not write it later unless the hub
signs again (it logs `leaf: config does not record that the hub signs;
dropping the record not yet stored`). The BBS knows this was you because the
file's timestamp moved and its contents did not. It cannot tell which
subscription you meant, so every subscription holding such a record drops it.

Any other change saved to `v3net.json` while the write is failing leaves the
record and the retry alone, and the same goes for a key the leaf has pinned
but could not write yet. So if the save that set **Hub Signs** to **N** also
changed something else in the file, or another change to it was still waiting
to be applied, the leaf keeps the record and does not log that line: save
**Hub Signs** again once the change has been applied, or restart the BBS,
which starts from what the file holds.

---

## Requiring signed messages

Updated leaves sign every message they post with their node key, so readers
do not have to take the hub's word for who wrote it. The message carries the
author's public key (`origin_pubkey_b64`) and an ed25519 signature
(`signature_b64`) over its author-set fields: network, area, the message,
thread and parent UUIDs, origin node and board, from, to, subject, date,
body, tearline, origin line and attributes. Kludges are not signed. The
exact bytes signed are in [Message Signatures](v3net/message-areas.md#message-signatures).

- **The hub** rejects a signed post (HTTP 403) unless the signature verifies,
  the key is the one the posting node subscribed with, and `origin_node` is
  that key's node ID. It stores the message unchanged, signature included.
- **Every leaf** checks the signature again when it polls, independently of
  the hub. A message that was altered after signing, or whose `origin_node`
  does not match its key, is logged as
  `leaf: message failed author signature check, dropping` and not imported.
  The leaf also drops a message labelled with a different network than the
  one it is polling, and a signed message over 32 KB, rather than cutting
  text the signature covers. The rest of the poll continues.
- **Long messages**: the leaf cuts a body over 32 KB down before signing, so
  the signature covers the body readers get. The hub never shortens a signed
  message; a signed post over the limit is refused with HTTP 413. Unsigned
  posts from older leaves are still shortened by the hub as before.
- **Every post**, signed or not, must name the posting node as its
  `origin_node`; a post claiming another node is refused with HTTP 403.

**Signed Messages defaults to N** (`hub.requireSignedMessages: false`),
including when the setting is absent from an older configuration. In that
mode the hub accepts unsigned posts from older leaves, logging each at debug
level as `v3net hub: accepting unsigned message`, and leaves import unsigned
messages as before unless they are set to refuse them ([on a leaf](v3net/configuration.md#on-a-leaf)). A message that carries a signature which does not verify
is rejected in both modes.

To move a network to signed messages:

1. Upgrade the hub. Nothing changes for existing leaves.
2. Leaf sysops upgrade, rebuild, and restart at their own pace, keeping their
   node key. Their new posts are signed automatically.
3. Watch the hub log at debug level for `v3net hub: accepting unsigned message`
   to see which nodes still post unsigned. As with signed subscriptions, a
   quiet log does not prove every node has upgraded; check with sysops.
4. Set **Signed Messages** to **Y** (or `hub.requireSignedMessages` to `true`
   in `configs/v3net.json`) and restart the BBS. Unsigned posts then get
   HTTP 401 telling the leaf to upgrade.

Setting it back to **N** and restarting undoes this; no data changes either
way. Messages stored before the switch stay as they are, and leaves keep
importing those that are unsigned.

### On a leaf

A leaf can refuse unsigned messages too, whatever the hub does. Set
**Signed Messages** to **Y** in the subscription's edit form (or
`requireSignedMessages` to `true` on the entry in `leaves`). The running BBS
applies it without a restart. From then on the leaf drops every unsigned
message it polls from that network, logging each as
`leaf: unsigned message from hub, dropping`, and imports only messages whose
signature verifies. Messages already in your message bases are not touched.

It defaults to **N**, including when the setting is absent, because an
unsigned message is still normal while nodes upgrade. Leave it at **N** until
every node on the network signs its posts, or the leaf drops their messages.
Setting it back to **N** applies to messages polled from then on. Unsigned
messages dropped while it was on are not fetched again, apart from any newer
than the last message the leaf imported.

Signed messages are not yet required by default on either side. That will
come in a later release, once nodes have upgraded.

---

## Security Considerations

**Ed25519 signatures are the security foundation.** Every message and NAL is signed with the originating node's Ed25519 private key and verified by recipients. This prevents impersonation and message forgery regardless of the transport layer. The hub serves plain HTTP — transport encryption is not required for authentication or integrity.

**Transport encryption is optional.** The hub serves plain HTTP by default. If you need to encrypt traffic in transit (e.g. for private mail), place a reverse proxy with a valid CA-signed certificate in front of the hub. Note that data is still stored in plaintext at rest:

- `data/v3net_dedup.sqlite` — deduplication database containing message content
- `data/v3net_hub/` — hub database (hub nodes only), fully readable by the hub operator and anyone with server access
- Local message base files — JAM or squish files written when messages are imported into boards

Restrict file permissions appropriately and consider the security of any backup systems that touch these paths.

**Protect your private key.** `data/v3net.key` and any exported seed phrase files should be readable only by the BBS process user (`chmod 600`). A compromised key allows an attacker to impersonate your node on all V3Net networks. Do not include the key file in unencrypted backups or commit it to version control.

**Hub operators can read all traffic.** V3Net is a federated public message network. The hub stores and forwards messages in plaintext. The hub sysop — and anyone with access to the hub server — can read all messages routed through it. This is the same trust model as FidoNet echomail and similar networks.

**Auto-approve.** Setting `Auto Approve: Y` allows any node to subscribe to your hub without review. Appropriate for open public networks; set it to `N` and manually approve nodes for curated or private networks. `Auto Approve Areas: Y` does the same for area proposals: any subscribed node can add an area to the network. Leave it `N` to review proposals in the Coordinator Panel, even on an open network.

**Public areas are public.** Messages posted to networked areas are replicated to every subscribed node and stored indefinitely on each. Do not post sensitive information in public message areas.

---

## Technical Reference

### File Locations

| File | Purpose |
|------|---------|
| `configs/v3net.json` | Generated and managed by the config editor; see the [Manual Configuration Reference](v3net/manual-config.md) for every key |
| `data/v3net.key` | Ed25519 keypair (auto-generated on first V3Net start, mode `0600`) |
| `data/v3net_dedup.sqlite` | Message deduplication database |
| `data/v3net_hub/` | Hub data directory (SQLite DB, NAL files) |
| `data/v3net_seen_areas.json` | Areas each network has offered, for the login question about new areas |
| `data/v3net_declined_areas.json` | Areas each SysOp answered **No** to at login |

### Hub Listener

The hub listens on `hub.host`:`hub.port`, with the port defaulting to `8765`.
A blank host binds every interface. An IPv6 host may be written bare (`::1`)
or bracketed (`[::1]`), and a scoped literal such as `fe80::1%eth0` is kept
as written. On startup the hub logs `v3net hub starting addr=:8765`.

### Configuration File

Every key in `configs/v3net.json`, with its type, default and meaning, is
tabulated in the [Manual Configuration Reference](v3net/manual-config.md),
together with the network registry format and complete leaf and hub examples.
The command-line tools that touch V3Net are documented where they are used:
`helper post` in [V3Net Message Areas](v3net/message-areas.md#technical-reference),
`v3net-bootstrap` in [V3Net Bootstrap Tool](v3net/bootstrap.md), and
`v3net-handover` in
[Moving the Network to a New Key](v3net/nal.md#moving-the-network-to-a-new-key).

---

## Related Documentation

- [Joining FelonyNet](v3net/felonynet.md) — step-by-step walkthrough for the FelonyNet network
- [V3Net Message Areas](v3net/message-areas.md) — how posts travel, `helper post`, menu commands
- [Network Area List (NAL)](v3net/nal.md) — area subscriptions, access modes, and proposals
- [V3Net Chat](v3net/chat.md) — real-time chat across the network
- [V3Net Hub TLS](v3net/hub-tls.md) — HTTPS for a hub through a reverse proxy
- [V3Net Key Recovery](v3net/recovery.md) — backing up and restoring your node identity
- [Manual Configuration Reference](v3net/manual-config.md) — JSON field reference for `v3net.json`
