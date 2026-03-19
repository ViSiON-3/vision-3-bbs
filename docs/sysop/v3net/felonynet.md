# FelonyNet

FelonyNet is a general-purpose public BBS message network running on **V3Net**,
Vision/3's native REST+SSE networking protocol. It coexists alongside
FTN-based echomail networks (FidoNet, fsxNet, etc.) — joining FelonyNet does
not affect your existing FTN configuration.

## What You Get

- **Message networking** — public message areas synced across all member BBSes
- **Real-time events** — see who logs on/off across the network instantly
- **Inter-BBS chat** — chat with users on other nodes in real time
- **Zero mailer software** — no FrontDoor, Binkd, or nodelist management
- **Firewall-friendly** — outbound HTTPS only (leaf nodes never need open ports)
- **5-minute setup** — add a config block and restart

## Requirements

- Vision/3 BBS (any version with V3Net support)
- Outbound HTTPS access to the FelonyNet hub

## Joining as a Leaf Node

### 1. Enable V3Net

In your `configs/config.json`, set the `v3net` section:

```json
{
  "v3net": {
    "enabled": true,
    "keystorePath": "data/v3net.key",
    "dedupDbPath": "data/v3net_dedup.sqlite"
  }
}
```

On first start, Vision/3 generates an Ed25519 keypair at `keystorePath` and
derives your permanent node ID from it. **Back up this file** — if lost, you
must re-register with all hubs.

> **Important:** After your first V3Net startup, back up your recovery seed
> phrase immediately. Run `./config > V3Net > Node Identity > [E]` to export
> it to a file. See [V3Net Key Recovery](recovery.md)
> for full details.

### 2. Create a Message Area

Add a local message area for FelonyNet messages. In your message area
configuration, create an area with a tag like `FELONYNET_GENERAL` (the exact
tag is up to you — it maps to the network in the leaf config below).

### 3. Add the Leaf Subscription

Add a `leaves` entry pointing to the FelonyNet hub:

```json
{
  "v3net": {
    "enabled": true,
    "keystorePath": "data/v3net.key",
    "dedupDbPath": "data/v3net_dedup.sqlite",
    "leaves": [
      {
        "hubUrl": "https://hub.felonynet.org",
        "network": "felonynet",
        "board": "FELONYNET_GENERAL",
        "pollInterval": "5m"
      }
    ]
  }
}
```

| Field | Description |
|-------|-------------|
| `hubUrl` | The FelonyNet hub URL |
| `network` | Must be `felonynet` |
| `board` | Your local message area tag |
| `pollInterval` | How often to poll for new messages (minimum 60s enforced by hub) |

### 4. Restart Vision/3

On startup you should see:

```
INFO: V3Net service started (node_id=a3f9e1b2c4d5e6f7, hub=false, leaves=1)
```

Your node automatically subscribes to the hub on first connection. If the hub
uses auto-approve (FelonyNet does), you start receiving messages immediately.

### 5. Verify

From the sysop admin menu, press `3` for **V3Net Status** to confirm your node
ID, subscription count, and network list.

## Area Subscriptions

FelonyNet uses the **Network Area List (NAL)** to define its message areas.
Each area has a tag (e.g., `fel.general`), a name, and an access mode.

### Subscribing to Areas

From the sysop menu, go to **V3Net > Area Subscriptions** to see all available
areas on FelonyNet. Press **Space** to subscribe, **E** to set the local message
base name for an area.

Most FelonyNet areas use **open** access — subscribe and you're in immediately.
Some specialized areas may use **approval** mode, where the area manager reviews
your subscription request first.

### Proposing New Areas

Any FelonyNet sysop can propose a new message area. From the Area Subscriptions
screen, press **P** to open the proposal form. The FelonyNet coordinator reviews
proposals and adds approved areas to the signed NAL.

For full details on area management, see [V3Net NAL documentation](nal.md).

## How It Works

1. **Polling**: Your leaf node polls the hub every `pollInterval` for new
   messages. Messages are deduplicated by UUID — you never get duplicates even
   if you poll the same range twice.

2. **SSE Events**: A persistent Server-Sent Events connection delivers
   real-time notifications: new messages, logon/logoff, and inter-BBS chat.

3. **Posting**: When a user posts to your FelonyNet message area, Vision/3
   automatically forwards the message to the hub, which distributes it to all
   other subscribers.

4. **Authentication**: Every request is signed with your node's Ed25519 key.
   No passwords, no tokens to rotate.

## Hosting a Hub

If you want to run your own V3Net network (not FelonyNet), you can enable hub
mode on your BBS:

```json
{
  "v3net": {
    "enabled": true,
    "keystorePath": "data/v3net.key",
    "dedupDbPath": "data/v3net_dedup.sqlite",
    "hub": {
      "enabled": true,
      "listenAddr": ":8765",
      "dataDir": "data/v3net_hub",
      "autoApprove": true,
      "networks": [
        {
          "name": "mynetwork",
          "description": "My private BBS network"
        }
      ]
    }
  }
}
```

The hub serves:
- REST API for message exchange on the configured port
- SSE event stream for real-time notifications
- Subscriber management (auto-approve or manual approval)

Hub requirements:
- An open inbound port (the `listenAddr` port)
- TLS recommended for production (set `tlsCert` and `tlsKey`), or terminate
  TLS at a reverse proxy

## Registry

The V3Net central registry at
`https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json`
lists public networks. Vision/3 caches the registry for 1 hour and uses it for
network discovery. The registry is optional — you can connect to any hub
directly by URL.

To list your network in the registry, submit a PR to the
[v3net-registry](https://github.com/ViSiON-3/v3net-registry) repository with
your network entry:

```json
{
  "name": "felonynet",
  "description": "General discussion. No warrants required.",
  "hub_url": "https://hub.felonynet.org",
  "hub_node_id": "a3f9e1b2c4d5e6f7",
  "tags": ["general", "tech", "bbs"]
}
```

## Troubleshooting

**"V3Net networking disabled"** — `v3net.enabled` is `false` in config.json.

**"message area not found, skipping"** — The `board` tag in your leaf config
doesn't match any configured message area. Check your message area tags.

**No messages arriving** — Check that the hub URL is reachable. Look for
`leaf: poll failed` warnings in the log. Verify your node is approved (hub may
require manual approval).

**Lost keypair** — If you have your 24-word recovery seed phrase, use
`./config > V3Net > Node Identity > [R]` to restore your identity. If you have
lost both the key file and the seed phrase, a new identity is generated on next
start and you must re-subscribe to all hubs.
