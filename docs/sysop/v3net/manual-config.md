# V3Net Manual Configuration Reference

> **Most sysops should use the TUI config editor (`./config`) rather than editing
> JSON directly.** See [V3Net Configuration](configuration.md) for the
> TUI walkthrough, or [Joining FelonyNet](felonynet.md) for a step-by-step
> setup guide.

This page documents every field in `configs/v3net.json` for advanced users who
need to edit the file directly — for example, when deploying via automation,
Docker environment variables, or scripted provisioning.

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use.

---

## Top-Level Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Master switch. Set to `true` to activate V3Net on startup. |
| `keystorePath` | string | `""` | Path to the Ed25519 keypair file. Auto-generated on first start if absent. Example: `"data/v3net.key"` |
| `dedupDbPath` | string | `""` | Path to the SQLite message deduplication database. Example: `"data/v3net_dedup.sqlite"` |
| `registryUrl` | string | `""` | URL of the V3Net network registry JSON file. Used by the BBS to list available networks. Leave blank to disable. |

---

## Hub Configuration (`hub` Object)

Enable this section to host your own V3Net network. Most sysops only need leaf
configuration to join an existing network.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Set to `true` to start the hub server. |
| `host` | string | `""` | Listen address. Blank means all interfaces (`0.0.0.0`). Set to `127.0.0.1` if behind a reverse proxy. |
| `port` | int | `8765` | TCP port for the hub HTTP(S) server. |
| `tlsCert` | string | `""` | Path to TLS certificate file (PEM). If both `tlsCert` and `tlsKey` are set, the hub serves HTTPS. |
| `tlsKey` | string | `""` | Path to TLS private key file (PEM). |
| `dataDir` | string | `""` | Directory for hub data (SQLite database, NAL files). Example: `"data/v3net_hub"` |
| `autoApprove` | bool | `false` | When `true`, new subscriber registrations and area proposals are approved automatically. Recommended for testing only. |
| `networks` | array | `[]` | List of networks hosted by this hub. See below. |
| `initialAreas` | array | `[]` | Area specs for the initial NAL seed. Consumed once on first hub start, then removed automatically. See below. |

### Hub Network Entry (`hub.networks[]`)

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Short lowercase network identifier (e.g. `"felonynet"`). Must be unique per hub. |
| `description` | string | Human-readable description shown to subscribers. |

### Hub Initial Areas (`hub.initialAreas[]`)

Written by the hub setup wizard. On first hub start, if no NAL exists for a
network, the BBS builds, signs, and stores a NAL from these entries, then
removes `initialAreas` from the config file.

| Field | Type | Description |
|-------|------|-------------|
| `tag` | string | Area tag (e.g. `"fel.general"`). Must match `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`. |
| `name` | string | Human-readable area name (e.g. `"FelonyNet General"`). |

---

## Leaf Configuration (`leaves` Array)

Each entry subscribes your BBS to one network on one hub. Add multiple entries
to subscribe to multiple networks.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hubUrl` | string | — | Full URL of the hub (e.g. `"https://felonynet.org"`). Required. |
| `network` | string | — | Network name to subscribe to. Must match a network hosted by the hub. Required. |
| `boards` | array | `[]` | Local message area tags for received messages (e.g. `["fn.general", "fn.tech"]`). |
| `pollInterval` | string | `"5m"` | How often to poll the hub for new messages. Accepts Go duration strings: `"30s"`, `"5m"`, `"1h"`. |
| `origin` | string | BBS name | Origin line appended to outbound messages. Falls back to the BBS name from `config.json` if blank. |

---

## Network Registry Format

The `registryUrl` field points to a public JSON file listing known V3Net networks.

```json
{
  "v3net_registry": "1.0",
  "updated": "2026-03-17",
  "networks": [
    {
      "name": "felonynet",
      "description": "Official ViSiON/3 BBS message network",
      "hub_url": "https://felonynet.org",
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

To add your network to the public registry, submit a PR to the
[v3net-registry](https://github.com/ViSiON-3/v3net-registry) repository.

---

## Example: Minimal Leaf (Joining FelonyNet)

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
      "boards": ["fn.general"],
      "pollInterval": "5m",
      "origin": "My Cool BBS - bbs.example.com"
    }
  ]
}
```

## Example: Hub + Leaf

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

## Example: Multiple Leaf Subscriptions

```json
{
  "leaves": [
    {
      "hubUrl": "https://felonynet.org",
      "network": "felonynet",
      "boards": ["fn.general", "fn.tech"],
      "pollInterval": "5m",
      "origin": "My BBS - bbs.example.com"
    },
    {
      "hubUrl": "https://hub.retronet.io",
      "network": "retronet",
      "boards": ["ret.general"],
      "pollInterval": "5m"
    }
  ]
}
```

---

## Related Documentation

- [V3Net Configuration](configuration.md) — TUI walkthrough for all config screens
- [Joining FelonyNet](felonynet.md) — step-by-step TUI guide
- [V3Net Hub TLS Setup](hub-tls.md) — TLS certificate setup
