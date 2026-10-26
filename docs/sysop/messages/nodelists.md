# FTN Nodelists

Every FTN (FidoNet Technology Network) network publishes a **nodelist**: the
list of its systems, with each one's address, name, location, sysop, status
and connection flags. Most networks send a new one each week through a file
echo — fsxNet's in `FSX_NODE`, tqwNet's in `TQW_NODE`, Agoranet's in
`AGN_NODE`. With a compiled nodelist your board can tell a caller whose system
a netmail is going to, warn when an address is not listed, and show system
names in message headers, instead of leaving everyone staring at bare
`zone:net/node` numbers.

ViSiON/3 compiles each network's nodelist into
`data/ftn/nodelist/<network>.json`, and looks systems up in it, for example
when a caller writes netmail. `v3mail toss`
does this whenever a new nodelist arrives by file echo; `helper nodelist
import` does it by hand. The FTN setup wizard also saves the nodelist it
downloads when you use **Node Lookup**, so a new network has one from the start.

## Compiling nodelists as they arrive

You need the network's nodelist echo set up as a file area — see
[FTN File Echoes](files/file-echoes.md). Then tell the network which echo
carries its nodelist, and which files in it are the nodelist. In the
Configuration Editor (`./config` → **4 - Echomail Networking** →
**Echomail Networks**), choose the network and set:

| Field              | `ftn.json`              | Example     | Description                                                                 |
| ------------------ | ----------------------- | ----------- | --------------------------------------------------------------------------- |
| **Nodelist Echo**  | `nodelist.file_echo`    | `FSX_NODE`  | File echo the nodelist arrives in. Empty turns automatic compiling off.    |
| **Nodelist Files** | `nodelist.file_pattern` | `FSXNET.Z*` | Which files in the echo are the nodelist. `*` and `?` are wildcards.       |

Set the pattern: nodelist echoes often carry other files too, such as
nodediffs, infopacks, or other networks' nodelists. Check the file names in the
echo's file area once a week's list has arrived. Common ones:

| Network  | Echo       | Pattern       |
| -------- | ---------- | ------------- |
| FidoNet  | `NODELIST` | `NODELIST.Z*` |
| fsxNet   | `FSX_NODE` | `FSXNET.Z*`   |
| tqwNet   | `TQW_NODE` | `TQWNET.Z*`   |
| Agoranet | `AGN_NODE` | `AGORANET.Z*` |
| AmigaNet | `AMYLIST`  | `AMYLIST.L*`  |

After each file is delivered, `v3mail toss` checks that it belongs to the
nodelist echo and matches the pattern, then compiles it:

- A plain-text nodelist, or a ZIP or LHA archive holding one, is read
  (`FSXNET.Z75` is a ZIP holding `FSXNET.275`; AmigaNet's `AMYLIST.L55` is an
  LHA archive holding `AmyList.255`). A nodediff is refused: it only holds the
  lines that changed.
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

## Compiling a nodelist by hand

Run `helper nodelist import` the first time, after changing the settings
above, or for a network whose nodelist does not come by file echo:

```bash
# A nodelist file you already have
./helper nodelist import --network fsxnet --file data/files/fsxnet/fsx_node/FSXNET.Z75

# Download it from the network registry's nodelist URL
./helper nodelist import --network fidonet --registry

# Download it from a URL
./helper nodelist import --network fidonet --url https://example.org/nodelist.zip
```

Give exactly one of `--file`, `--url` and `--registry`. The same checks apply as
for a nodelist that arrives by file echo. The flags are listed in the
[Technical Reference](#helper-nodelist).

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

`helper post --to "Name@zone:net/node"` does the same lookup when it posts
netmail from the command line, and prints whose system the address belongs
to (see [Posting from the Command Line](messages/message-areas.md#posting-from-the-command-line)).

### Message headers

Two message header placeholders give the system names for a message's
addresses, looked up in its area network's nodelist:

| Code  | Replaced with                         |
| ----- | ------------------------------------- |
| `@Y@` | The origin system's name (`@O@`)      |
| `@R@` | The destination system's name (`@A@`) |

A point gets its node's name. Both are blank when the address is not listed,
the area is not an FTN area, or the network has no compiled nodelist, so wrap
them in an optional group to hide the label too:

```text
From: @F@|{ at @Y@|}
```

The default header templates do not use them yet; see
[Message Header Placeholders](messages/placeholders.md).

### Looking an address up

`helper nodelist lookup` looks addresses up in the compiled nodelists, in
every network or only in `--network`. A point address shows the node it is a
point of.

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

## Troubleshooting

**Nodelist not compiled after a toss.** Check that the network's
**Nodelist Echo** matches the echo's tag and that **Nodelist Files** matches
the file's name in the area. A file that matches the pattern but cannot be
compiled is a toss error saying why (not a nodelist, a nodediff, or another
zone's list). An older list than the one already compiled is skipped and
logged. Compile one by hand with `helper nodelist import`.

**`helper nodelist import` refuses an AmigaNet list.** Only LHA methods
`-lh0-` and `-lh4-` to `-lh7-` are supported; the old `-lh1-` and LArc
methods report an error saying so. Unpack the archive yourself and import
the plain-text list with `--file`.

**Callers see no "Sending to" line.** The network has no compiled nodelist
(look for `data/ftn/nodelist/<network>.json`), or the address is in a zone
the list does not cover.

**`@Y@` / `@R@` are blank in a header.** Same causes, or the area is not an
FTN area. Wrap them in `|{ … |}` so the label disappears with them.

## Technical Reference

### `ftn.json`

Per network (`networks.<key>.nodelist`):

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `nodelist.file_echo` | string | `""` | File echo the nodelist arrives in (**Nodelist Echo**; upper-cased, no spaces). Empty turns automatic compiling off. |
| `nodelist.file_pattern` | string | `""` | Glob (`*`, `?`) for the nodelist files in that echo (**Nodelist Files**). Empty tries every file. |

```json
"fsxnet": {
    "own_address": "21:4/158.1",
    "nodelist": { "file_echo": "FSX_NODE", "file_pattern": "FSXNET.Z*" },
    ...
}
```

### Compiled nodelist files

Each network's compiled list is `data/ftn/nodelist/<network>.json`, written
atomically. It records the list's date and day number from the nodelist's
header line, and one entry per system. The BBS loads a list when it is first
needed and reloads it when the file changes, so a toss or an import takes
effect without a restart. A list older than the saved one (by header date)
is not written unless forced.

Accepted input: a plain-text nodelist (FTS-0005), a ZIP holding one (the
`.Zxx` convention), or an LHA archive (header levels 0, 1 and 2; methods
`-lh0-`, `-lh4-` to `-lh7-`). Nodediffs are refused.

### `helper nodelist`

`helper nodelist import`:

| Flag              | Description                                                                |
| ----------------- | -------------------------------------------------------------------------- |
| `--network <name>`| Network name in `ftn.json` (required)                                      |
| `--file <path>`   | Nodelist file: plain text, or a ZIP or LHA archive holding it              |
| `--url <url>`     | Download the nodelist from this URL                                        |
| `--registry`      | Download from the registry's nodelist URL for the network's zone; the URL in `ftn_networks.json` comes first |
| `--force`         | Replace the compiled nodelist even if it is newer                          |
| `--config <dir>`  | Config directory (default: `configs`)                                      |
| `--data <dir>`    | Data directory (default: `data`)                                           |

`helper nodelist lookup <address> [...]`:

| Flag              | Description                                                                |
| ----------------- | -------------------------------------------------------------------------- |
| `--network <name>`| Look only in this network (default: every network in `ftn.json`)          |
| `--config <dir>`  | Config directory (default: `configs`)                                      |
| `--data <dir>`    | Data directory (default: `data`)                                           |

Exit status: `0` on success; `1` when an import fails, when the arguments are
wrong, or when any looked-up address is not listed.

### Related settings elsewhere

- The FTN Setup Wizard's **Node Lookup** downloads the network's nodelist
  from the registry to find your hub, and saving the wizard compiles that
  list unless a newer one is already compiled or it has no entry for your
  zone ([FTN Echomail](messages/ftn-echomail.md#quick-setup-ftn-setup-wizard-recommended)).
- `v3mail toss` compiles nodelists as part of TIC delivery
  ([V3Mail](messages/v3mail.md)).
