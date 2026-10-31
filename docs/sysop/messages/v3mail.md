# v3mail — Vision3 Mail Utility

`v3mail` is the built-in command-line tool for managing JAM message bases and network mail on a ViSiON/3 BBS, the descendant of ViSiON/2's `V2MAIL`. It combines message base maintenance (stats, defragmentation, purging, integrity checks, reply-thread linking) with a complete FTN echomail tosser/scanner/packer that replaces external tools like HPT/Husky.

## Commands

### JAM Base Commands

| Command    | Description                                                                  |
| ---------- | ---------------------------------------------------------------------------- |
| `stats`    | Display message counts, base sizes, and metadata for one or all areas        |
| `pack`     | Defragment a base — physically removes deleted messages and compacts storage. Surviving messages are renumbered from 1, and each user's lastread pointer is moved with them so it keeps naming the same message rather than leaving unread mail looking read. Reply threads are then relinked against the new numbers, so no separate `link` run is needed |
| `purge`    | Delete messages exceeding per-area `max_messages` or `max_age` limits        |
| `fix`      | Verify base integrity; use `--repair` to automatically fix corrupt headers. A repair that rebuilds the base also relinks its reply threads, so no separate `link` run is needed |
| `link`     | Build reply-thread chains (`ReplyTo` / `Reply1st` / `ReplyNext` JAM fields)  |
| `lastread` | Show or reset per-user lastread pointers                                     |
| `readdress` | Address stored private mail to the recipient's handle; `--dry-run` reports without writing (see below) |

### Readdressing private mail

A private message can be read only by the account whose **handle** is in its
To or From field; a real name never grants access, because anyone can change
theirs and two users can share one. Netmail and QWK mail often arrive addressed
to a real name or to `Sysop`, so `toss`, `qwk-toss`/`qwk-poll` and QWK reply
(REP) uploads now store such mail with the recipient's handle. They resolve the
name on the message, ignoring case, to:

1. the user with that handle;
2. user #1, for `Sysop`;
3. the one user with that real name. If two or more users share it, the name
   is ambiguous and the mail is left as it came.

Deleted accounts never receive mail. Echomail and other public messages are
never changed. `toss` and the QWK commands read `users/users.json` under `--data`; if
it cannot be read they log a warning and store To as received.

Mail stored before this — or that named no account — is not addressed to any
handle. The sysop (access level at or above `sysOpLevel`) can read such
**undeliverable** private mail in the message reader, list and newscan; apart
from its sender, nobody else can. `v3mail readdress` fixes the mail that can be fixed: for every private
message whose To is not already a user's handle, it applies the same resolution
and rewrites To (and the base's index, so the new-mail and "to you" counts see
it). It reports how many it readdressed and how many it left undeliverable or
ambiguous. It needs `users/users.json` (it will not create one), changes nothing on a
second run, and writes nothing with `--dry-run`:

```bash
./v3mail readdress --all --dry-run   # see what would change
./v3mail readdress --all
./v3mail readdress data/msgbases/fsx_netmail
```

The rewritten headers leave their old copies behind in the `.jhr` file; the next
`v3mail pack` reclaims the space.

### Poll

`v3mail poll` sends and fetches mail for every enabled network in one step.
It is the command to reach for when a network looks quiet and the next
scheduled poll is too long to wait. The sysop can run the same thing from the
admin menu with **M** (Poll Mail Networks).

For FTN networks it:

1. scans and packs outbound mail, so anything waiting goes out on this call;
2. calls each hub link that has a hostname with
   `binkd -p -P <hub>@<network> data/ftn/binkd.conf`, the same call the
   scheduled poll events make (links without a hostname are listed, since
   those hubs can only deliver when they call in);
3. tosses what arrived, for every network, because networks can share an
   inbound.

For QWK networks it runs the same exchange as `qwk-poll`.

| Option            | Description                                                            |
| ----------------- | ---------------------------------------------------------------------- |
| `--network NAME`  | Only this network. The name can be an FTN or a QWK network key         |
| `--ftn-only`      | Poll FTN networks only                                                 |
| `--qwk-only`      | Poll QWK networks only                                                 |
| `--timeout DUR`   | Longest a single binkd call may take (default `5m`)                   |
| `-v`              | Show binkd's session output instead of a one-line summary per hub      |

Each hub gets a line such as `[fsxnet] 21:1/100: sent 1, received 2 file(s)`.
A failed call prints binkd's last lines; the full session is in
`data/logs/binkd.log`. The exit status is 1 if any step failed.

Only one `v3mail poll` runs at a time (the lock is `data/v3mail_poll.lock`); a
second one exits straight away saying another poll is running. Separately,
every FTN toss, scan and pack takes `data/ftn/ftn_mail.lock`, whether it comes
from a poll, the BBS's export cycle or a `v3mail` command (binkd runs
`v3mail toss` after each session). So a poll that overlaps any of those waits
its turn instead of exporting or importing the same mail twice. A run that
waits more than two minutes gives up with an error and leaves the work for
the next run. Interrupting a poll (Ctrl-C or SIGTERM) stops the binkd call
in progress, skips the remaining hubs and still tosses what arrived.

From the admin menu, **ESC** or **Q** stops the poll, and so does
disconnecting. It also stops
after 10 minutes plus 5 per hub it can call (or plus the `--timeout` given on
the menu entry per hub).

### FTN Echomail Commands

| Command    | Description                                                                |
| ---------- | -------------------------------------------------------------------------- |
| `toss`     | Unpack inbound ZIP bundles and toss `.pkt` files into JAM message bases, storing each message as received; deliver inbound file echo files (`.TIC`) into their file areas and compile nodelists among them; with **Auto-Create** on, create areas for unknown echoes |
| `scan`     | Scan JAM bases for new outbound echomail and netmail and create staging `.pkt` files; From, To and Subject go out as CP437 unless the message declares `CHRS: UTF-8` |
| `ftn-pack` | Pack staged `.pkt` files into ZIP bundles for binkd; writes BSO flow files |

### QWK Network Commands

For boards that are nodes on a QWK-based network such as DOVE-Net. See
[QWK Networking](messages/qwk-networking.md).

| Command           | Description                                                                                   |
| ----------------- | --------------------------------------------------------------------------------------------- |
| `qwk-poll`        | Full exchange with each hub: toss pending packets, pack new posts, upload the REP, download the QWK, toss it |
| `qwk-scan`        | Pack new posts from `qwknet` areas into `<HUBID>.REP` without connecting                     |
| `qwk-toss`        | Import `.QWK` packets waiting in the inbound directory without connecting                    |
| `qwk-conferences` | Print the hub's conference numbers and names (`--network` required)                          |

### AreaFix and other `helper` commands

Subscribing to echoes, importing echo lists, compiling nodelists and posting
from the command line are `helper`'s job, not `v3mail`'s. `helper areafix`
writes the AreaFix netmail into `outbound_path`, and the next `v3mail ftn-pack`
(or the integrated mailer's export cycle) sends it:

```bash
./helper areafix --network fsxnet --command "%LIST"
```

See [Send AreaFix Commands](messages/ftn-echomail.md#step-3b-send-areafix-commands),
[FTN Nodelists](messages/nodelists.md), and
[Posting from the Command Line](messages/message-areas.md#posting-from-the-command-line).

## Usage Examples

```bash
# JAM base maintenance
./v3mail stats --all
./v3mail pack --all
./v3mail purge --all
./v3mail fix --repair --all
./v3mail link --all
./v3mail readdress --all --dry-run

# Operate on a single area
./v3mail stats data/msgbases/fsx_gen
./v3mail purge data/msgbases/fsx_gen

# FTN echomail workflow
./v3mail toss --config configs --data data
./v3mail scan --config configs --data data
./v3mail ftn-pack --config configs --data data

# Limit to one network
./v3mail toss --network fsxnet

# Send and fetch mail for every network now
./v3mail poll
./v3mail poll --network fsxnet
./v3mail poll --qwk-only
```

## How Echomail Flow Works

```text
Inbound:
  binkd → secure_in/ → v3mail toss → JAM bases → Vision/3 users
                                   → file areas (file echoes, by .TIC)
                                   → data/ftn/nodelist/ (nodelists among them)

Outbound:
  Users post → JAM bases → v3mail scan → temp_out/*.pkt
                         → v3mail ftn-pack → out/NNNNFFFF.DOW0 (+ .clo)
                         → binkd picks up → Hub
```

1. binkd receives a bundle from your hub and places it in `inbound_path`
2. `v3mail toss` extracts the bundle, parses each `.pkt`, and writes messages into the correct JAM bases; adds your node address to SEEN-BY and PATH (a point system leaves both lines alone, in both directions, since its net/node is the boss's address and the boss would reject the mail as a loop); detects duplicates via `data/ftn/dupes.json`
3. Users read and reply to messages in Vision/3
4. `v3mail scan` reads new messages from JAM bases (using a per-base high-water mark stored in each area's `.jlr` file under the `v3mail` scanner user) and creates outbound `.pkt` files in `outbound_path`
5. `v3mail ftn-pack` bundles the `.pkt` files into BSO ZIP archives in `binkd_outbound_path`; if link `flavour` is `Crash`, a `.clo` flow file is written to trigger an immediate binkd call
6. binkd transmits the bundle to the hub

## Nightly Maintenance Sequence

The recommended nightly sequence (configured via the event scheduler):

| Time  | Command                     | Purpose                               |
| ----- | --------------------------- | ------------------------------------- |
| 02:00 | `v3mail fix --repair --all` | Check and repair JAM base integrity   |
| 02:15 | `v3mail purge --all`        | Remove messages past age/count limits |
| 02:30 | `v3mail pack --all`         | Defragment and compact all bases      |

## Event Scheduler Integration

`v3mail` is designed to run as scheduled events. See `configs/events.json` and [Event Scheduler](advanced/event-scheduler.md) for full examples. A typical configuration:

> **Integrated mailer alternative:** the scheduler-driven `toss`/`scan`/`ftn-pack`
> workflow below is one way to run FTN mail. If you enable the integrated
> **binkd** mailer instead (Server Setup → **Binkd Mailer** — see
> [FTN Echomail](messages/ftn-echomail.md#enabling-the-integrated-mailer-recommended)),
> Vision/3 tosses inbound mail automatically via binkd's `exec` hook and
> exports outbound mail on a timer, so these `binkd_poll`/`v3mail toss`/
> `v3mail scan`/`v3mail ftn-pack` scheduler events should be **disabled** to
> avoid double-exporting.

```json
{
  "id": "v3mail_toss",
  "command": "{BBS_ROOT}/v3mail",
  "args": ["toss", "--config", "{BBS_ROOT}/configs", "--data", "{BBS_ROOT}/data"],
  "schedule": "@hourly"
}
```

## Troubleshooting

**Messages not appearing after toss**
Run `v3mail toss` manually and check stdout for `WARN` or `ERROR` lines. Verify `configs/ftn.json` has the correct `inbound_path` and that the echo area tag in the bundle matches an area in `configs/message_areas.json`.

**Outbound mail not leaving**
Run `v3mail scan` then `v3mail ftn-pack` manually. Check that `binkd_outbound_path` has a `.zip` bundle and (for Crash links) a `.clo` flow file. Verify binkd is configured to watch that directory.

**Messages landing in BAD area**
The echo area tag in the inbound packet does not match any area in `configs/message_areas.json`. Either add the area or update `bad_area_tag` in `configs/ftn.json` to route these to a catchall area.

**High duplicate rate**
Search `v3mail.log` for `dupe message`: each line names the echo, MSGID, packet, sending link and when the message was first seen. Dupes from a second uplink, or a bundle resent after a dropped session, are normal. Set `dupe_area_tag` to keep a copy of every dupe for inspection. The size of `data/ftn/dupes.json` is not a dupe count: it holds every message seen in the last 30 days.

**File echo files not arriving in their area**
The toss prints an error for each TIC it could not deliver, saying why, and
moves the TIC and its file to `temp_path/badtic`: usually no file area linked
to the echo, or a `tic_password` mismatch. Fix the cause, move both files back
into the inbound and toss again. See
[Undeliverable TICs](files/file-echoes.md#undeliverable-tics).

**Nodelist not compiled**
Check that the network's `nodelist.file_echo` matches the echo's tag and that
`nodelist.file_pattern` matches the file's name in the area. A file that
matches the pattern but cannot be compiled is a toss error saying why (not a
nodelist, a nodediff, or another zone's list). An older list than the one
already compiled is skipped and logged. Compile one by hand with
`helper nodelist import`. See [FTN Nodelists](messages/nodelists.md).

## Technical Reference

### Global options

```text
--all           Operate on all areas defined in configs/message_areas.json
--config DIR    Path to config directory (default: configs)
--data DIR      Path to data directory (default: data)
-q              Quiet mode — suppress informational output
```

A JAM base command takes either `--all` or one or more base paths
(`data/msgbases/fsx_gen`).

### Per-command options

| Command | Option | Meaning |
| ------- | ------ | ------- |
| `pack` | `--dry-run` | Report what would be removed without modifying |
| `purge` | `--days N` | Delete messages older than N days in areas with no `max_age` of their own (with `--all`) |
| `purge` | `--keep N` | Keep only the newest N messages in areas with no `max_messages` of their own (with `--all`) |
| `purge` | `--dry-run` | Report without deleting |
| `fix` | `--repair` | Attempt to repair the problems found |
| `lastread` | `--reset USER` | Reset the lastread pointer for this user |
| `readdress` | `--dry-run` | Report what would change without writing |
| `toss`, `scan`, `ftn-pack` | `--network NAME` | Restrict to a single FTN network (default: all networks with `internal_tosser_enabled: true`) |
| `qwk-poll`, `qwk-scan`, `qwk-toss`, `qwk-conferences` | `--network KEY` | Restrict to one network key from `qwknet.json` (default: every network with `"enabled": true`). The named network runs even when disabled |
| `poll` | `--network`, `--ftn-only`, `--qwk-only`, `--timeout DUR`, `-v` | See [Poll](#poll) |

Per-area `max_messages` and `max_age` always take precedence over `--days`
and `--keep`.

### Exit status and environment

`v3mail` exits `0` when the command completed and `1` when it did not: a bad
or missing command, a base that could not be opened, a toss, scan or pack
that reported an error, a QWK poll step that failed, or a `poll` in which any
hub or step failed. There are no other exit codes.

| Variable | Effect |
| -------- | ------ |
| `V3MAIL_NO_CONSOLE_LOG` | When set (to anything), log records go to `data/logs/v3mail.log` only and not to the console. The BBS sets it when it runs `v3mail` for a sysop (admin menu **M**, Poll Mail Networks) so the records do not bury the progress lines. |

Logging settings come from `configs/config.json` (`logging`); the log file is
`v3mail.log` in the configured log directory, `data/logs` by default.

### FTN configuration (`ftn.json`)

FTN behaviour is controlled by `configs/ftn.json`, which the Configuration Editor
(`./config` → **4 - Echomail Networking**) maintains. The full key reference,
including the integrated `binkd` block, is in
[FTN Echomail](messages/ftn-echomail.md#technical-reference); the keys `v3mail`
itself reads are:

Global fields (top-level):

| Field                 | Description                                                     |
| --------------------- | --------------------------------------------------------------- |
| `dupe_db_path`        | MSGID dupe database (`toss`); MSGID-less mail is keyed by a content hash |
| `inbound_path`        | Directory binkd delivers inbound bundles to                     |
| `secure_inbound_path` | Directory for password-authenticated sessions                   |
| `outbound_path`       | Staging directory for outbound `.pkt` files (`scan` output)     |
| `binkd_outbound_path` | binkd BSO outbound directory (`ftn-pack` output)                |
| `temp_path`           | Temporary directory for bundle extraction                       |
| `bad_area_tag`        | JAM area tag for messages with unknown echo tags (e.g. `"BAD"`) |
| `dupe_area_tag`       | JAM area tag for duplicate MSGIDs (e.g. `"DUPE"`)               |

Per-network fields (`networks.<key>`):

| Field                       | Description                                                     |
| --------------------------- | --------------------------------------------------------------- |
| `own_address`               | This node's FTN address (zone:net/node.point)                   |
| `internal_tosser_enabled`   | Set `true` to enable `v3mail` for this network                  |
| `origin`                    | Origin line text (empty = board name)                           |
| `binkd_outbound_path`       | Optional: this network's own BSO outbound (`ftn-pack` output); empty = the global one. Use one per network when carrying several. |
| `nodelist`                  | Optional: `file_echo` and `file_pattern` naming the file echo and files the network's nodelist arrives as; `toss` compiles each one into `data/ftn/nodelist/<network>.json`. See [FTN Nodelists](messages/nodelists.md). |
| `auto_create`               | Optional: `toss` creates an echomail area for an unknown echo instead of sending the mail to the bad area, with the ACS, purge limits, auto-join and real-name settings given. Only for packets from a configured link that carry its password or arrived in the secure inbound. See [Auto-creating echo areas](messages/ftn-echomail.md#auto-creating-echo-areas). |

Hub polling is scheduled through **Events**, using the per-network
`echomail_poll_<network>` event created by the FTN wizard. See
[FTN mail polling](advanced/event-scheduler.md#ftn-mail-polling-binkd).
The obsolete `poll_interval_seconds` field is ignored when loading older configs.

Per-link fields (`networks.<key>.links[]`):

| Field                  | Description                                                     |
| ---------------------- | --------------------------------------------------------------- |
| `address`              | Link's FTN address                                              |
| `packet_password`      | Packet password shared with hub (empty for no auth)             |
| `areafix_password`     | Password for AreaFix netmail (subject line; set by hub)         |
| `tic_password`         | Password inbound file echo TICs from this link must carry       |
| `filefix_password`     | Password for FileFix netmail (subject line); empty = `tic_password` |
| `filefix_name`         | Hub's file echo robot name (default `FileFix`)                  |
| `name`                 | Human-readable label for this link                              |
| `flavour`              | Delivery mode: `Normal` (default), `Crash`, `Hold`, `Direct`    |
| `hostname`, `port`     | Hub BinkP host and port; `poll` calls links that have a hostname |
| `ip_family`            | `ipv4` / `ipv6` pins the family binkd calls the hub over (binkd `-4` / `-6`); empty = auto |
| `session_password`     | BinkP session password, written to `binkd.conf`                 |

## See Also

- [FTN Echomail](messages/ftn-echomail.md) — End-to-end FTN setup guide
- [FTN File Echoes](files/file-echoes.md) — Inbound TIC processing during `toss`
- [FTN Nodelists](messages/nodelists.md) — Nodelist compiling during `toss`, and `helper nodelist`
- [JAM Echomail](messages/jam-echomail.md) — JAM message base internals
- [Event Scheduler](advanced/event-scheduler.md) — Scheduling v3mail commands
- [Message Areas](messages/message-areas.md) — Configuring message areas
