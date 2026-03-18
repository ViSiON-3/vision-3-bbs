# V3Net Configuration

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net is ViSiON/3's native inter-BBS message networking protocol. It uses REST+SSE over HTTPS with Ed25519 cryptographic authentication. Configuration is stored in `configs/v3net.json`.

If the file does not exist, V3Net is disabled by default.

## Quick Start

To join an existing network like FelonyNet, see the [FelonyNet guide](../../felonynet.md). This page documents every configuration field in detail.

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
      "hub_url": "https://hub.felonynet.example.com/v3net",
      "hub_node_id": "a3f9e1b2c4d5e6f7"
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
      "hubUrl": "https://hub.felonynet.org",
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

### Hub Network Entry (`hub.networks[]`)

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Short lowercase network identifier (e.g. `"felonynet"`). Must be unique per hub. |
| `description` | string | Human-readable description shown to subscribers. |

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
| `hubUrl` | string | — | Full URL of the hub (e.g. `"https://hub.felonynet.org"`). Required. |
| `network` | string | — | Network name to subscribe to. Must match a network hosted by the hub. Required. |
| `board` | string | `""` | Local message area tag prefix for received messages. When the hub has multiple areas, each area creates a local message area with this prefix. |
| `pollInterval` | string | `"5m"` | How often to poll the hub for new messages. Accepts Go duration strings: `"30s"`, `"5m"`, `"1h"`. Shorter intervals mean faster delivery but more hub traffic. |
| `origin` | string | BBS name | Origin line text appended to locally-posted messages. Identifies your BBS to readers on other nodes. If blank, falls back to the BBS name from `config.json`. |

### Multiple Network Example

```json
{
  "leaves": [
    {
      "hubUrl": "https://hub.felonynet.org",
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

V3Net settings can also be edited through the TUI configuration editor (`./config`). The editor provides dedicated screens for:

- **V3Net Leaf Subscriptions** — add, edit, and remove leaf entries with field-by-field editing (Hub URL, Network, Board, Poll Interval, Origin)
- **V3Net Hub Networks** — add, edit, and remove hosted network definitions (Name, Description)

---

## File Locations

| File | Purpose |
|------|---------|
| `configs/v3net.json` | Main configuration file |
| `data/v3net.key` | Ed25519 keypair (auto-generated) |
| `data/v3net_dedup.sqlite` | Message deduplication database |
| `data/v3net_hub/` | Hub data directory (SQLite DB, NAL files) |

The keypair file is critical — it is your node's identity on the network. **Back it up.** If lost, you will need to re-register with every hub.

---

## Related Documentation

- [Joining FelonyNet](../../felonynet.md) — step-by-step guide for the FelonyNet network
- [Network Area List (NAL)](../../v3net-nal.md) — area subscriptions, access modes, and proposals
- [Message Areas](../messages/message-areas.md) — configuring local JAM message bases
- [V3Net Message Areas](../messages/v3net.md) — how V3Net areas differ from local and FTN areas
