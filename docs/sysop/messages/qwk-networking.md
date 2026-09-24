# QWK Networking (QWKnet)

A QWK network moves conference mail between bulletin boards in the same
packets offline readers use. ViSiON/3 can join one as a **node**: it packs new
posts from its networked areas into a `.REP` packet, uploads that to the hub,
downloads the hub's `.QWK` packet, and tosses the messages it contains into the
local message bases. The conventions are Synchronet's, which defined how modern
QWK networks work, so a ViSiON/3 node interoperates with Synchronet hubs such
as DOVE-Net and with any other system that speaks the same dialect.

This is separate from [QWK Offline Mail](messages/qwk.md), which serves packets
to your *users*. Both share the same packet codec and the same BBS ID.

## How It Works

```text
Hub FTP server <--upload <HUBID>.REP--   data/qwknet/out/   <-- v3mail qwk-scan   <-- JAM bases <-- users post
               --download <HUBID>.QWK--> data/qwknet/in/    --> v3mail qwk-toss   --> JAM bases --> users read
```

1. **Identity.** Your node is known to the hub by its **QWK ID** (System →
   Registration → QWK ID, or derived from the board name). That ID is also the
   user name the node logs in with on the hub's FTP server. The hub itself has
   a QWK ID (DOVE-Net's hub is `VERT`), which names the packets on the wire:
   you upload `VERT.REP` and download `VERT.QWK`.
2. **Routing.** The hub numbers its conferences. Each local message area that
   mirrors one records that number in its `qwk_conference` field. Every message
   in a packet carries a conference number, and that number is the only thing
   either side routes on. Conference names are for people; numbers are for
   software.
3. **Scan.** `v3mail qwk-scan` walks each `qwknet` area on the network, takes
   every message not yet exported, and writes them into `<HUBID>.REP` in the
   outbound directory. Each area keeps an export high-water mark, and each
   exported message is stamped as processed, so nothing is sent twice.
4. **Poll.** `v3mail qwk-poll` logs in to the hub, uploads the waiting REP (and
   deletes the local copy once the hub accepted it), downloads the hub's QWK
   packet into the inbound directory, and tosses it.
5. **Toss.** `v3mail qwk-toss` opens each waiting packet, maps every message's
   conference number to a local area, drops duplicates and routing loops, and
   writes the rest into JAM. Imported messages are marked processed on arrival,
   so the next scan never sends the hub its own mail back.

### What travels in a packet

Beyond the 128-byte QWK header (25-character names, MM-DD-YY dates), each
message carries the extensions Synchronet defined:

| Carrier | Fields |
|---------|--------|
| `HEADERS.DAT` | Full-length `Sender`, `To`, `Subject`; `Message-ID`; `Reply-ID`; `WhenWritten` with time zone; `SenderNetAddr` (the originating QWK ID); `Utf8`; `Conference` |
| Body kludges | `@MSGID:`, `@REPLY:`, `@TZ:` at the top of the body, for hubs that ignore `HEADERS.DAT`; `@VIA:` on relayed mail, listing the systems it has passed through |
| Tearline | Outbound messages get a `---` tearline and a ` ■ ViSiON/3 ■ <tagline>` line, unless the message already has one |

On import, `HEADERS.DAT` wins over the body kludges, which win over the short
header fields. Dates come from `WhenWritten` (with its offset), else from the
header date in the `@TZ` zone, else UTC. A packet flagged `Utf8` is converted to
CP437 for storage. A message whose `@VIA` route already names your own QWK ID
has been through your system before and is dropped.

## Prerequisites

- A **QWK ID** for your board. Set it under System → Registration → QWK ID in
  the config editor, or `qwkID` in `configs/config.json`. Leave it blank and
  ViSiON/3 derives one from the board name (letters and digits only, max 8,
  upper-cased). Set it explicitly and early: the hub knows you by it, and
  changing it later means a new hub account.
- An **account on the hub**. For DOVE-Net: telnet to `vert.synchro.net`, log on
  as a new user whose **name is your QWK ID**, and answer **Yes** to "Is this
  account to be used for QWK Networking (DOVE-Net)?". The password you choose
  is the hub password. Other networks have their own sign-up process; ask the
  hub operator for a QWK-ID account and password.
- The hub's **FTP host and port** (DOVE-Net: `vert.synchro.net`, port 21) and
  its **QWK ID** (`VERT`).

## Joining a Network with the Wizard

Run `./config`, choose **4 - Echomail Networking**, then **QWK Network Wizard**.

The form asks for:

| Field | What to enter |
|-------|---------------|
| Network | Press Enter to browse known networks. Pick **DOVE-Net** to preload the hub (`VERT`, `vert.synchro.net`, port 21) and its conference list, or **Custom** for any other hub. |
| Network Key | Short lowercase key (e.g. `dovenet`). It names the network in `qwknet.json`, in each area's `network` field, and in the poll event ID. |
| Hub QWK-ID | The hub's QWK ID (e.g. `VERT`). |
| Hub Host / Hub Port | The hub's FTP server. Port defaults to 21. |
| Your QWK-ID | Read-only: the ID the node will log in as, from System → Registration → QWK ID or derived from the board name. |
| Login Name | Leave blank to log in as your QWK ID, which is what Synchronet hubs expect. |
| Password | The hub account password. |
| Tagline | Added under the tearline of every message you send (e.g. `My BBS - bbs.example.org`). |
| Poll Schedule | Cron schedule for the poll event. Default `*/30 * * * *` (every 30 minutes). |
| Newscan Default | Y adds the new areas to users' newscan by default. |
| Conferences | Press Enter. For a known network the preset list appears at once; otherwise the wizard logs in to the hub, downloads its packet and reads the conference list from `CONTROL.DAT`. Space toggles a conference, A selects all, N clears, Enter confirms. |

Press **S** or **PgDn** to save. The wizard then:

1. Writes the network to `configs/qwknet.json`.
2. Creates a message **conference group** named after the network.
3. Creates one message area per chosen conference: tag `<KEY>_<conference-name>`,
   `area_type` `qwknet`, `network` set to the key, `qwk_conference` set to the
   hub's number, `echo_tag` set to the hub's conference name, ACS `s10` to
   read and `s20` to post.
4. Adds an event scheduler entry `qwknet_poll_<key>` that runs
   `{BBS_ROOT}/v3mail qwk-poll --network <key> --config {BBS_ROOT}/configs --data {BBS_ROOT}/data`
   on the schedule you chose, and enables the scheduler.

Re-running the wizard on an existing network loads its values so you can change
the hub details or add conferences. Areas you no longer tick are left alone;
remove them under Message Areas if you want them gone.

A packet the wizard downloaded to read the conference list is kept in the
inbound directory and tossed on the first poll, so no mail is lost by
browsing.

### Managing networks after setup

**4 - Echomail Networking → QWK Networks** lists the configured networks.
Enter edits one, **I** opens the wizard for a new one, **W** re-runs the
wizard on the highlighted network (to add conferences), **D** deletes, **G**
edits the global paths, ESC returns. The edit screen has these fields:

| Field | Meaning |
|-------|---------|
| Network Key | Renaming moves the network under a new key; areas and the poll event follow. |
| Name | Display name. |
| Enabled | Whether `qwk-poll` and `qwk-scan` include this network when no `--network` is given. |
| Hub QWK-ID, Hub Host, Hub Port | Where and who the hub is. |
| Login Name | FTP user name; blank means your QWK ID. |
| Password | Hub password. |
| Tagline | Line under the tearline of outbound messages. |
| HEADERS.DAT | Y (the default) sends full-length names, Message-IDs and time zones; N sends only the body kludges, for a hub that chokes on the file. |
| Timeout (secs) | Per-transfer FTP timeout. 0 means 300. |

Global (**G**): Inbound Path, Outbound Path, Temp Path, Dupe DB Path, Bad Area
Tag (a local area that catches mail for conferences you have not mirrored;
blank drops it with a log line).

## Manual Configuration

### `configs/qwknet.json`

```json
{
  "inboundPath": "data/qwknet/in",
  "outboundPath": "data/qwknet/out",
  "tempPath": "data/qwknet/temp",
  "dupeDbPath": "data/qwknet/dupes.json",
  "badAreaTag": "",
  "networks": {
    "dovenet": {
      "enabled": true,
      "name": "DOVE-Net",
      "hubId": "VERT",
      "ownId": "",
      "host": "vert.synchro.net",
      "port": 21,
      "username": "",
      "password": "hub-password",
      "tagline": "My BBS - bbs.example.org",
      "noHeaders": false,
      "timeoutSeconds": 300
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `inboundPath` | Downloaded `.QWK` packets waiting to be tossed. Relative paths are under the BBS root. |
| `outboundPath` | Packed `.REP` waiting to be uploaded. |
| `tempPath` | Scratch directory, created at startup. Packets are not staged here: each is built or downloaded beside its destination (a `.part` file in `inboundPath`, a `.REP.tmp` in `outboundPath`) so the final move never crosses filesystems. |
| `dupeDbPath` | JSON file of imported Message-IDs, kept 90 days. |
| `badAreaTag` | Tag of a local area that receives messages for conferences no area mirrors. Blank drops them with a log line, as Synchronet does; a hub sends every conference your account subscribes to, so unmapped ones are normal. |
| `networks.<key>.enabled` | Include this network in unqualified `qwk-poll` / `qwk-scan` runs. |
| `hubId` | The hub's QWK ID. Names the packets: `<hubId>.REP` up, `<hubId>.QWK` down. |
| `ownId` | Your QWK ID on this network. Blank uses the system QWK ID. |
| `host`, `port` | Hub FTP server. Port 0 or absent means 21. |
| `username` | FTP login. Blank uses `ownId` (or the system QWK ID). |
| `password` | Hub password. Required. |
| `tagline` | Appended below the tearline of every exported message. |
| `noHeaders` | `true` omits `HEADERS.DAT` from outbound packets. |
| `timeoutSeconds` | FTP transfer timeout. 0 means 300. |

The file holds a password, so it is written mode `0600`.

### `configs/message_areas.json`

An area joins a network by its type, network key and conference number:

```json
{
  "id": 40,
  "tag": "DOVENET_GENERAL",
  "name": "DOVE-Net General",
  "description": "General discussion",
  "acs_read": "s10",
  "acs_write": "s20",
  "conference_id": 5,
  "base_path": "msgbases/dovenet_general",
  "area_type": "qwknet",
  "network": "dovenet",
  "qwk_conference": 2001,
  "echo_tag": "DOVE-Net General",
  "auto_join": true
}
```

| Field | Description |
|-------|-------------|
| `area_type` | `qwknet` |
| `network` | The key from `qwknet.json` |
| `qwk_conference` | The hub's conference number. Required; an area without one is logged and ignored. Two areas on one network cannot share a number. |
| `echo_tag` | The hub's conference name. Informational only. |

To find the numbers, use the wizard's conference browser or
`v3mail qwk-conferences --network <key>`. DOVE-Net publishes its list at
<http://www.synchro.net/docs/dove-net.txt>.

Posts in a `qwknet` area are stored as echomail: they get a Message-ID of the
form `<time.areatag@yourqwkid>` when written, so replies coming back from the
network thread onto them. They do not get an FTN origin line; the tagline is
added at export time instead.

### The poll event

The wizard creates this entry in `configs/events.json`. Add it by hand if you
configured the network manually:

```json
{
  "id": "qwknet_poll_dovenet",
  "name": "Poll QWK Hub (VERT)",
  "schedule": "*/30 * * * *",
  "command": "{BBS_ROOT}/v3mail",
  "args": ["qwk-poll", "--network", "dovenet", "--config", "{BBS_ROOT}/configs", "--data", "{BBS_ROOT}/data"],
  "working_directory": "{BBS_ROOT}",
  "timeout_seconds": 600,
  "enabled": true
}
```

One poll does everything (scan, upload, download, toss), so no separate toss or
scan events are needed. Polling every 15 to 60 minutes is typical; hub
operators generally do not want nodes calling more often than that. See
[Event Scheduler](advanced/event-scheduler.md#qwk-network-polling-v3mail).

## `v3mail` Commands

| Command | What it does |
|---------|--------------|
| `v3mail qwk-poll` | Full exchange with each hub: toss anything left from last time, scan new posts into the REP, upload it, download the hub's packet, toss it. |
| `v3mail qwk-scan` | Pack new posts into `<HUBID>.REP` without connecting. |
| `v3mail qwk-toss` | Import the packets waiting in the inbound directory without connecting. |
| `v3mail qwk-conferences --network KEY` | Print the hub's conference numbers and names. |

All take `--config DIR` (default `configs`), `--data DIR` (default `data`) and
`--network KEY`. Without `--network`, poll, scan and toss run every enabled
network; with it, the named network runs even if disabled.

```bash
# Try the hub by hand
./v3mail qwk-poll --network dovenet --config configs --data data

# See what the hub offers before creating areas
./v3mail qwk-conferences --network dovenet
```

`qwk-poll` prints one summary line per network and exits non-zero if any step
reported an error. A hub outage does not lose mail: the REP stays in the
outbound directory and the next scan appends new posts to it.

## Files and Directories

| Path | Purpose | Managed by |
|------|---------|------------|
| `configs/qwknet.json` | Networks, credentials, paths | Sysop (config editor or by hand) |
| `configs/message_areas.json` | `qwknet` areas with their conference numbers | Sysop |
| `configs/events.json` | `qwknet_poll_<key>` events | Wizard, editable |
| `data/qwknet/in/<HUBID>.QWK` | Downloaded packets waiting to be tossed (a second one gets a timestamp suffix) | Automatic |
| `data/qwknet/in/*.bad` | Packets set aside after a failure | Sysop reviews, then deletes or renames back to `.QWK` to retry |
| `data/qwknet/out/<HUBID>.REP` | Packed messages waiting for upload | Automatic |
| `data/qwknet/out/<HUBID>.REP.bad` | A REP that could not be read back when new posts were to be appended | Sysop reviews |
| `data/qwknet/in/<HUBID>.QWK.*.part` | A download in progress; one left behind after a failed move into place is a complete packet | Automatic; if the log names one, rename it to `<HUBID>.QWK` to toss it |
| `data/qwknet/out/<HUBID>.REP.tmp` | A REP being rewritten | Automatic |
| `data/qwknet/out/<HUBID>.REP.sent` | An uploaded REP that could not be deleted, moved aside so it is not sent twice | Sysop deletes; check the outbound directory's permissions |
| `data/qwknet/dupes.json` | Imported Message-IDs, pruned after 90 days | Automatic |
| `<area>.jlr` (user `qwknet`) | Per-area export high-water mark inside each JAM base | Automatic |

## Operator Logging

`v3mail` writes to `logs/v3mail.log` through the structured logger. Lines worth
watching, each with `network` and usually `hub`, `area` or `path` fields:

| Level | Message | Meaning / action |
|-------|---------|------------------|
| INFO | `qwknet REP packed` | Scan wrote the REP; `new` and `pending` give the counts. |
| INFO | `qwknet REP uploaded` | The hub accepted the REP; the local copy is removed. |
| INFO | `qwknet QWK downloaded` | A packet arrived (`bytes`, `path`). |
| INFO | `qwknet hub had no packet for us` | Normal on a quiet network. |
| INFO | `qwknet tossed message` | One message written to `area`. |
| INFO | `qwknet toss complete` | Per-run totals: `imported`, `duplicates`, `unmapped`, `skipped`, `errors`. |
| INFO | `qwknet private mail not supported, skipped` | A conference-0 message (hub private mail). Not implemented. |
| WARN | `qwknet message for a conference no area mirrors` | The hub sent mail for a `conference` you have no area for. Create one with that `qwk_conference`, or ask the hub to stop sending it. Counted as `unmapped`. |
| WARN | `qwknet area has no conference number and cannot be routed` | A `qwknet` area with `qwk_conference` 0. Set it. |
| WARN | `two areas claim the same QWK conference; the first wins` | Fix the duplicate number. |
| WARN | `qwknet message already passed through this node, dropped` | Loop protection: the `@VIA` route named your QWK ID. |
| WARN | `packet ended early; importing what was readable` | Truncated or corrupt `MESSAGES.DAT`; the readable part was tossed. |
| WARN | `packet set aside for inspection` | The packet was renamed `.bad`: it was unreadable, came from another hub, or a message could not be written. Messages that did land are in the dupe database, so renaming it back to retry is safe. |
| WARN | `unreadable REP set aside` | The waiting REP could not be parsed to append to it; it was moved to `.REP.bad` and a fresh one written. |
| WARN | `failed to mark message exported` / `failed to advance export pointer` | The REP was written but the JAM base would not update; the message may be exported again next time. Check the base with `v3mail fix`. |

The poll's connection errors (`connect to hub`, `login refused`, `upload REP`,
`download QWK`) are printed by `v3mail qwk-poll` and returned in its exit code
rather than logged as separate lines.

## Troubleshooting

**`login refused: 530`**
The hub does not know your QWK ID or the password is wrong. On DOVE-Net the
account name must be exactly your QWK ID and the account must have been created
as a QWK networking account. Check System → Registration → QWK ID matches what
you signed up with.

**Poll succeeds, `hub had no packet for us` every time**
The hub has nothing new for the conferences your account is set up for, or the
hub has not yet linked your account to any conferences. Post something in a
networked area and poll again; a working link shows `REP uploaded`.

**Messages arrive but `unmapped` keeps counting up**
The hub sends conferences you have not created areas for. Run
`v3mail qwk-conferences --network <key>` and add areas for the numbers you
want, or accept the warnings.

**Your posts never appear on other systems**
Check that the area's `area_type` is `qwknet`, its `network` matches the
key in `qwknet.json`, and `qwk_conference` is set. `v3mail qwk-scan` reports
`packed 0 new` when nothing qualifies. A REP sitting in `data/qwknet/out`
means the upload is failing; the poll output says why.

**A message came through twice**
The hub sent it without a Message-ID and with different content or date each
time. Dupe detection falls back to a digest of conference, names, subject,
date and body when no ID is present.

**The wizard's conference list is empty**
For a custom hub the list comes from the packet the hub sends, and a hub with
no new mail may send none. Enter the conferences under Message Areas by hand,
or try again after posting a test message.

## Limitations

- **Node only.** ViSiON/3 does not host a hub: it cannot serve QWK packets to
  other nodes or accept their REPs.
- **Plain FTP.** No FTPS/TLS. Synchronet hubs accept plain FTP.
- **No private mail.** Conference 0 (hub-routed private mail) is skipped and
  logged. Public conferences only.
- **No polls or votes.** `VOTING.DAT` is ignored; vote records (status `V`)
  in `MESSAGES.DAT` are skipped.
- **CP437 storage.** UTF-8 packets are converted; characters outside CP437
  become `?`.

## See Also

- [QWK Offline Mail](messages/qwk.md) for the user-facing packet download and upload
- [FTN Echomail](messages/ftn-echomail.md) for the FidoNet-style alternative
- [Message Areas](messages/message-areas.md)
- [Event Scheduler](advanced/event-scheduler.md)
- [v3mail](messages/v3mail.md)
