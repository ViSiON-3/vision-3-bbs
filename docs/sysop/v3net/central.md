# V3Net Central

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. See [V3Net Configuration](v3net/configuration.md).

V3Net Central is the caller-facing side of a V3Net network: a shared place
for callers to find the other boards on the network, and, in later phases,
to meet people and join activities across them. This page covers **phase 1,
the board directory** (a V3Net Central menu for callers, and a listing each
participating board can publish so others can find it and call it),
**phase 2, [Who's Online and Last Callers](#whos-online-and-last-callers)**
across the network, for callers who choose to be seen, **phase 3,
[network chat](#network-chat)**, for callers who choose to talk, and
**[activity alerts](#activity-alerts)**, live toasts of who logs on, joins
network chat or posts on the other boards, for callers who choose to send
or see them.

Everything in Central is opt-in, per feature and per network. Joining a
V3Net network for message areas enables none of it, and upgrading never
turns anything on. The one exception is network chat, which boards had
before Central: it stays on through an upgrade, and is on for a new
subscription, until you turn it off (see [Offering It](#offering-it)). A
caller's handle leaves the board only if the sysop offers a phase 2
feature or an activity alert on that network *and* the caller turns it on
for themselves, or if the caller agrees to network chat and joins a room.

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
- **Chat** opens network chat on the networks where you offer it, once
  the caller has agreed to take part; see [Network Chat](#network-chat).
- **Preferences** is where the caller decides what Central shares about
  them: a switch for each phase 2 feature and one for network chat, off
  until they turn it on, and only offered once you have enabled the
  feature. Later features (events, the wall) will get their own switches
  here.
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

A caller's switches are on one screen, reached two ways: **V3Net Central →
Preferences**, and **V3Net Sharing** (`R`) on Your Konfig (`K`). Konfig
shows that item only while a network offers Who's Online, Last Callers or
network chat, with what the caller shares beside it (Off, or Online, Calls
and Chat as they apply):

```
V3Net Central: Sharing
───────────────────────────────────────────────────────────────────────────────
  Nothing about you is shared through V3Net Central.
  Browsing boards, Who's Online and Last Callers sends nothing about you.
  [1] Show me on Who's Online        Off
  [2] My availability               Not shown
  [3] Show my calls on Last Callers Off
  [4] Take part in network chat     Off
  [5] Activity alerts               Off

  What other boards would see
  Who's Online: Caller on My BBS
                while you are on
  Last Callers: Caller on My BBS
                when each call began and ended; only calls from when you
                turn it on, each kept 24 hours
  Never your real name, location, user number, or what you are doing.
```

- A switch the caller has not set follows the network's **Callers
  Default**: off unless you turn that on, in which case the screen shows
  it on for that network. Upgrading never turns one on by itself.
- The preview shows exactly what is published: the handle (colour codes
  removed) and your board's name, plus the chosen availability for Who's
  Online and the call times for Last Callers. Nothing else about the
  account is sent.
- **Show my calls**, turned on by the caller, covers calls from that
  moment; calls made before are never published. A caller counted in by
  **Callers Default** has their calls from the last 24 hours shown.
- Pressing a switch flips what the screen shows, and from then on it is
  the caller's own choice: a default no longer applies to it.
- A switch for a feature you do not offer on any network says
  `not offered here` and cannot be turned on.
- The choices are stored on the caller's account on this board
  (`centralPresence`, `centralAvailability` and `centralLastCallsSince` in
  `users.json`, with `centralPresenceChosen` and `centralLastCallsChosen`
  recording that the caller set a switch themselves). They are not linked to any account on another board.

Callers who log in invisibly are never published, whatever their switches
say, and neither is a call they hid from the local last callers list.

### Offering Them

Each feature is a switch in the subscription's Central block, beside the
phase 1 ones:

| Editor field | JSON | Default | Effect |
|---|---|---|---|
| **Who's Online** | `presence` | Y on a new subscription | Offers network Who's Online on this network: callers here can see it, and each can choose to be shown. Needs **Central**. |
| **Last Callers** | `lastCallers` | Y on a new subscription | Offers network Last Callers on this network: callers here can see it, and each can choose to have their calls shown. Needs **Central**. |
| **Callers Default** | `callersDefault` | Y on a new subscription | Counts a caller who has not set a switch of their own as sharing on this network's Who's Online and Last Callers. N leaves them out until they opt in. A caller's own choice, on or off, always wins. |

A subscription created by the setup wizard, the config editor, the
bundled `configs/v3net.json` or a caller-side subscribe from the BBS starts
with **Central**, **Who's Online**, **Last Callers** and **Callers
Default** on (and **Publish Listing** off), so its callers show on the
network until they turn sharing off under Preferences. A subscription from
before this has no Central block, or one you set yourself, and is left as
it is: switch **Callers Default** on yourself to share callers who have
not chosen.

With **Callers Default** on, a caller who has not chosen has their calls
from the last 24 hours shown, including calls made before you turned it
on. Callers who log in invisibly, and calls hidden from the local list,
are still never sent.

#### Tell Your Callers With a News Item

Callers counted in by **Callers Default** never chose to share, so tell
them before or as you turn it on. The recommended way is a one-time
[System News](../menus/news.md) item: post it from the Admin Menu (`%` →
`W` → `A`) with **Always** set to N, and each caller sees it once, at
their next login, and not again. It needs `PRINTNEWS` in the login
sequence, which the default `configs/login.json` has.

A suggested item (the title is at most 28 characters):

```text
Title:  V3Net Sharing Is On
Always: N
Body:
This board shares who is online, and recent calls, with other
boards on V3Net. Other boards see your handle and this board's
name only: never your real name, location or what you are doing.

You are included unless you turn it off. To change it, press
K for Konfig, then R for V3Net Sharing.
```

Post it before switching **Callers Default** on for an existing board.
On a new board, post it as part of setup, so the first callers see it.

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

## Network Chat

Phase 3 lets callers on different boards meet in one room. It is the
`CHAT` command's network side, which this phase puts under the same
consent as the rest of Central: a switch for you per network, and an
agreement from each caller before they join. Local chat (this board
only) needs neither. Unlike the other Central features, chat is on
unless you turn it off, and does not need **Central** itself on: boards
chatted over V3Net before Central, and go on doing so.

### What Callers See

**Chat** on the Central menu, where you offer chat and the hub serves it,
runs network chat without the Local option. The main menu's `C` (the
`CHAT` command) still offers the networks and Local as before; see
[V3Net Chat](v3net/chat.md).

Before a caller's first network join, on either path, they see what
leaves the board and are asked to agree:

```
V3Net Central: Network Chat
───────────────────────────────────────────────────────────────────────────────
  Network chat puts you in a room with callers on other boards.

  What other boards see
  Your handle Caller and this board's name My BBS,
  in the room's user list while you are there and on every line you
  type. A private message goes to the hub and to the recipient's board.
  Never your real name, location, user number, or what you are doing.

  What is kept
  The hub keeps room messages as history for callers who join later,
  and private messages for the same time; a ViSiON/3 hub keeps 7 days.
  Room history can be read by anyone who knows the hub's address, without
  joining: treat what you say in a room as public.

  Who moderates
  Your SysOp decides whether this board takes part, and can turn network
  chat off for you; the hub's operator decides which boards may. The hub
  limits how fast anyone can post. You can mute or block anyone from your
  screen (/mute for this session, /block for good).
  You can turn network chat off at any time under Preferences (V3Net
  Central, or V3Net Sharing in Konfig), and leave a room with /q.

  Take part in network chat? (Y/N):
```

- **Y** records the agreement on the caller's account and goes on to the
  network picker. It is asked once; the notice is not shown again.
- **N**, or anything else, records nothing. From the main menu the caller
  gets local chat this time and is asked again next time; from the Central
  menu they are sent back.
- A caller who has agreed can withdraw under **Preferences**, switch **4**
  (**Take part in network chat**), reached from the Central menu or as
  **V3Net Sharing** in Konfig, which is there whenever a network offers
  chat, with or without **Central** on. A caller you have barred sees the
  switch as `Off (set by the SysOp)`; see [Moderation](#moderation). Turning it on again shows the notice
  again. The choice is stored on the local account (`centralChat`, with
  `centralChatChosen` recording that the caller answered) and is not
  linked to any account on another board.
- No **Callers Default** applies to chat: nobody is in a room until they
  agreed themselves.

In the room, a caller from another board is named with their board, as
`Eve@Other BBS`, in the user list, on each line they type, on a private
message from them and on join and leave notices; this board's own callers
are named by handle alone. `/msg` takes the names as listed. See
[Callers From Other Boards](v3net/chat.md#callers-from-other-boards).

Withdrawing takes effect at once. A caller in a network room on another
node ends chat with the next line they type (it is not posted), one
waiting at a network or room picker is not joined, and the `/network`
command no longer lists the networks.

### Offering It

One more switch in the subscription's Central block:

| Editor field | JSON | Default | Effect |
|---|---|---|---|
| **Lobby Chat** | `chat` | Y | Offers network chat on this network: callers here can join its rooms once they agree. Does not need **Central**. N ends it for callers who are in a room. |
| **Chat Block List** | `chatBlocked` | empty | Callers on other boards, each `handle@node-id`, whose lines, private messages and join/leave notices this board drops for everyone here. See [Moderation](#moderation). |

The switch is on by default on every subscription, new or upgraded,
including one from before Central, which has no Central block at all: a
board that chats over V3Net today goes on doing so after upgrading, now
with the caller notice in front of the first join. Set it to **N** on a
network where you do not want chat. A subscription with only **Lobby
Chat** on runs with no message areas, as one with **Central** or
**Publish Listing** on does.

Turning the switch off, or removing the subscription, ends the network
chat sessions on this board at once: each caller sees why (`Network chat
is no longer offered on this board.` or `This board left the network`) and
their next line ends chat rather than being posted. Any other change to
the subscription restarts its connection, which also ends open chat
sessions, with `This board's network connection was reset; use /network
to rejoin.` The CHAT command, and the leaf behind it, refuse to join or
post on a network whose switch is off whatever the caller's agreement
says.

**Status** shows a `CHAT` column per network: `off`, `checking...` until
the hub's feature list has been read, `hub does not serve it`, or `on`.

### Chat and Older Versions

A hub lists `central.chat` among its features once it runs this version.
Against a hub that does not, **Status** shows `hub does not serve it` and
the Central menu does not offer **Chat**; the main menu's `CHAT` command
still lists the network if the hub answers its room list, since older
hubs served chat without naming it. Update a hub before the boards that
chat on it to get the Central entry. A board from before this version
offers chat on every subscription too, with no agreement asked of its
callers; the hub treats both the same. A hub from before this version
lists a room's members by handle alone, so against it a board names
everyone in the room by handle, with no board.

### Moderation

Three parties have a say, and none overrides another's ground: a caller
over what they see, you over your own callers and your board, the hub's
operator over the network. Where two apply, the stricter wins.

**Callers** have `/mute`, `/block`, `/unblock` and `/ignored`, described
under [Mute, Block and Ignore](v3net/chat.md#mute-block-and-ignore). A
block is a Twit List entry, so it is kept and managed where the caller's
other Twit entries are.

**You** have two controls:

- **Net Chat** in the user editor (`N`), `Allowed` or `Barred`. A barred
  caller cannot join, post or send private messages on any network from
  this board, whatever they agreed to: `CHAT NET` says `your SysOp has
  turned it off`, `CHAT` runs local chat, a caller in a network room is
  taken out at their next line, and Preferences shows switch 4 as `Off
  (set by the SysOp)`. Their own agreement is kept, so lifting the bar
  gives them back what they chose. The flag is `centralChatBarred` on the
  account. This is your answer to a complaint about one of your callers,
  and what the hub's operator will expect you to use before they act.
- **Chat Block List** on the subscription (`central.chatBlocked`), a list
  of `handle@node-id`. Lines, private messages and join and leave notices
  from a listed caller are dropped by the leaf before any session here
  sees them, and their lines are left out of the history shown on a join.
  Topic changes are the room's and are kept. The handle is matched
  ignoring case, the node ID exactly: one board's caller, not every
  caller with that handle. It is your shield against a known abuser while
  the hub's operator is away, and costs the hub nothing.

You cannot mute or ban a caller on another board for anyone but your own
callers; that is the hub operator's, and the lever for it is a report.

**Reports and network-wide sanctions** (`/report`, hub-side mute, ban,
kick and room lock, with the network told why a voice stopped) are the
remaining parts of [#672](https://github.com/ViSiON-3/vision-3-bbs/issues/672)
and follow in their own releases; until then a complaint about a caller on
another board goes to that board's sysop or the hub's operator by other
means.

### Chat Retention and Access

Room history is kept by the hub for its retention period (7 days on a
ViSiON/3 hub) and served to callers who join a room. The hub's room list
and room history endpoints are readable without a node signature today,
by anyone who knows the hub's address, which is why the notice tells
callers to treat a room as public; a later phase decides and documents
that access policy. Private messages
are stored for the same period and are never replayed; see
[Private Message Delivery](v3net/chat.md#private-message-delivery). Mute
and block controls, a sysop's bar and block list are under
[Moderation](#moderation); reports and network-wide sanctions are later
parts of #672.

## Activity Alerts

Activity alerts are short live notices, shown as
[toasts](users/login-sequence.md#toasts), of what callers do on the other boards of a
network:

| Kind | Sent when | Example |
|---|---|---|
| Logon | a caller finishes logging on | `Alice on Example BBS logged on (examplenet)` |
| Logoff | a caller's session ends after login | `Alice on Example BBS logged off (examplenet)` |
| Chat | a caller joins a network chat room | `Alice on Example BBS entered network chat (lobby on examplenet)` |
| Post | a caller saves a post in one of the network's message areas | `Alice on Example BBS posted in General on examplenet` |

An alert names the caller by handle and their board by name, so two
callers with one handle on different boards read differently. A chat
alert names the room; a post alert names the area as *your* board names
it. An alert never carries a message, its subject, private mail, a
private chat message, or anything about a caller's account beyond the
handle.

Everything here is off until you and the caller both turn it on. Offering
Who's Online, Last Callers or chat, a caller sharing their presence,
agreeing to chat, or **Callers Default** never sends an alert or
subscribes anyone to them, and upgrading changes nothing.

### Choosing Alerts

Callers choose under **V3Net Central → Preferences → [5] Activity
alerts**, also reached through **V3Net Sharing** on Your Konfig. The entry
appears only while some network offers an alert. The screen has two
columns of switches:

```
V3Net Central: Activity Alerts
───────────────────────────────────────────────────────────────────────────────
  Alerts are short live notices of what callers do on other boards.
  Sharing and seeing are separate; each switch starts off.

  Tell other boards when...     Show me when, on other boards...
  [1] I log on             Off   [5] someone logs on        Off
  [2] I log off            Off   [6] someone logs off       Off
  [3] I join network chat  Off   [7] someone joins net chat Off
  [4] I post a message     Off   [8] someone posts          Off
```

- **Tell other boards** (1-4) is the caller's permission to announce what
  they do here. **Show me** (5-8) is their subscription to what callers
  elsewhere do. The two are independent: subscribing announces nothing,
  and announcing subscribes to nothing.
- Each kind has its own pair, so logons and logoffs are chosen apart.
- A kind no network offers shows `n/a` and cannot be turned on. One left
  on after you stop offering it shows `On*` and can still be turned off.
- The choices are stored on the account (`centralAlerts` in `users.json`)
  and read at the moment they matter: turning a switch off on one node
  stops the next alert from or to that caller at once, on every node.
- An invisible login is never announced, and neither is a private or
  anonymous post.
- Post alerts cover posts and replies written in the BBS's message
  editor, and replies uploaded in a QWK packet, from the BBS or through
  the QWK API. A packet sends one alert per V3Net area it posted to, after
  the upload, and at most five, however many replies it holds; its
  private mail is never announced. Posts made with `helper post` or a
  script have no caller to ask, and are not announced.

### Offering Alerts

Each kind is its own switch on the subscription, below **Chat Block
List** in the editor:

| Editor field | JSON (`central.alerts`) | Default | Effect |
|---|---|---|---|
| **Alert Logons** | `logons` | N | Logon alerts on this network. Needs **Central**. |
| **Alert Logoffs** | `logoffs` | N | Logoff alerts on this network. Needs **Central**. |
| **Alert Chat** | `chat` | N | Chat join alerts on this network. Needs **Central** and **Lobby Chat**. |
| **Alert Posts** | `posts` | N | Post alerts for this network's message areas. Needs **Central**. |

A switch covers both directions on its network: on, this board announces
its sharing callers' actions of that kind and shows the other boards'
alerts of that kind to its subscribed callers; off, it does neither,
whatever callers chose. A caller's choice cannot override it.

Who sees an alert is decided on the receiving board, from each caller's
stored record:

- only callers who are past login and subscribed to its kind;
- a post alert only where this board carries the area on that network,
  and only to callers who pass the area's read ACS and its conference's
  ACS, each checked against the caller's own connection as the menus do;
- a chat alert not to a caller you have barred from network chat;
- nothing from a caller on the viewer's Twit List, or on your **Chat
  Block List**;
- nothing from your own board: your callers already see your local
  [logon and logoff toasts](users/login-sequence.md#toasts).

The four lines are strings you can change in the string editor
(**Alert: Network Logon**, **Logoff**, **Chat** and **Post**;
`centralAlertLogon`, `centralAlertLogoff`, `centralAlertChatJoin` and
`centralAlertPost` in `strings.json`). Each takes the handle, the board
and the network, with the room or the area before the network for chat
and posts. A blank string shows no alert of that kind.

### How Alerts Work

The board where a caller acts writes the alert, signs it with its node
key, and posts it to the hub. The hub checks the signature against the
board's registered key, as it does a listing, and relays the alert as
signed to every board on the network as a `central_alert` event; each
board checks the signature again before showing it, so a hub cannot alter
an alert or send one in another board's name. As with listings, a board is
known by its key, so a hub that set out to deceive its network could still
invent a board of its own and alerts from it, unless the network's
coordinator publishes a [board roster](#board-roster). One action on a board
in several networks
goes to each network with one ID, and a board on several of them shows it
once.

Alerts are news, not records:

- An alert is valid for **two minutes** (a hub refuses one longer than
  five) and is dropped once it expires, wherever it is. A board that
  cannot reach its hub retries the same alert until shortly before then,
  then gives up rather than send it late.
- The hub stores no alert. It remembers the IDs it has relayed until
  they expire, so a retry after a lost reply, or a replayed alert, is
  acknowledged and not sent again; each board also drops an ID it has
  seen.
- There is **no inbox and no catch-up**. An alert waits for a caller who
  cannot be shown it yet (one in a door, say) for at most 60 seconds, and
  is never queued for a later login. A board that reconnects to its hub,
  or a caller who reconnects, is sent nothing that happened meanwhile.
- A logoff alert comes only from a caller's own session ending on their
  board. A board losing its link to the hub, or the hub dropping a
  board's presence, never produces one.
- A failed action announces nothing: a chat alert follows a successful
  join, and a post alert a successful save. Rejoining after a reconnect,
  network message imports and presence refreshes send none.

A hub relays at most 30 alerts in a burst, then one a second, per board
and network.

### Alerts and Older Versions

A hub lists `central.alerts` among its features once it runs this
version. A board sends and shows alerts on a network only when its hub
names it; against an older hub the switches do nothing and **Activity
alerts** is not offered for that network. Boards from before this
version neither send nor show alerts, and nothing else changes for them.

## Board Roster

Every Central listing, activity snapshot and alert is signed by the board
it is about, so a hub cannot change one or send one in another board's
name. But a board is known only by its key, and nothing else on a network
vouches for keys: a hub operator who set out to deceive could make up a key,
and with it a board, its listing, its callers and their alerts. A board
roster closes that gap with the one key on a network the hub does not hold:
the coordinator's, which every board pins the first time it verifies the
network's NAL.

The roster is the coordinator's signed list of the boards (node ID and
public key) it vouches for. A board that holds one shows Central listings,
Who's Online, Last Callers and activity alerts only from boards on it,
matched on the full public key; the others are dropped as though they were
not there. Its own board always shows. A network whose coordinator has
never published a roster works as before, showing every board.

It protects only where the coordinator is not the hub's operator: a hub
operator who is also the coordinator holds both keys.

It covers network chat too. Boards sign every room line, room join and
private message they send, and the hub passes the signature on with it;
while a board holds a roster it shows a line, join or private message only
when it is signed by a board on the roster and says what the hub says it
does, and a live one only if it is fresh and not seen before. Room history
is checked the same way when a caller joins, and the room's member list
leaves out boards not on the roster. Leaves and topic changes are shown as
the hub reports them. Boards from before signed chat send their lines
unsigned: while a roster is in force, other boards do not show them, so
upgrade the network's boards before publishing a roster if they chat.

### Publishing a Roster

The coordinator keeps the roster under **V3Net → Coordinator Panel → [B]oard
roster**:

```
V3Net: Board Roster — felonynet
───────────────────────────────────────────────────────────────────────────────
  2 boards on the roster. Boards that hold it show Central only from these.

  #  ROSTER  BOARD                          NODE ID
  1  on      Alpha BBS                      a1b2c3d4e5f60718
  2  on      Hub BBS                        0f1e2d3c4b5a6978 (this node)
  3  -       Gamma BBS                      9988776655443322
```

The screen lists the roster's boards and the directory's other boards,
with each board's node ID. `A` and a row number admits a board, `R` and a
row number removes one, and `K` admits a board by its public key, for one
that publishes no listing. Each change is signed and published at once.

The first roster asks before it goes out, and always names your own board:
once boards take a roster they keep it.

To stop filtering, open the network with `O`: after a yes, you publish the
roster marked open, and boards that take it show every board again. They
keep holding it, so a hub cannot put an older, filtering roster back, and
the boards on it stay on it: `C` closes the network again and boards filter
by them once more. You can admit and remove boards while it is open, ready
for closing it. Boards from before opening existed cannot read an open
roster and go on filtering by the last one they took.

Admit a board only once you have checked its key through a channel the
hub does not control: ask its sysop for the key (or its node ID) by mail,
in person, or on another network, and compare it with the one on screen. A
signed listing proves only that whoever holds the key wrote it; a dishonest
hub can make up a key and sign a listing with it, and the directory you
choose from is served by that hub. The roster is only as good as these
checks.

Each edit is made from the roster on screen. If the roster changed in the
meantime (from another session of yours, say), the edit is refused rather
than undoing the other change, and the screen shows the roster as it now
is.

### How Boards Use It

A board takes a roster only when it is signed by the coordinator key its
subscription pins, and only a newer one replaces it; a roster from a new
coordinator, after a handover moved the pin, replaces the old
coordinator's. The board fetches it at start, when the hub announces a new
one, and after its event stream reconnects, and keeps the one it took in
`data/v3net_rosters/`, one file per subscription. So a restart keeps the
roster in force, and a hub that stops serving it or serves an older one
changes nothing; a board that sees its hub stop serving the roster it holds
says so on **Status**.

A board is protected only once it has taken a roster. Until then (a board
new to the network, or one that has never fetched since you first published)
it shows every board, and a hub that withholds the roster from it keeps it
that way. Tell your network's sysops when you publish the first roster, and
ask them to check **Status** shows it `on`.

**Status** shows a `ROSTER` column per network: `on, N boards`, `open (all
shown)`, `none (all shown)`, or `hub does not serve it`. A hub lists `central.roster` among its
features once it runs this version; boards from before it ignore rosters
and show every board, as they did.

---

## Pilot Status

Phase 1 has been exercised end to end in automated tests: two boards on
one hub publish, discover and update each other's listings, a withdrawal
removes a listing, a replayed event is ignored, a hub-altered listing is
dropped by the leaf, and a leaf on a hub without the feature hides it.
Evidence from a volunteer multi-board pilot on a live network is still to
be gathered before Central is offered beyond development use; the pilot is
tracked on the epic, [#669](https://github.com/ViSiON-3/vision-3-bbs/issues/669).

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
network is still to come, tracked on the epic,
[#669](https://github.com/ViSiON-3/vision-3-bbs/issues/669).

Activity alerts have been exercised in automated tests: two boards on one
hub exchange all four kinds, verified and naming the right board, caller,
room and area; the sending board does not alert itself; a retry reaches
no one twice; a board whose sysop has a kind off neither sends nor shows
it; expired, forged and blocked alerts are dropped; a board that cannot
reach its hub gives up once the alert would be stale. On the BBS side,
only subscribed callers past login see an alert, a withdrawn choice
stops the next one, post alerts follow the area's read ACS, and the
Twit List and chat bar apply. A live multi-board pilot is still to come.

---

## Related Documentation

- [V3Net Configuration](v3net/configuration.md)
- [Manual Configuration Reference](v3net/manual-config.md)
- [V3Net Chat](v3net/chat.md)
- [Menu Commands: V3Net Central](reference/menu-commands.md#v3net-central)
