# V3Net Manual Configuration Reference

> **Most sysops should use the TUI config editor (`./config`) rather than editing
> JSON directly.** See [V3Net Configuration](v3net/configuration.md) for the
> TUI walkthrough, or [Joining FelonyNet](v3net/felonynet.md) for a step-by-step
> setup guide.

This page documents every field in `configs/v3net.json` for advanced users who
need to edit the file directly — for example, when deploying via automation,
Docker environment variables, or scripted provisioning. It is the technical
reference for the whole V3Net section; the other V3Net pages point here rather
than repeating the key tables.

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use.

> **Note:** The running BBS picks up changes to `leaves` on its own; the
> top-level switch, `hub` block and file paths take effect at the next restart.
> Run `vision3 doctor` after a hand edit: it reports a file that will not
> parse, unknown hub URLs, bad poll intervals and board tags that match no
> local area. See [vision3 doctor](configuration/configuration.md#doctor).

---

## Top-Level Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Master switch. Set to `true` to activate V3Net on startup. |
| `keystorePath` | string | `""` | Path to the Ed25519 keypair file. Auto-generated on first start if absent. Example: `"data/v3net.key"` |
| `dedupDbPath` | string | `""` | Path to the SQLite message deduplication database. Example: `"data/v3net_dedup.sqlite"` |
| `registryUrl` | string | `""` | URL of the V3Net network registry JSON file. Used by the BBS to list available networks. Leave blank to disable. |
| `hub` | object | — | Hub server settings. See below. |
| `leaves` | array | `[]` | Subscriptions to networks on remote hubs. See below. |

A missing `configs/v3net.json` is the same as `"enabled": false`; the BBS logs
`v3net.json not found, V3Net disabled` and carries on.

---

## Hub Configuration (`hub` Object)

Enable this section to host your own V3Net network. Most sysops only need leaf
configuration to join an existing network.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Set to `true` to start the hub server. |
| `host` | string | `""` | Listen address. Blank means all interfaces. Set to `127.0.0.1` if behind a reverse proxy. IPv6 literals may be bare (`"::1"`) or bracketed (`"[::1]"`); zone identifiers such as `"fe80::1%eth0"` are accepted. |
| `port` | int | `8765` | TCP port for the hub HTTP server. `0` means `8765`. |
| `dataDir` | string | `""` | Directory for hub data (SQLite database, NAL files). Created on first start if missing. Example: `"data/v3net_hub"` |
| `autoApprove` | bool | `false` | When `true`, new subscriber registrations are approved automatically. |
| `autoApproveAreas` | bool | unset | When `true`, area proposals are added to the NAL on arrival; when `false`, they wait for the coordinator in the Coordinator Panel. Unset follows `autoApprove`, which covered proposals too before this setting existed. Recommended `false` outside testing. |
| `requireSignedSubscribe` | bool | `false` | Require signed subscriptions. Leave false during rolling upgrades; true rejects older unsigned leaves with HTTP 401. Restart to apply; see [independent upgrades](v3net/configuration.md#upgrading-hub-and-leaf-nodes-independently). |
| `requireSignedMessages` | bool | `false` | Reject message posts without an author signature (HTTP 401). Leave false until every leaf signs its messages; posts with a bad signature are rejected either way. Restart to apply; see [requiring signed messages](v3net/configuration.md#requiring-signed-messages). |
| `networks` | array | `[]` | List of networks hosted by this hub. See below. |
| `initialAreas` | array | `[]` | Area specs for the initial NAL seed. Consumed once on first hub start, then removed automatically. See below. |

The hub always serves plain HTTP. There are no certificate keys in this file;
for HTTPS put a reverse proxy in front of it, as described in
[V3Net Hub TLS](v3net/hub-tls.md).

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
| `description` | string | Optional one-line description of the area. |

---

## Leaf Configuration (`leaves` Array)

Each entry subscribes your BBS to one network on one hub. Add multiple entries
to subscribe to multiple networks.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hubUrl` | string | — | Full URL of the hub (e.g. `"https://felonynet.org"`). Required. |
| `network` | string | — | Network name to subscribe to. Must match a network hosted by the hub. Required. |
| `boards` | array | `[]` | Local message area tags for received messages (e.g. `["fn.general", "fn.tech"]`). A local post in any of these areas is sent to the hub, whatever the area's type and even while `enabled` is `false` — it waits for V3Net to come on. |
| `pollInterval` | string | `"5m"` | How often to poll the hub for new messages. Accepts Go duration strings: `"30s"`, `"5m"`, `"1h"`. |
| `origin` | string | BBS name | Origin line appended to outbound messages. Falls back to the BBS name from `config.json` if blank. |
| `autoJoinAreas` | bool | unset (`true`) | **Newscan Default** in the editor. When true or unset, message areas created for this subscription are flagged as newscan defaults for users. Set `false` to leave new areas out of newscans until users add them. |
| `coordinatorPubKey` | string | blank | **Coord Key** in the editor. The network coordinator's Ed25519 public key (base64). NALs signed by any other key are rejected. Blank pins the key of the next NAL the leaf verifies and writes it here. See [Coordinator Key Pinning](v3net/nal.md#coordinator-key-pinning). |
| `hubPubKey` | string | blank | **Hub Key** in the editor. The hub's Ed25519 public key (base64) from the network's `/info`. Pinned and checked the same way. |
| `pendingCoordinatorPubKey`, `pendingHubPubKey` | string | blank | Written by the BBS, shown as **Offered Coord** / **Offered Hub**. A key the hub offered that differs from the pin. It is rejected; copy it into the pin to accept it. |

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
      "hub_node_id": "22819c83e045cd1e",
      "hub_pubkey_b64": "<hub's base64 Ed25519 public key>",
      "coordinator_pubkey_b64": "<coordinator's base64 Ed25519 public key>"
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
| `hub_pubkey_b64` | Optional. The hub's public key. A leaf joined from the registry browser pins it as `hubPubKey`. |
| `coordinator_pubkey_b64` | Optional. The coordinator's public key, which signs the NAL (on a V3Net hub, the same as the hub key). A leaf joined from the registry browser pins it as `coordinatorPubKey` instead of trusting the first NAL it sees. |

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
    "host": "127.0.0.1",
    "port": 8765,
    "dataDir": "data/v3net_hub",
    "autoApprove": false,
    "autoApproveAreas": false,
    "requireSignedSubscribe": false,
    "requireSignedMessages": false,
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

`host` is `127.0.0.1` here because a reverse proxy on the same machine
terminates HTTPS and forwards to the hub; leave it blank for a hub that
answers directly on every interface.

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
      "pollInterval": "5m",
      "autoJoinAreas": false
    }
  ]
}
```

---

## Related Documentation

- [V3Net Configuration](v3net/configuration.md) — TUI walkthrough for all config screens
- [Joining FelonyNet](v3net/felonynet.md) — step-by-step TUI guide
- [V3Net Message Areas](v3net/message-areas.md) — message flow, `helper post`, wire details
- [V3Net Hub TLS Setup](v3net/hub-tls.md) — HTTPS through a reverse proxy
