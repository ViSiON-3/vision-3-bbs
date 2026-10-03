# FTN File Echoes (TIC)

A **file echo** is the file-area counterpart of an echomail area: a network
distributes files — weekly nodelists, infopacks, software — to every system
subscribed to the echo. Each file travels with a small `.TIC` control file
saying which echo it belongs to, its CRC, its description, and a password
agreed with the sending link.

ViSiON/3 receives file echoes. `v3mail toss` checks every `.TIC` in the inbound
directory and moves its file into the file area linked to that echo, with a
file record carrying the TIC's description. Sending files to a file echo
(hatching) and subscribing to file echoes by AreaFix-style netmail are not
supported yet.

## Before you start

File echoes ride on an FTN network that is already set up for echomail:
the network must be in `configs/ftn.json` with its hub as a link, and binkd
must be delivering into the inbound directory. See
[FTN Echomail](messages/ftn-echomail.md) if that is not done yet.

## Setting up file echo areas

### 1. Get the network's file echo list

Networks publish their file echoes as a list much like the echomail `.na`
file — tqwNet's is `tqw_file.na`. It comes in one of two shapes, and both
are accepted:

```text
Area TQW_NODE        0  !  Weekly Nodelists
Area TQW_LINUXFILES  0  !  Linux files
```

```text
TQW_NODE        Weekly Nodelists
TQW_LINUXFILES  Linux files
```

> Some networks name the file echo list `.na` too. `helper ftnsetup` refuses
> a file echo list rather than creating echomail areas from it; use
> `helper fileecho` for it instead.

### 2. Create the file areas with `helper fileecho`

```bash
./helper fileecho --na tqw_file.na --network tqwnet --acs-list s10 --acs-download s20
```

This adds one file area per echo to `configs/file_areas.json`, linked to the
echo, stored under `data/files/<network>/<echo>`. Echoes that already have a
linked area are left alone, so it is safe to re-run with a newer list. Use
`--dry-run` to preview.

| Flag                   | Required | Description                                                         |
| ---------------------- | -------- | ------------------------------------------------------------------- |
| `--na <path>`          | Yes      | The network's file echo list                                        |
| `--network <name>`     | Yes      | Network name in `ftn.json` the echoes come from                     |
| `--tag-prefix <pfx>`   | No       | Prefix for the new areas' tags                                      |
| `--conference-id <id>` | No       | Conference the new areas belong to (default: ungrouped)             |
| `--acs-list <acs>`     | No       | ACS to list files                                                   |
| `--acs-download <acs>` | No       | ACS to download files                                               |
| `--acs-upload <acs>`   | No       | ACS to upload (default `s250`: these areas are fed by the network)  |
| `--config <dir>`       | No       | Config directory (default: `configs`)                               |
| `--dry-run`            | No       | Show what would be added without writing anything                   |

A running BBS picks up the new areas once no callers are online.

To link an existing area by hand instead, set its **Network** and **File
Echo** fields in the Configuration Editor (File Areas), or its `network` and
`file_echo` in `file_areas.json`:

```json
{
    "id": 4,
    "tag": "LINUX",
    "name": "Linux files",
    "path": "tqwnet/tqw_linuxfiles",
    "network": "tqwnet",
    "file_echo": "TQW_LINUXFILES"
}
```

### 3. Set the TIC password

Most hubs put a password on the TICs they send (the `Pw` line). Ask your hub
for it and set it as the link's `tic_password` — Configuration Editor,
Echomail Links → **TIC Password**, or in `ftn.json`:

```json
"links": [
    {
        "address": "1337:1/100",
        "packet_password": "PKTPASS",
        "tic_password": "TICPASS",
        "name": "tqwNet hub"
    }
]
```

When `tic_password` is set, a TIC from that link without the matching
password (compared case-insensitively) is refused. When it is empty, TICs
from that link are accepted only from the **secure** inbound
(`secure_inbound_path`, binkd's `inbound`), which only sessions that passed
the link's session password can write to. A TIC's `From` line is just text,
so without either check anyone able to drop files in the unsecured inbound
could pose as your hub.

### 4. Subscribe at your hub

Ask your hub to send you the echoes, or use its file echo manager (often
called FileFix or AllFix) by netmail. Files start arriving with the next
poll.

### 5. Nodelists

If one of the echoes carries the network's nodelist, set the network's
**Nodelist Echo** and **Nodelist Files** so that each new nodelist is compiled
for lookups as it arrives. See [FTN Nodelists](messages/nodelists.md).

## How inbound TICs are processed

On every `v3mail toss` (and `v3mail poll`), before any packets are tossed,
each network looks at the `.TIC` files in the inbound directories:

1. **Is it from one of our links?** The TIC's `From` address is matched
   against the network's links (zone, net and node). A TIC from an unknown
   address is left for another network; if no network takes it, it is
   reported as unclaimed mail like a packet would be, and moved (with its
   file) to `temp_path/unclaimed` after a day.
2. **Password** — checked against the link's `tic_password`, if set. With
   no `tic_password`, the TIC must have arrived in the secure inbound.
3. **Area** — the file area whose network and file echo match. There must
   be one; ViSiON/3 never creates areas on its own.
4. **The file** — found next to the TIC, by its `File` or long (`Lfile` /
   `Fullname`) name, ignoring case. If it has not arrived yet the TIC waits;
   after a day without it, the TIC is moved aside.
5. **Size and CRC** — the TIC must have a `Crc` line, and the file must
   match it; `Size` is checked when the TIC has one.
6. **Delivery** — the file is moved into the area and recorded, with the
   TIC's long description (or its one-line description), uploaded by the
   address that hatched it, and marked reviewed. The TIC is deleted.
7. **Replaced files** — files in the area that the TIC's `Replaces` lines
   name are removed (see [Replaced files](#replaced-files) below).
8. **Nodelist** — when the area is the network's nodelist echo and the file
   matches its nodelist pattern, the nodelist is compiled for lookups (see
   [FTN Nodelists](messages/nodelists.md)).

The toss reports the files received, dropped as duplicates, moved aside as
bad, and removed as replaced, and says when it compiled a nodelist:

```text
[fsxnet] toss: 3 packets, 41 imported, 0 dupes; file echoes: 2 received, 0 dupes, 0 bad, 1 replaced files removed, nodelist compiled
```

A file the area already holds under the same name is a **duplicate** when
its CRC matches — it is dropped — and a **new version** when it does not: it
replaces the old file and updates its record, keeping the download count.
Weekly nodelists and infopacks that reuse one file name work this way.

### Replaced files

Files whose name changes every week, such as nodelists with a day-number
extension (`FSXNET.Z75`, then `FSXNET.Z82`), come with a TIC that names the
files they supersede on a `Replaces` line, for example `Replaces FSXNET.Z*`.
Once the new file is delivered, the files in the same area that match are
removed, with their records. `*` and `?` are wildcards, and names compare
ignoring case.

Because the sending system decides what is removed, only files that came in
by TIC are removed this way; a file you added to the area yourself stays,
whatever the pattern says. The new file itself is never removed. Each removal
is logged, and the toss reports how many files were removed. A duplicate's
`Replaces` lines apply too, so if a removal fails (it is reported as a toss
error), the hub resending the TIC retries it, as does next week's file naming
the same pattern.

### Undeliverable TICs

A TIC that fails a check — wrong or missing password, no linked area, no
CRC or a CRC or size mismatch, a line that does not parse, or a file that
never arrived — is moved together with its file to
`temp_path/badtic`, and the toss reports it as an error, saying why. Nothing
is deleted. Fix the cause (add the area, set the password), move both files
back into the inbound directory, and toss again.

### A note on .ZIP files in the inbound

Mail bundles can be named `.zip`, and so can most file echo files. A file
named by a TIC in the same directory is never treated as a mail bundle, and
a `.zip` with no packets inside is left in place rather than deleted.

## File area records

Files delivered by TIC carry a `crc32` field in the area's `metadata.json`.
The BBS, `v3mail` and `helper files import` can all add to an area's file
list while the others are running; every change is made to the list on disk
under a lock, so none of them overwrites what another added.
