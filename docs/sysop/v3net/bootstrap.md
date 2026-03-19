# V3Net Bootstrap Tool

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice.

`v3net-bootstrap` is a developer utility that manually creates and publishes
the initial Network Area List (NAL) for a V3Net hub. It is not built by
`build.sh` — run it with `go run` when needed.

> **Superseded for normal use.** The `./config` setup wizard (option `E — V3Net Setup`) and hub startup auto-init handle NAL seeding automatically. Use this tool only when you need to publish a NAL outside of the normal setup flow (e.g. disaster recovery, scripted environments).

## When to Use

The `./config` wizard writes `initialAreas` to `v3net.json` and the BBS seeds the NAL automatically on first start. You only need `v3net-bootstrap` if:

- You are bypassing the wizard and setting up a hub via direct JSON editing.
- You need to re-seed a NAL after a keystore change (disaster recovery).
- You are scripting hub setup in an automated environment without the TUI.

Leaf nodes do not need this tool.

## Usage

```bash
go run ./cmd/v3net-bootstrap \
  -keystore <path>   \
  -hub      <url>    \
  -network  <name>   \
  -areas    <specs>
```

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `-keystore` | Yes | Path to the hub's Ed25519 keystore file (e.g. `data/v3net.key`). The file must already exist — generate it by starting the BBS with V3Net enabled at least once. |
| `-hub` | No | Base URL of the hub. Defaults to `http://localhost:8765`. |
| `-network` | Yes | Network name to bootstrap (e.g. `felonynet`). Must match a network entry in the hub's `v3net.json`. |
| `-areas` | Yes | Comma-separated list of `tag:Name` pairs defining the areas to include in the NAL. |

### Area Spec Format

Each area is specified as `tag:Name`, where `tag` is the short lowercase area
identifier and `Name` is the human-readable display name:

```
fel.general:FelonyNet General,fel.art:FelonyNet Art
```

Areas are created with these defaults:

- Language: `en`
- Access mode: open
- Max message body: 64 000 bytes
- ANSI allowed: yes
- Manager: the hub node (from the keystore)

## Example

```bash
go run ./cmd/v3net-bootstrap \
  -keystore data/v3net.key \
  -hub      http://localhost:8765 \
  -network  felonynet \
  -areas    "fel.general:FelonyNet General,fel.art:FelonyNet Art"
```

Expected output on success:

```
Node ID: a3f9e1b2c4d5e6f7
Signed NAL: network=felonynet, areas=2, updated=Mon, 17 Mar 2026 00:00:00 UTC
Response: 200 {"status":"ok"}
NAL published successfully!
```

## Prerequisites

1. The hub must be running and reachable at the `-hub` URL.
2. The hub must have the network listed in its `hub.networks[]` configuration (see [V3Net Configuration](configuration.md#hub-network-entry-hubnetworks)).
3. The keystore file must exist. Start the BBS with `"enabled": true` and a valid `keystorePath` to auto-generate it.

## How It Works

The tool:

1. Loads the hub's Ed25519 keystore and derives the node ID and public key.
2. Constructs a NAL (`protocol.NAL`) containing the specified areas, with the hub node set as manager of each area.
3. Signs the NAL using `nal.Sign`.
4. POSTs the signed NAL to `POST /v3net/v1/{network}/nal` on the hub, with a signed `X-V3Net-Signature` request header for authentication.

## Build

This tool is intentionally excluded from `build.sh`. To build a standalone binary:

```bash
go build -o v3net-bootstrap ./cmd/v3net-bootstrap
```

The compiled binary is listed in `.gitignore` and should not be committed.

## Related Documentation

- [V3Net Configuration](configuration.md) — hub and leaf setup
- [V3Net Networking](message-areas.md) — how V3Net works from a sysop perspective
