# V3Net Network Area List (NAL)

## What is the NAL?

The Network Area List (NAL) is the official list of message areas that a V3Net network carries. Think of it as the master directory of all the message boards available on a network like felonynet. It is what the area browser shows you, what the login question about new areas is drawn from, and what decides who may carry each area.

The NAL is **signed** by the network coordinator — the sysop who runs the hub. This signature ensures that nobody can tamper with the area list. Every node on the network can verify the signature before trusting the NAL.

## How Areas Work

Each message area has:

- **Tag** — A short identifier like `fel.general` or `fel.phreaking`
- **Name** — A human-readable display name
- **Description** — What the area is about
- **Access Mode** — Who can subscribe (see below)
- **Policy** — Content rules (max message size, ANSI allowed, etc.)

## Subscribing to Areas

Subscriptions can be managed from two places. In the config editor, open the
subscription's **Areas** (or **Browse Areas**) field under
**6 — ViSiON/3 Networking (V3Net) → 2. Subscriptions**. In the BBS, the V3Net
sysop menu (`V3NETM`, **3** on the Admin menu) has **Area Subscriptions**
(`V3NETAREAS`). You'll see a list of all areas the network offers:

```
[ V3Net: felonynet — Area Subscriptions ]

  TAG                 NAME              STATUS     LOCAL BOARD
  fel.general         General           ACTIVE     FelonyNet General
  fel.phreaking       Phreaking         PENDING    —
  fel.art             ANSI/ASCII Art    —          —

  [Space] subscribe/unsubscribe  [E]dit local board name
  [P]ropose new area             [Q]uit
```

- Press **Space** on an area to subscribe or unsubscribe
- Press **E** to set which local message base this area maps to
- Status shows `ACTIVE` (you're in), `PENDING` (waiting for approval), or blank (not subscribed)

A change made here applies at once; no restart is needed. When the hub adds
an area later, each SysOp is asked about it at login — see
[New Areas on Your Networks](v3net/message-areas.md#new-areas-on-your-networks).

## Access Modes

Each area has one of three access modes:

### Open
Any subscribed node can carry this area. Subscribe and you're immediately active. Most public discussion areas use this mode.

### Approval
The area manager must approve your subscription. After you subscribe, your status shows `PENDING` until the manager approves you from their sysop menu.

### Closed
Only nodes explicitly added by the area manager can access this area. You cannot subscribe on your own — the manager must add your node ID to the allow list.

A **deny list** is always enforced regardless of mode. If your node is on an area's deny list, you cannot subscribe no matter what.

## Proposing a New Area

Any sysop on the network can propose a new area. From Area Subscriptions, press **P** to open the proposal form:

```
[ V3Net: Propose New Area — felonynet ]

  Area Tag     : fel.________________
  Display Name : ________________________________
  Description  : ________________________________________________
  Language     : en
  Access Mode  : [Open] / Approval / Closed
  Allow ANSI   : [Y]

  [S]ubmit  [Q]uit
```

Area tags must follow the format `{prefix}.{name}` — for example, `fel.coding` or `fel.music`. The prefix is usually the network's short name.

After submitting, your proposal goes to the network coordinator for review. You'll be notified via the V3Net event stream when it's approved or rejected. On a hub with **Auto Approve Areas** set to `Y`, the area is added to the NAL as soon as the proposal arrives, with no review.

## For Area Managers

If you manage an area (the coordinator assigned you as manager), you'll see the **Area Access Requests** screen in the V3Net menu:

```
[ V3Net: Area Access Requests ]

  #  NETWORK     AREA TAG          BBS NAME              NODE ID   REQUESTED
  1  felonynet   fel.phreaking     The Underground BBS   A1B2C3D4  2d ago
  2  felonynet   fel.phreaking     Sector 7 BBS          E5F6A7B8  5h ago

  [A]pprove #  [D]eny #  [Q]uit
```

Type the action letter and the row number, for example `A 1` or `D2`.

- **Approve** — Grants the requesting node access to the area and activates its subscription
- **Deny** — Rejects the request, optionally with a reason, and adds the node to the area's deny list so it cannot re-request

The list covers every subscribed network and is refreshed after each action.

## For Network Coordinators

Coordinators see the **Coordinator Panel** in the V3Net menu:

```
[ V3Net: Coordinator Panel — felonynet ]

  [P]ending area proposals  (2)
  [M]anage area managers
  [Q]uit
```

The panel appears only when the NAL of a subscribed network names your node as its coordinator.

### Reviewing Proposals

When sysops propose new areas, you review them from the Pending Proposals screen. Type the action letter and the row number, for example `A 1` or `R2`.

- **Approve** — Asks for the access mode (`O`pen, `A`pproval or `C`losed; press Enter to keep the one proposed), then adds the area to the NAL and publishes it to all nodes. The proposing node becomes the area's manager.
- **Reject** — Declines the proposal, optionally with a reason, and notifies the proposing node

Proposals only wait here while the hub's **Auto Approve Areas** is `N`
(`hub.autoApproveAreas: false`). With it on, every proposal is added as it
arrives. A hub configured before the setting existed follows **Auto
Approve** instead, so an open hub that approves subscribers on its own also
approves areas until you set **Auto Approve Areas** to `N` explicitly. See
[V3Net Configuration](v3net/configuration.md#system-setup--server-setup).

### Changing an Area's Manager

**Manage area managers** lists every area in the NAL with its current manager. Type `C` and the row number, for example `C 3`, then enter the node ID of the new manager. The new manager must be an active subscriber of the network: the hub records the node's public key from its own registry, so you never paste a key. A sysop can find their node ID on the V3Net status screen.

The change is published to all nodes straight away, and the new manager sees the area's access requests from then on.

### Handing Over a Network

The hub signs the NAL with its own key, so the coordinator is always the hub operator. There is no separate coordinator transfer. To give a network to a new operator, move the hub:

1. Write down the hub's 24-word recovery phrase, or export it to a file (see [Key Recovery](v3net/recovery.md)).
2. Copy the hub's data directory to the new server.
3. On the new server, enter those 24 words in the recovery screen (`./config` → V3Net → Node Identity → `[R] Recover`), then start the hub.

The node ID stays the same, so the NAL, subscribers and area managers carry over unchanged. Point the hub's hostname at the new server; if the address changes instead, every subscriber has to update the hub URL in their V3Net config.

To share the work of running a network without handing it over, reassign individual areas to other sysops with **Manage area managers**.

## Troubleshooting

| Symptom | What is happening |
|---------|-------------------|
| An area the hub just added is not in the browser | The leaf re-fetches the NAL when the hub announces a change (`nal_updated`) and whenever its event stream reconnects. If the BBS was down, it catches up on the next start. |
| Subscribed, but status stays `PENDING` | The area is in approval mode and its manager has not acted yet. Approval-mode areas are the manager's call, not the coordinator's. |
| Cannot subscribe at all | The area is closed and your node is not on its allow list, or your node is on the deny list. Ask the area manager. |
| Coordinator Panel never appears | Your node is not the coordinator of any subscribed network. Only the hub's own node is; see Handing Over a Network. |

## Related Documentation

- [V3Net Message Areas](v3net/message-areas.md) — local areas, message flow, the login question
- [V3Net Configuration](v3net/configuration.md) — hub approval settings
- [V3Net Key Recovery](v3net/recovery.md) — moving a hub's identity

---

## Technical Reference

### NAL Document

The NAL is a signed JSON document. The coordinator's Ed25519 key signs a canonical representation of the NAL (all fields sorted alphabetically, signature field cleared). Any modification after signing causes verification to fail.

| Field | Meaning |
|-------|---------|
| `v3net_nal` | Format version |
| `network` | Network name |
| `coordinator_node_id`, `coordinator_pubkey_b64` | The hub's node ID and public key, which signed the document |
| `updated` | Date of the last change, `YYYY-MM-DD` UTC |
| `signature_b64` | Ed25519 signature over the canonical payload |
| `areas[]` | The areas, each with `tag`, `name`, `description`, `language`, `moderated`, `manager_node_id`, `manager_pubkey_b64`, `access`, `policy` and `added` (the date the hub added it, used by the login question) |
| `areas[].access` | `mode` (`open`, `approval` or `closed`), `allow_list` and `deny_list` of node IDs |
| `areas[].policy` | `max_body_bytes`, `allow_ansi`, `require_tearline` |

Area tags must match `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`.

### Where It Lives

- On the hub: in the hub data directory (`hub.dataDir`, normally
  `data/v3net_hub/`), seeded from `hub.initialAreas` on first start.
- On a leaf: fetched with `GET /v3net/v1/{network}/nal`, kept in memory, and
  re-fetched after a `nal_updated` event (with a short random delay so a
  hub's leaves do not all ask at once) and on every SSE reconnect.
- `data/v3net_seen_areas.json` records which areas each network has offered
  this BBS, and `data/v3net_declined_areas.json` the areas each SysOp
  declined at login.

The optional git repository at `v3net-registry` serves as an archival mirror only. All area management happens natively through the V3Net protocol and ViSiON/3's sysop menus; the `v3net-bootstrap` tool can publish a NAL by hand when needed (see [V3Net Bootstrap Tool](v3net/bootstrap.md)).
