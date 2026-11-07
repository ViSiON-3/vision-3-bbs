# V3Net Central

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. See [V3Net Configuration](v3net/configuration.md).

V3Net Central is the caller-facing side of a V3Net network: a shared place
for callers to find the other boards on the network, and, in later phases,
to meet people and join activities across them. This page covers **phase 1,
the board directory** (a V3Net Central menu for callers, and a listing each
participating board can publish so others can find it and call it), and
**phase 2, [Who's Online and Last Callers](#whos-online-and-last-callers)**
across the network, for callers who choose to be seen.

Everything in Central is opt-in, per feature and per network. Joining a
V3Net network for message areas enables none of it, and upgrading never
turns anything on. A caller's handle leaves the board only if the sysop
offers a phase 2 feature on that network *and* the caller turns it on for
themselves.

---

## What Callers See

The stock main menu binds the Central menu to `/V` (or the word `CENTRAL`).
It offers:

```
V3Net Central: Menu
───────────────────────────────────────────────────────────────────────────────
  Networks: felonynet

  [B]oards            find participating boards and how to call them
  [W]ho's Online      callers on other boards who chose to be shown
  [L]ast Callers      recent calls to other boards, shared by choice
  [P]references       what, if anything, is shared about you
  [S]tatus            connection to each network's hub
  [Q]uit
```

- **Boards** is the directory: every board that published a listing on a
  network where you enabled Central and the hub serves the directory. It
  appears only when at least one such network exists.
- **Who's Online** and **Last Callers** are the phase 2 feeds, each shown
  only where you offer it and the hub serves it; see
  [below](#whos-online-and-last-callers).
- **Preferences** is where the caller decides what Central shares about
  them: a switch for each phase 2 feature, off until they turn it on, and
  only offered once you have enabled the feature. Later features (the
  lobby, events, the wall) will get their own switches here.
- **Status** shows, per network, your Central switches, whether the hub
  serves the directory, whether the event stream is up, the state of your
  own listing and when the directory was last fetched.

With V3Net off, or Central enabled on no network, the menu says so and
returns. Sysops can also bind the screens separately; see
[menu commands](reference/menu-commands.md#v3net-central).

### The Boards Screen

```
V3Net Central: Boards  ■ online  √ seen this week  · stale
───────────────────────────────────────────────────────────────────────────────
   Board                  Description             Address
 ■ Sector 7               Demoscene and code      ssh://sector7.example:2222
 √ The Vault              Files since 1991        telnet://vault.example:23
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
| `■` online | The board's event stream to the hub is open now. |
| `√` seen this week | The hub had contact with the board in the last 7 days. |
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
retained for the directory. Leaves keep the directory in memory only.
No caller data is stored or sent by any phase 1 feature; for phase 2, see
[Activity Retention](#activity-retention).

---

## Who's Online and Last Callers

Phase 2 lets callers who want to be seen show up across the network:

- **Who's Online** lists the callers on participating boards right now,
  with their board and, if they chose to share one, an availability
  (`available` or `busy`).
- **Last Callers** lists recent calls to participating boards: who, on
  which board, when the call began and ended.

The two are separate features with separate switches, for you and for each
caller. Looking at either sends nothing about the caller who is looking.

### Caller Controls

A caller's switches are on **V3Net Central → Preferences**:

```
V3Net Central: My Preferences
───────────────────────────────────────────────────────────────────────────────
  Nothing about you is shared through V3Net Central.
  Browsing boards, Who's Online and Last Callers sends nothing about you.

  [1] Show me on Who's Online        Off
  [2] My availability               Not shown
  [3] Show my calls on Last Callers Off

  What other boards would see
  Who's Online: Caller on My BBS
                while you are on
  Last Callers: Caller on My BBS
                when each call began and ended; only calls from when you
                turn it on, each kept 24 hours
  Never your real name, location, user number, or what you are doing.
```

- Every switch is off for every account, new or old. Upgrading never turns
  one on, and nothing turns one on but the caller.
- The preview shows exactly what is published: the handle (colour codes
  removed) and your board's name, plus the chosen availability for Who's
  Online and the call times for Last Callers. Nothing else about the
  account is sent.
- **Show my calls** covers calls from the moment it is turned on. Calls
  made before are never published.
- A switch for a feature you do not offer on any network says
  `not offered here` and cannot be turned on.
- The choices are stored on the caller's account on this board
  (`centralPresence`, `centralAvailability` and `centralLastCallsSince` in
  `users.json`). They are not linked to any account on another board.

Callers who log in invisibly are never published, whatever their switches
say, and neither is a call they hid from the local last callers list.

### Offering Them

Each feature is a switch in the subscription's Central block, beside the
phase 1 ones:

| Editor field | JSON | Default | Effect |
|---|---|---|---|
| **Who's Online** | `presence` | N | Offers network Who's Online on this network: callers here can see it, and each can choose to be shown. Needs **Central**. |
| **Last Callers** | `lastCallers` | N | Offers network Last Callers on this network: callers here can see it, and each can choose to have their calls shown. Needs **Central**. |

Your switch is the ceiling. With it off, nothing of that kind is sent to
the network's hub, whatever your callers chose, and the feed is not shown
on your board. Turning it off withdraws what your board published, at
once, as the subscription reloads.

On the stock main menu, `W` shows this board's callers and the network's
together (`LASTCALLERS 20 --all`), each row flagged `local` or with the
network's name; `A` hides this board's callers to show the network's
alone. Bind `LASTCALLERS 20 --network` to open on that view.
While your board offers Last Callers on no network, `W` is the plain local
screen, with no `A` or `R` keys, exactly as before. The Central menu has both feeds as `W` and `L`; see
[menu commands](reference/menu-commands.md#v3net-central) to bind them
elsewhere. Both screens are laid out by the menu set's `LASTNET` and
`WHONET` templates, which you can edit as you would `LASTCALL` and
`WHOONLN`; see [menu templates](menus/menu-system.md#v3net-last-callers-and-whos-online-lastnet-whonet).

The **Status** screen shows each feature per network as `off`, `hub does
not serve it`, `pending`, `failed` (with the hub's reason underneath), or
`on, N online (2m ago)` / `on, N calls (2m ago)`: how many entries your
board last shared, and when.

### How Presence and Last Calls Work

Each board publishes one signed **snapshot** per feature:
`PUT /v3net/v1/{network}/central/presence` and
`PUT /v3net/v1/{network}/central/lastcallers`. A snapshot holds every
entry the board is publishing, and each new one replaces the last whole.
The board signs it with its node key, as it does its listing, and the hub
stores it byte for byte, so a hub can withhold a snapshot but cannot add
a caller to one or change one. A leaf fetching a feed checks every
snapshot's signature itself and drops one that fails.

The snapshot is rebuilt from the board's own state each time: the
sessions logged on now, and the call history, each filtered through the
caller's current switches. It is republished a couple of seconds after
every logon, logoff or change of a switch, and on a heartbeat besides
(every minute for presence; for last callers every 30 minutes, or sooner
when the oldest call is about to reach 24 hours).

**Sessions, not accounts.** A Who's Online entry is a session. Its ID is
a random value for that one call, derived with a key that never leaves the
BBS process, so it says nothing about the account and two calls by one
caller cannot be linked through it. A caller on two nodes shows as two
sessions, grouped as one row with `(2 nodes)`; when one ends, the other
stays.

**Abrupt disconnects.** A caller's session leaves the next snapshot
however it ended, since the snapshot is built from the sessions still
registered; the heartbeat catches anything a logoff missed within a
minute. If a whole board drops off the network, the hub removes its
presence as soon as the board's event stream closes, and presence is
only ever served for a board whose stream is open. Every presence
snapshot also expires 3 minutes after it is published (the protocol
allows at most 5), so even a hub that missed the close stops serving it.
When the board reconnects it republishes at once.

**Withdrawal.** When a caller turns a switch off, the next snapshot leaves
them out, and the hub replaces the old one: they are off Who's Online,
and every call of theirs, including calls already shown, is gone from
Last Callers. When you turn a feature off, or remove or move the
subscription, the board withdraws the snapshot with a `DELETE`. Either
way connected boards are told with a `central_activity_updated` event and
fetch the feed again; a board that was offline fetches the whole feed
when it next looks, so there is no backlog of deletions to deliver.

**Replay.** The hub keeps, per board and feature, the highest sequence
number it has accepted, even after a withdrawal, and refuses any snapshot
that is not above it. A snapshot captured before a caller withdrew cannot
be sent again to put them back.

**What cannot be recalled.** A caller who was on a feed was visible to
anyone who looked while they were. Withdrawal stops it from then on; it
cannot unsee what a caller elsewhere already read. A board that cannot
reach its hub to withdraw stays as last published until its snapshot
expires: minutes for presence, and for last calls no later than 24 hours
after the oldest call in it began.

### Activity Retention

| | Kept on the hub | Kept on a viewing board |
|---|---|---|
| Presence | Latest snapshot per board, while its stream is open and for at most 5 minutes | In memory, 30 seconds |
| Last calls | Latest snapshot per board. It expires no later than 24 hours after its oldest call began | In memory, 30 seconds |

A board never publishes a call that began more than 24 hours ago, the
hub refuses a snapshot that holds one or that would outlive it, and a
viewing board drops any it is sent. Expired snapshots are pruned every 5
minutes. The board's own call history (`callhistory.json`) is unchanged
by any of this.

### Activity Limits

| Limit | Value |
|---|---|
| Sessions per presence snapshot | 64 |
| Calls per last-callers snapshot | 50, newest first |
| Handle | 30 characters, colour codes removed, pipes and control characters refused |
| Presence lifetime | 3 minutes, refreshed every minute (protocol maximum 5) |
| Call retention | 24 hours from when the call began |
| Boards per network, per feature | 500 |
| Publishes per board, per feature | 10 at once, then one per 2 seconds |
| Feed cache on a leaf | 30 seconds, or until an activity event |

### Activity and Older Versions

The hub names `central.presence` and `central.lastcallers` in its
`features` list. A leaf whose hub does not name a feature never calls its
endpoints and hides it, with message networking and the directory carrying
on. A board from before phase 2 publishes nothing and simply does not
appear in the feeds.

Older ViSiON/3 versions sent every caller's handle to every subscribed hub
at logon and logoff, with no switch. This version no longer does: the
only handles it sends are those of callers who opted in, in the snapshots
above. A board still running an older version keeps sending them until it
is upgraded.

---

## Pilot Status

Phase 1 has been exercised end to end in automated tests: two boards on
one hub publish, discover and update each other's listings, a withdrawal
removes a listing, a replayed event is ignored, a hub-altered listing is
dropped by the leaf, and a leaf on a hub without the feature hides it.
Evidence from a volunteer multi-board pilot on a live network is still to
be gathered before Central is offered beyond development use; the tracking
issue is [#670](https://github.com/ViSiON-3/vision-3-bbs/issues/670).

Phase 2 has been exercised the same way: three boards on one hub, two
offering both features and one offering neither, though its callers had
opted in. The two show each other's opted-in callers, and the third's
never appear. A caller ending one of two sessions stays online. A caller
withdrawing leaves both feeds, along with a call published earlier, and
replaying the snapshot captured before the withdrawal is refused. A board
that drops off takes its callers off Who's Online at once and brings
them back when it reconnects. A call older than 24 hours is never
published. A leaf on a hub without the features never calls them.
Turning a feature off on reload withdraws it. A volunteer pilot on a live
network is still to come; the tracking issue is
[#671](https://github.com/ViSiON-3/vision-3-bbs/issues/671).

---

## Related Documentation

- [V3Net Configuration](v3net/configuration.md)
- [Manual Configuration Reference](v3net/manual-config.md)
- [V3Net Chat](v3net/chat.md)
- [Menu Commands: V3Net Central](reference/menu-commands.md#v3net-central)
