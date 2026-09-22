# V3Net Network Area List (NAL)

## What is the NAL?

The Network Area List (NAL) is the official list of message areas that a V3Net network carries. Think of it as the master directory of all the message boards available on a network like felonynet.

The NAL is **signed** by the network coordinator — the sysop who runs the hub. This signature ensures that nobody can tamper with the area list. Every node on the network can verify the signature before trusting the NAL.

## How Areas Work

Each message area has:

- **Tag** — A short identifier like `fel.general` or `fel.phreaking`
- **Name** — A human-readable display name
- **Description** — What the area is about
- **Access Mode** — Who can subscribe (see below)
- **Policy** — Content rules (max message size, ANSI allowed, etc.)

## Subscribing to Areas

From the V3Net sysop menu, select **Area Subscriptions**. You'll see a list of all areas the network offers:

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

After submitting, your proposal goes to the network coordinator for review. You'll be notified via the V3Net event stream when it's approved or rejected.

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
  [Q]uit
```

The panel appears only when the NAL of a subscribed network names your node as its coordinator.

### Reviewing Proposals

When sysops propose new areas, you review them from the Pending Proposals screen. Type the action letter and the row number, for example `A 1` or `R2`.

- **Approve** — Adds the area to the NAL as it was proposed and publishes it to all nodes
- **Reject** — Declines the proposal, optionally with a reason, and notifies the proposing node

### Managers and Coordinator Transfer

The hub always makes the proposing node the manager of an approved area and has no endpoint for changing it afterwards. Coordinator transfer exists on the hub's HTTP API but needs the new coordinator's public key and an acceptance step, and is not offered from the BBS menu yet.

## For Developers

The NAL is a signed JSON document. The coordinator's ed25519 key signs a canonical representation of the NAL (all fields sorted alphabetically, signature field cleared). Any modification after signing causes verification to fail.

The optional git repository at `v3net-registry` serves as an archival mirror only. All area management happens natively through the V3Net protocol and Vision/3's sysop menus.
