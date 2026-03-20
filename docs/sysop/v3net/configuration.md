# V3Net Configuration

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net is ViSiON/3's native inter-BBS message networking protocol. It uses REST+SSE over HTTPS with Ed25519 cryptographic authentication. Configuration is stored in `configs/v3net.json`.

If the file does not exist, V3Net is disabled by default.

## Setup Wizard

The easiest way to configure V3Net is the guided setup wizard in the TUI config editor. From the top menu:

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Subscriptions  →  [I]nsert
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Networks       →  [I]nsert
```

When you press **[I]** (Insert) on either record list, a guided wizard launches instead of raw field editing:

- **Subscriptions → [I]nsert** opens **"Leaf Setup — Join a Network"** — walks you through hub URL, network name, board tag, poll interval, and origin line. Writes a `leaves[]` entry and saves.
- **Networks → [I]nsert** opens **"Hub Setup — Host a Network"** — walks you through network name, description, listen port, auto-approve setting, and initial message areas. Saves the full hub config; the BBS auto-initialises the hub data directory and seeds the NAL on first start.

After the wizard saves, restart the BBS to activate the configuration.

---

## Quick Start

To join an existing network like FelonyNet, see the [FelonyNet guide](felonynet.md). This page documents every configuration field in detail.

## Configuration File

### Top-Level Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Master switch. Set to `true` to activate V3Net on startup. |
| `keystorePath` | string | `""` | Path to the Ed25519 keypair file. If the file does not exist, a new keypair is generated automatically on first start. Example: `"data/v3net.key"` |
| `dedupDbPath` | string | `""` | Path to the SQLite database used for message deduplication. Prevents the same message from being imported twice. Example: `"data/v3net_dedup.sqlite"` |
| `registryUrl` | string | `""` | URL of a V3Net network registry (JSON). Used by the BBS menu to list available networks. Optional — leave blank if you know the hub URL already. |

### Network Registry

The `registryUrl` field points to a public JSON file that lists known V3Net
networks and their hub URLs — a directory of available networks your BBS can
join. The default registry is hosted at:

```
https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json
```

The registry is fetched (and cached for 1 hour) when a sysop opens the
**N — Network Registry** option from the V3Net menu. It displays each
network's name, description, and hub URL, and marks networks you are
already subscribed to.

**Registry format:**

```json
{
  "v3net_registry": "1.0",
  "updated": "2026-03-17",
  "networks": [
    {
      "name": "felonynet",
      "description": "Official ViSiON/3 BBS message network",
      "hub_url": "https://hub.felonynet.org",
      "hub_node_id": "22819c83e045cd1e"
    }
  ]
}
```

| Field | Description |
|-------|-------------|
| `name` | Short lowercase network identifier (must match the hub's network name) |
| `description` | Human-readable summary shown in the registry browser |
| `hub_url` | Base URL of the hub's V3Net endpoint |
| `hub_node_id` | 16-character hex node ID of the hub |

**Adding your network to the registry:** Submit a pull request to the
[v3net-registry](https://github.com/ViSiON-3/v3net-registry) repository
adding your network entry to `registry.json`. Your hub must be publicly
reachable and running before submission.

You can also host your own private registry by setting `registryUrl` to any
URL that serves the same JSON format. Leave the field blank to disable the
registry browser.

---

### Example (Minimal Leaf)

```json
{
  "enabled": true,
  "keystorePath": "data/v3net.key",
  "dedupDbPath": "data/v3net_dedup.sqlite",
  "registryUrl": "https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json",
  "hub": {
    "enabled": false
  },
  "leaves": [
    {
      "hubUrl": "https://felonynet.org",
      "network": "felonynet",
      "board": "fel.general",
      "pollInterval": "5m",
      "origin": "My Cool BBS - bbs.example.com"
    }
  ]
}
```

---

## Hub Configuration (`hub` Object)

Enable the hub section to host your own V3Net network. Most sysops only need leaf configuration to join an existing network.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Set to `true` to start the hub server. |
| `host` | string | `""` | Listen address. Blank means all interfaces (`0.0.0.0`). Set to `127.0.0.1` if behind a reverse proxy. |
| `port` | int | `8765` | TCP port for the hub HTTP(S) server. |
| `tlsCert` | string | `""` | Path to TLS certificate file (PEM). If both `tlsCert` and `tlsKey` are set, the hub serves HTTPS. |
| `tlsKey` | string | `""` | Path to TLS private key file (PEM). |
| `dataDir` | string | `""` | Directory for hub data (SQLite database, NAL files). Example: `"data/v3net_hub"` |
| `autoApprove` | bool | `false` | When `true`, new subscriber registrations and area proposals are approved automatically. Recommended for testing; disable for production networks. |
| `networks` | array | `[]` | List of networks hosted by this hub. See below. |
| `initialAreas` | array | `[]` | Area specs written by the setup wizard. Consumed once on first hub start to seed the initial NAL, then removed automatically. See below. |

### Hub Network Entry (`hub.networks[]`)

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Short lowercase network identifier (e.g. `"felonynet"`). Must be unique per hub. |
| `description` | string | Human-readable description shown to subscribers. |

### Hub Initial Areas (`hub.initialAreas[]`)

Written by the setup wizard. On first hub start, if no NAL exists for a network, the BBS builds, signs, and stores a NAL from these entries, then removes `initialAreas` from the config file. You do not need to manage this field manually — the wizard handles it.

| Field | Type | Description |
|-------|------|-------------|
| `tag` | string | Area tag (e.g. `"fel.general"`). Must match `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`. |
| `name` | string | Human-readable area name (e.g. `"FelonyNet General"`). |

### Hub Startup Auto-Init

When the hub starts for the first time, ViSiON/3 automatically:

1. Creates the `dataDir` directory if it does not exist.
2. Registers the hub's own node as an active subscriber for each network (idempotent — safe on every restart).
3. Seeds the initial NAL from `initialAreas` if no NAL exists yet, then clears `initialAreas` from the config file.

This means you do not need to run any bootstrap commands after completing the setup wizard — just start the BBS.

### Example (Hub + Leaf)

```json
{
  "enabled": true,
  "keystorePath": "data/v3net.key",
  "dedupDbPath": "data/v3net_dedup.sqlite",
  "hub": {
    "enabled": true,
    "port": 8765,
    "tlsCert": "/etc/letsencrypt/live/hub.example.com/fullchain.pem",
    "tlsKey": "/etc/letsencrypt/live/hub.example.com/privkey.pem",
    "dataDir": "data/v3net_hub",
    "autoApprove": false,
    "networks": [
      {
        "name": "mynet",
        "description": "My BBS Network"
      }
    ]
  },
  "leaves": []
}
```

---

## Leaf Configuration (`leaves` Array)

Each entry in the `leaves` array subscribes your BBS to one network on one hub. You can subscribe to multiple networks by adding multiple leaf entries.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hubUrl` | string | — | Full URL of the hub (e.g. `"https://felonynet.org"`). Required. |
| `network` | string | — | Network name to subscribe to. Must match a network hosted by the hub. Required. |
| `board` | string | `""` | Local message area tag prefix for received messages. When the hub has multiple areas, each area creates a local message area with this prefix. |
| `pollInterval` | string | `"5m"` | How often to poll the hub for new messages. Accepts Go duration strings: `"30s"`, `"5m"`, `"1h"`. Shorter intervals mean faster delivery but more hub traffic. |
| `origin` | string | BBS name | Origin line text appended to locally-posted messages. Identifies your BBS to readers on other nodes. If blank, falls back to the BBS name from `config.json`. |

### Multiple Network Example

```json
{
  "leaves": [
    {
      "hubUrl": "https://felonynet.org",
      "network": "felonynet",
      "board": "fel.general",
      "pollInterval": "5m",
      "origin": "My BBS - bbs.example.com"
    },
    {
      "hubUrl": "https://hub.retronet.io",
      "network": "retronet",
      "board": "retro.general",
      "pollInterval": "10m"
    }
  ]
}
```

---

## Configuration Editor

V3Net settings are spread across two areas of the TUI configuration editor (`./config`):

### V3Net Category Menu

From the top menu, select **4 — ViSiON/3 Networking (V3Net)**. This opens a sub-menu with three options:

| Item | Description |
|------|-------------|
| **Node Identity** | View your node ID and public key. Show, export, or recover your seed phrase. |
| **Subscriptions** | Add, edit, and remove leaf entries (Hub URL, Network, Board, Poll Interval, Origin). Press **[I]** to launch the leaf setup wizard. |
| **Networks** | Add, edit, and remove hosted network definitions (Name, Description). Press **[I]** to launch the hub setup wizard. |

### System-Level V3Net Settings

Global V3Net settings and hub server parameters live in the system configuration screens:

```
./config  →  1 — System Configuration  →  Server Setup
```

This screen contains:

| Field | Description |
|-------|-------------|
| V3Net | Master enable/disable for V3Net |
| Keystore Path | Path to Ed25519 keypair file |
| Dedup DB Path | Path to deduplication SQLite database |
| Registry URL | Central V3Net registry URL (optional) |
| V3Net Hub | Enable/disable the hub server |
| Hub Host | Listen address (blank = all interfaces) |
| Hub Port | Listen port (default: 8765) |
| Hub TLS Cert | Path to TLS certificate (blank for plain HTTP) |
| Hub TLS Key | Path to TLS private key |
| Hub Data Dir | Hub data storage directory |
| Auto Approve | Automatically approve new leaf subscriptions |

For details on the Hub TLS fields, see [Hub TLS Setup](hub-tls.md).

---

## Node Identity

Your V3Net node identity is an Ed25519 keypair. The node ID (a 16-character hex
string) is derived from the public key and serves as your permanent identity on
all networks.

The key file is stored at the path configured in `keystorePath` (default:
`data/v3net.key`). If it doesn't exist, it is generated automatically on first
V3Net startup.

### Backing Up Your Identity

A 24-word recovery seed phrase can restore your keypair if the key file is lost.
Access it through the config editor:

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Node Identity
```

From this screen you can:
- **[S] Show** the seed phrase on screen
- **[E] Export** the seed phrase to a file
- **[R] Recover** a key from a previously saved seed phrase

For full details, see [V3Net Key Recovery](recovery.md).

---

## File Locations

| File | Purpose |
|------|---------|
| `configs/v3net.json` | Main configuration file |
| `data/v3net.key` | Ed25519 keypair (auto-generated) |
| `data/v3net_dedup.sqlite` | Message deduplication database |
| `data/v3net_hub/` | Hub data directory (SQLite DB, NAL files) |

The keypair file is critical — it is your node's identity on the network. **Back up your recovery seed phrase** via `./config → 4 → Node Identity → [E]`. See [V3Net Key Recovery](recovery.md).

---

## Related Documentation

- [Joining FelonyNet](felonynet.md) — step-by-step guide for the FelonyNet network
- [Network Area List (NAL)](nal.md) — area subscriptions, access modes, and proposals
- [Message Areas](../messages/message-areas.md) — configuring local JAM message bases
- [V3Net Message Areas](message-areas.md) — how V3Net areas differ from local and FTN areas
