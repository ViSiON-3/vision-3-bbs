# V3Net Central

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. See [V3Net Configuration](v3net/configuration.md).

V3Net Central is the caller-facing side of a V3Net network: a shared place
for callers to find the other boards on the network, and, in later phases,
to meet people and join activities across them. This page covers **phase 1,
the board directory**: a V3Net Central menu for callers, and a listing each
participating board can publish so others can find it and call it.

Everything in Central is opt-in, per feature and per network. Joining a
V3Net network for message areas enables none of it, and upgrading never
turns anything on. Nothing a caller does in phase 1 sends anything about
them anywhere.

---

## What Callers See

The stock main menu binds the Central menu to `/V` (or the word `CENTRAL`).
It offers:

```
V3Net Central: Menu
───────────────────────────────────────────────────────────────────────────────
  Networks: felonynet

  [B]oards            find participating boards and how to call them
  [P]references       what, if anything, is shared about you
  [S]tatus            connection to each network's hub
  [Q]uit
```

- **Boards** is the directory: every board that published a listing on a
  network where you enabled Central and the hub serves the directory. It
  appears only when at least one such network exists.
- **Preferences** tells the caller what Central shares about them. In
  phase 1 that is nothing, and the screen says so. Later features
  (presence, the lobby, events, the wall) will each get a switch here, off
  until the caller turns it on, and only once you have enabled the feature.
- **Status** shows, per network, your Central switches, whether the hub
  serves the directory, whether the event stream is up, the state of your
  own listing and when the directory was last fetched.

With V3Net off, or Central enabled on no network, the menu says so and
returns. Sysops can also bind the screens separately; see
[menu commands](reference/menu-commands.md#v3net-central).

### The Boards Screen

```
V3Net Central: Boards  ● online  ○ seen this week  · stale
───────────────────────────────────────────────────────────────────────────────
   Board                  Description             Address
 ● Sector 7               Demoscene and code      ssh://sector7.example:2222
 ○ The Vault              Files since 1991        telnet://vault.example:23
 · Old Haunt              Back someday            telnet://haunt.example:23
───────────────────────────────────────────────────────────────────────────────
 [Enter] Details  [/] Search  [R] Refresh  [Q] Quit                      1/3
```

Boards are ordered online first, then boards the hub has seen in the last
seven days, then stale ones. **Enter** shows everything the board
published (description, every address, software, capabilities, node ID)
and what the hub knows (last seen, when the listing expires). **/** filters
by name, description or address; a blank search clears the filter. **R**
fetches the directory again.

A board's text is shown as it was written. Pipe codes and control
characters are refused when a listing is published, and every field is
cleaned and clipped again before it is drawn, so a listing cannot run
anything on a caller's terminal.

---

## Sysop Setup

Each subscription has its own Central block, in the editor under
**6 — ViSiON/3 Networking (V3Net) → Subscriptions → (the network)** or in
`configs/v3net.json` under `leaves[].central`
([manual reference](v3net/manual-config.md#leaf-central-settings-leavescentral)).

| Editor field | JSON | Default | Effect |
|---|---|---|---|
| **Central** | `enabled` | N | Callers may open Central for this network: the Boards screen fetches the directory. Nothing about your board or your callers is sent. |
| **Publish Listing** | `publishListing` | N | Your board is listed in the network's directory. N withdraws it. |
| **Listing Desc** | `description` | blank | One line about your board, at most 240 characters. |
| **Listing Addrs** | `addresses` | blank | Where to call you: `ssh://host:port`, `telnet://host:port` and so on, up to 8. Blank publishes addresses built from Server Setup (the SSH host, and each enabled listener's port) when that host is a public name or address; a wildcard or loopback bind address such as `0.0.0.0` publishes none, so set this field. |

The two switches are independent. A board can offer the directory to its
callers without being in it, or be in it without offering it. A
subscription with either switch on runs even with no boards selected, so
a board can take part in Central on a network whose message areas it does
not carry.

Changes to `leaves` apply to the running BBS without a restart, as any
subscription change does. Turning **Publish Listing** off, removing the
subscription or moving it to another hub withdraws the listing from the
hub that holds it as the subscription reloads; turning publication on
publishes within moments.

The listing's name is your board name from Server Setup, cleaned as it is
for the network (pipes become slashes, control characters spaces, cut to
64 characters). Its software field is the ViSiON/3 version.

### Checking It

`vision3 doctor` reports a Central address that is not a valid
`scheme://host[:port]`. On the BBS, the **Status** screen under the Central
menu shows the listing as `published`, `pending`, `failed` (with the hub's
reason) or `withdrawn`, and the log says `leaf: central listing published`
or `leaf: central listing withdrawn`.

---

## How It Works

### Listings Are Signed by the Board

A listing is a small JSON document the leaf builds, signs with the node's
Ed25519 key and sends to the hub with `PUT /v3net/v1/{network}/central/listing`.
The hub checks that the signature verifies, that the key is the one the
node subscribed with and that the node ID in the listing is the
authenticated node, then stores the listing exactly as sent.

When a leaf fetches the directory, it checks every listing's signature
itself and drops any that fails, is for another network, has expired, or
is listed under a node other than the one its key derives. The hub's
signature on the reply (see [hub signatures](v3net/configuration.md#hub-signatures))
covers the whole directory, including the fields only the hub can know:
when each board was last seen and whether it is connected now. So:

- A hub can decline to list a board, or serve a stale directory, but it
  cannot put words in a board's listing or list a board that never
  published.
- A board cannot publish as another board: the listing's key must be the
  key it registered, and the node ID is derived from the key.
- Transport authentication (the signed request) says which node sent a
  request. The listing's own signature says who wrote the listing. The
  two are checked separately, so a listing relayed through anything other
  than the hub it was sent to would still verify.

### Freshness and Expiry

Each listing declares `published_at` and `expires_at`, at most 30 days
apart. A publishing leaf refreshes its listing every six hours, moving the
expiry on, so a board that is up never expires. The hub leaves expired
listings out of every directory and removes them hourly.

The hub also records when it last had contact with each listed node (the
opening and closing of its event stream, and each publish) and whether a
stream is open now. The Boards screen shows three states:

| Marker | Meaning |
|---|---|
| `●` online | The board's event stream to the hub is open now. |
| `○` seen this week | The hub had contact with the board in the last 7 days. |
| `·` stale | No contact for over 7 days. The listing is still served until it expires. |

### Withdrawal

Turning **Publish Listing** off, or removing or moving the subscription,
withdraws the listing with `DELETE /v3net/v1/{network}/central/listing`.
The hub deletes the row at once, tells connected leaves, and the board is
gone from the next directory every leaf fetches. A leaf that was offline
when the withdrawal happened simply never sees the listing again:
directories are fetched whole, not as differences, so there is nothing
left to propagate. The hub keeps no copy of a withdrawn listing, only the
highest sequence number the board has published, so that a captured
publish cannot be replayed to undo the withdrawal.

A board that cannot reach the hub to withdraw (the hub is down, or the
board is gone for good) stays listed until its listing expires, at most 30
days after it was last refreshed, and is shown as stale from a week after
the hub last heard from it. A hub operator can remove a listing sooner by
deleting the row from `central_listings` in the hub database.

### Events and Replay

The hub sends `central_listing_updated` and `central_listing_withdrawn`
events on the network's event stream. A leaf never trusts the content of
such an event: it only marks its cached directory stale, so the next look
at the Boards screen fetches it again (the cache otherwise lasts a
minute). Each event carries an ID and an expiry (10 minutes); a leaf drops
an event it has already acted on or one past its expiry, so a replayed or
delayed event costs nothing. An event from another network is ignored.

### Mixed Versions

The hub names the features it serves in the `features` field of
`GET /v3net/v1/{network}/info`. A leaf that finds `central.directory`
there offers the directory; one that does not, or that talks to a hub
from before the field existed, treats Central as unsupported on that
network: the Boards entry is left off the menu, the Status screen says
`no directory`, and nothing is published or withdrawn. Message networking
is unaffected either way.

A hub from this version serves every older leaf exactly as before: the
Central endpoints are only reached by leaves that ask for them, and the
`features` field is ignored by leaves that predate it. A leaf whose hub
is rolled back after the leaf saw the feature stops offering it on the
next directory fetch.

### Operating Limits

| Limit | Value |
|---|---|
| Listings per network | 500 |
| Name | 64 characters |
| Description | 240 characters |
| Addresses | 8, each 128 characters, schemes `telnet`, `ssh`, `rlogin`, `http`, `https` |
| Capabilities | 16, each 32 characters |
| Listing lifetime | 30 days from `published_at`; refreshed every 6 hours |
| Publishes per node | 3 at once, then one per 10 seconds |
| Directory cache on a leaf | 1 minute, or until a listing event |
| Event lifetime | 10 minutes; 256 event IDs remembered per leaf |

A listing over any limit is refused by the hub with the reason, and the
leaf shows it on the Status screen. The whole directory is fetched in one
signed reply; a leaf refuses one over 4 MB, which a full directory of 500
listings at every limit stays well inside.

### Retention

The hub keeps one row per board and network: the listing as sent, its
sequence number, its expiry, and the last-seen time. Nothing else is
retained for Central in phase 1. Leaves keep the directory in memory only.
No caller data is stored or sent by any phase 1 feature.

---

## Pilot Status

Phase 1 has been exercised end to end in automated tests: two boards on
one hub publish, discover and update each other's listings, a withdrawal
removes a listing, a replayed event is ignored, a hub-altered listing is
dropped by the leaf, and a leaf on a hub without the feature hides it.
Evidence from a volunteer multi-board pilot on a live network is still to
be gathered before Central is offered beyond development use; the tracking
issue is [#670](https://github.com/ViSiON-3/vision-3-bbs/issues/670).

---

## Related Documentation

- [V3Net Configuration](v3net/configuration.md)
- [Manual Configuration Reference](v3net/manual-config.md)
- [V3Net Chat](v3net/chat.md)
- [Menu Commands: V3Net Central](reference/menu-commands.md#v3net-central)
