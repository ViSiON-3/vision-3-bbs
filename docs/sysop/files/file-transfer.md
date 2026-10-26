# File Transfer Protocols

Callers download and upload files the way they always have on a BBS: with a
transfer protocol spoken by their terminal program. ViSiON/3 uses **sexyz**
(Synchronet's external X/Y/ZModem program) as its transfer engine, giving
ZModem with 8k blocks over both SSH and telnet connections. Protocol
definitions live in `configs/protocols.json`, and any external transfer
program can be added alongside sexyz.

| Protocol  | Engine             | Key | Connection Types | PTY Required |
| --------- | ------------------ | --- | ---------------- | ------------ |
| ZModem 8k | sexyz (Synchronet) | `Z` | SSH + Telnet     | No           |

### Why sexyz?

- **Universal** — Works on both SSH and telnet connections without modification
- **No PTY required** — Operates on raw I/O pipes, avoiding PTY line-discipline corruption
- **8k blocks** — Faster throughput than standard 1k ZModem
- **Battle-tested** — Used by Synchronet BBS and other BBS software for decades

## Installing sexyz

sexyz is included as a pre-built binary at `bin/sexyz` in the release bundle.
If it is missing, or you need it for another platform, build it from source.

### Building sexyz from Source

sexyz is **not available through standard package managers**. It must be
built from source or obtained from Synchronet BBS.

- Website: https://www.synchro.net
- Source: https://gitlab.synchro.net

The recommended approach is to build sexyz v3.2 from the Synchronet source repository:

```bash
# Clone the Synchronet source
git clone https://gitlab.synchro.net/main/sbbs.git
cd sbbs

# Build sexyz
cd src/sbbs3
make RELEASE=1 sexyz

# The binary will be at:
# src/sbbs3/*/sexyz (platform-specific subdirectory)
```

### Installing the binary

```bash
cp sexyz /path/to/vision3/bin/sexyz
chmod +x /path/to/vision3/bin/sexyz
```

Verify it works:

```bash
bin/sexyz -help
```

You should see version and usage information from Synchronet External X/Y/ZMODEM.

### Platform Availability

| Platform     | How to Obtain                           |
| ------------ | --------------------------------------- |
| Linux x86_64 | Build from source (recommended)         |
| Linux ARM64  | Build from source                       |
| macOS        | Build from source                       |
| Windows      | Pre-built .exe from Synchronet builds   |

## Configuration

Use the [Configuration Editor](configuration/configuration.md#configuration-editor-tui)
(`./config`, section **8 — Transfer Protocols**) to add, edit and remove
protocols. Each protocol has these fields:

| Field | Meaning |
| --- | --- |
| **Key** | The letter callers press in the protocol menu |
| **Name** / **Description** | Shown in the protocol menu |
| **Send Command** / **Send Args** | Program and arguments for a download (BBS to caller); args are a JSON array |
| **Recv Command** / **Recv Args** | Program and arguments for an upload (caller to BBS) |
| **Batch Send** | Yes if the program can send several files in one run |
| **Use PTY** | Yes if the program needs a pseudo-terminal (sexyz does not) |
| **Default** | Used when the caller does not choose |
| **Conn Type** | `ssh`, `telnet`, or blank for both |

The stored file is `configs/protocols.json`; see the
[Technical Reference](#technical-reference) for its keys and placeholders.

### Adding Custom Protocols

While sexyz is the default and recommended engine, the protocol system
supports any external transfer program. Add a protocol in the editor (or an
entry to `configs/protocols.json` following the field definitions below). Set
**Use PTY** if the program requires a pseudo-terminal, and use **Conn Type**
to restrict it to one connection type.

## Docker Deployment

The stock image does **not** contain sexyz — `.dockerignore` excludes `bin/`, and
the binary is a third-party build that has to match the container architecture
(linux/amd64 for the Alpine base). Supply it one of two ways.

Mount it at runtime:

```yaml
volumes:
  - ./bin:/vision3/bin
```

Or bake it into a derived image:

```dockerfile
FROM vision3:latest
COPY bin/sexyz /vision3/bin/sexyz
```

`.dockerignore` excludes `bin/` but re-includes `bin/sexyz` and `bin/binkd`, so
this resolves with the repository root as the build context.

The entrypoint copies `sexyz.ini` into `bin/` on every start, so only the binary
itself needs supplying. See [Docker Deployment](getting-started/docker.md#adding-sexyz-and-binkd).

## Day-to-Day Operation

When a caller downloads (`DOWNLOADFILE`, `BATCHDOWNLOAD`, or a member from
the archive viewer) or uploads (`UPLOADFILE`), the BBS offers the protocols
whose **Conn Type** matches their connection: Enter takes the default, a
key picks another, **Q** cancels. Each successful transfer is credited to the
caller's upload or download count and byte total (the `|UK` and `|DK`
template codes, in kilobytes, which the login statistics screen shows); a
rejected upload (a duplicate, or one ZipLab refused) earns no credit.

sexyz's own messages are written to the BBS log as `INFO: [sexyz stderr] ...`,
which is the first place to look when a transfer misbehaves.

### Troubleshooting

**"sexyz: command not found" or "no such file"** — sexyz is not at
`bin/sexyz`. Build it from source or download from Synchronet and place it in
the `bin/` directory.

**Transfer hangs or times out**

- Ensure your terminal client supports ZModem (SyncTERM, NetRunner, etc.)
- Check that sexyz has execute permissions: `chmod +x bin/sexyz`
- Review logs for sexyz stderr output (logged as `INFO: [sexyz stderr] ...`)

**Permission denied on sexyz binary**

```bash
chmod +x bin/sexyz
```

**"No transfer protocols configured"** — ensure `configs/protocols.json`
exists and contains valid protocol definitions. The `connection_type` field
must match the user's connection (or be `""` for both).

**A batch download fails with a batch error** — the chosen protocol has
`batch_send` off. Use a batch-capable protocol (ZModem, YModem) for tagged
downloads.

## Technical Reference

### protocols.json

The default protocol configuration in `configs/protocols.json`:

```json
[
  {
    "key": "Z",
    "name": "Zmodem 8k (SEXYZ)",
    "description": "ZModem-8k batch file transfer via Synchronet SEXYZ",
    "send_cmd": "bin/sexyz",
    "send_args": ["-raw", "-8", "sz", "{filePath}"],
    "recv_cmd": "bin/sexyz",
    "recv_args": ["-raw", "-8", "rz", "{targetDir}"],
    "batch_send": true,
    "use_pty": false,
    "default": true,
    "connection_type": ""
  }
]
```

| Field             | Type     | Description                                                |
| ----------------- | -------- | ---------------------------------------------------------- |
| `key`             | string   | Short key shown in protocol selection menu                 |
| `name`            | string   | Display name shown to users                                |
| `description`     | string   | Short description for help/selection menus                 |
| `send_cmd`        | string   | Executable path for sending files (download to user)       |
| `send_args`       | string[] | Arguments for send command                                 |
| `recv_cmd`        | string   | Executable path for receiving files (upload from user)     |
| `recv_args`       | string[] | Arguments for receive command                              |
| `batch_send`      | bool     | Whether the protocol supports multi-file batch sends. When `false`, attempting a multi-file send (e.g. tagged batch download) returns an error — use a batch protocol (ZModem, YModem) for that. |
| `use_pty`         | bool     | Whether the command requires a PTY (pseudo-terminal)       |
| `default`         | bool     | Sets this as the default protocol when user doesn't choose |
| `connection_type` | string   | `""` = any, `"ssh"` = SSH only, `"telnet"` = telnet only  |

### Argument Placeholders

- `{filePath}` — Expanded to one or more file paths (send only). If absent from `send_args`, file paths are appended at the end.
- `{targetDir}` — Expanded to the upload target directory (receive only). A trailing `/` is automatically ensured for sexyz compatibility.
- `{fileListPath}` — Expanded to a temporary file containing one file path per line (for batch sends).

### sexyz Flags

- `-raw` — Disables telnet IAC processing within sexyz. ViSiON/3's telnet layer handles IAC transparently, so sexyz must not double-process it. Harmless on SSH (no IAC exists).
- `-8` — Enables 8k ZModem blocks for faster throughput.
- `sz` / `rz` — Send (download to user) / Receive (upload from user) ZModem mode.

### How It Works

#### SSH Connections

SSH provides a fully binary-transparent channel. sexyz's `-raw` flag has no effect (there are no IAC sequences to process). The data flows through `RunCommandDirect` which pipes sexyz's stdin/stdout directly to the SSH session channel.

#### Telnet Connections

ViSiON/3's telnet layer (`TelnetConn`) handles IAC stripping/escaping transparently. sexyz sees a clean byte stream via `-raw` mode. The data flows through `RunCommandDirect` with the same pipe-based I/O.

#### Execution Flow

1. User selects files and initiates transfer
2. The session's `InputHandler` is reset (`resetSessionIH`) to release the session reader
3. sexyz is launched via `RunCommandDirect` (no PTY)
4. Two goroutines copy data: session→sexyz stdin, sexyz stdout→session
5. When sexyz exits, the stdin goroutine is interrupted via `SetReadInterrupt`
6. Leftover protocol bytes are drained from the session
7. The `InputHandler` is recreated and the BBS resumes normal operation
