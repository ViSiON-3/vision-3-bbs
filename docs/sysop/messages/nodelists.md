# FTN Nodelists

Every FTN network publishes a **nodelist**: the list of its systems, with each
one's address, name, location, sysop, status and connection flags. Most
networks send a new one each week through a file echo — fsxNet's in
`FSX_NODE`, tqwNet's in `TQW_NODE`, Agoranet's in `AGN_NODE`.

ViSiON/3 compiles each network's nodelist into
`data/ftn/nodelist/<network>.json`, and looks systems up in it, for example
when a caller writes netmail. `v3mail toss`
does this whenever a new nodelist arrives by file echo; `helper nodelist
import` does it by hand.

## Compiling nodelists as they arrive

You need the network's nodelist echo set up as a file area — see
[FTN File Echoes](files/file-echoes.md). Then tell the network which echo
carries its nodelist, and which files in it are the nodelist. In the
Configuration Editor, open **Echomail Networks**, choose the network and set:

| Field              | `ftn.json`              | Example     | Description                                                                 |
| ------------------ | ----------------------- | ----------- | --------------------------------------------------------------------------- |
| **Nodelist Echo**  | `nodelist.file_echo`    | `FSX_NODE`  | File echo the nodelist arrives in. Empty turns automatic compiling off.    |
| **Nodelist Files** | `nodelist.file_pattern` | `FSXNET.Z*` | Which files in the echo are the nodelist. `*` and `?` are wildcards.       |

```json
"fsxnet": {
    "own_address": "21:4/158.1",
    "nodelist": { "file_echo": "FSX_NODE", "file_pattern": "FSXNET.Z*" },
    ...
}
```

Set the pattern: nodelist echoes often carry other files too, such as
nodediffs, infopacks, or other networks' nodelists. Check the file names in the
echo's file area once a week's list has arrived. Common ones:

| Network  | Echo       | Pattern       |
| -------- | ---------- | ------------- |
| fsxNet   | `FSX_NODE` | `FSXNET.Z*`   |
| tqwNet   | `TQW_NODE` | `TQWNET.Z*`   |
| Agoranet | `AGN_NODE` | `AGORANET.Z*` |

After each file is delivered, `v3mail toss` checks that it belongs to the
nodelist echo and matches the pattern, then compiles it:

- A plain-text nodelist or a ZIP holding one (`FSXNET.Z75` holds `FSXNET.275`)
  is read. A nodediff is refused: it only holds the lines that changed.
- The list must have an entry for the zone of the network's own address, so
  another network's nodelist sent through the same echo is never compiled in
  its place.
- A list older than the one already compiled (by the date in its first line)
  is skipped, so weeks that arrive out of order cannot roll it back.

The toss reports "nodelist compiled" when it compiled one. A file that matches
the pattern but cannot be compiled is reported as a toss error, saying why. With
no pattern every file in the echo is tried, and those that are not a usable
nodelist are skipped without an error.

The file itself stays in its file area for callers to download. Last week's
file is removed when the new one's TIC says it replaces it (see
[Replaced files](files/file-echoes.md#replaced-files)).

## Where the BBS uses it

### Writing netmail

Once a network has a compiled nodelist, a caller addressing netmail in one of
its netmail areas is shown the system the address belongs to:

```text
To: Paul Hayton@21:1/100
Sending to Risa HUB, Dunedin NZL (Paul Hayton)
```

A point address shows the system it is a point of. The caller is asked before
sending, and can enter another address, when the address:

- is not in the nodelist: `21:4/999 is not in the fsxnet nodelist of
  2026-10-02. Send anyway?`
- is listed as **Down**.

A system listed as **Hold** or **Pvt** gets a note, since its mail waits at or
goes through its host. Nothing is refused: the list can be a week old, and a
new system may not be in it yet. Nothing is shown when the network has no
compiled nodelist, or the address is in a zone the list does not cover.

## `helper nodelist import`

Compiles a network's nodelist by hand: the first time, after changing the
settings above, or for a network whose nodelist does not come by file echo.

```bash
# A nodelist file you already have
./helper nodelist import --network fsxnet --file data/files/fsxnet/fsx_node/FSXNET.Z75

# Download it from the network registry's nodelist URL
./helper nodelist import --network fidonet --registry

# Download it from a URL
./helper nodelist import --network fidonet --url https://example.org/nodelist.zip
```

| Flag              | Description                                                                |
| ----------------- | -------------------------------------------------------------------------- |
| `--network <name>`| Network name in `ftn.json` (required)                                      |
| `--file <path>`   | Nodelist file: plain text, or a ZIP holding it                             |
| `--url <url>`     | Download the nodelist from this URL                                        |
| `--registry`      | Download from the registry's nodelist URL for the network's zone; the URL in `ftn_networks.json` comes first |
| `--force`         | Replace the compiled nodelist even if it is newer                          |
| `--config <dir>`  | Config directory (default: `configs`)                                      |
| `--data <dir>`    | Data directory (default: `data`)                                           |

Give exactly one of `--file`, `--url` and `--registry`. The same checks apply as
for a nodelist that arrives by file echo.

## `helper nodelist lookup`

Looks addresses up in the compiled nodelists, in every network or only in
`--network`. A point address shows the node it is a point of.

```text
$ ./helper nodelist lookup 21:4/999.1
21:4/999.1  [fsxnet]
  Point of:  21:4/999
  System:    Example BBS
  Location:  Springfield USA
  Sysop:     Jane Sysop
  Flags:     CM,INA:bbs.example.com,IBN
```

It exits with status 1 if any address is not listed.
