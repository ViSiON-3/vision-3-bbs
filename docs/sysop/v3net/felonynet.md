# Joining FelonyNet

FelonyNet is a general-purpose public BBS message network running on **V3Net**,
ViSiON/3's native REST+SSE networking protocol. It coexists alongside
FTN-based echomail networks (FidoNet, fsxNet, etc.) — joining FelonyNet does
not affect your existing FTN configuration. Joining is the quickest way to
get your board talking to other ViSiON/3 systems: a few screens in the config
editor, a restart, and your callers are reading and posting across the
network and chatting with callers on other boards.

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

> **Security note:** All messages and NALs are signed with Ed25519 keys, preventing impersonation and forgery. However, traffic between leaf nodes and the hub is **unencrypted on the wire** (plain HTTP) — signatures guarantee authenticity, not confidentiality. Messages are also stored unencrypted on disk at both leaf and hub nodes. FelonyNet is a public network — treat everything you post as public and permanent. See [Security Considerations](v3net/configuration.md#security-considerations) for full details.

## What You Get

- **Message networking** — public message areas synced across all member BBSes
- **Real-time events** — see who logs on/off across the network instantly
- **Inter-BBS chat** — chat with users on other nodes in real time
- **Zero mailer software** — no FrontDoor, Binkd, or nodelist management
- **Firewall-friendly** — outbound HTTP only (leaf nodes never need open ports)
- **5-minute setup** — a few screens in the config editor and a restart

## Requirements

- ViSiON/3 BBS (any version with V3Net support)
- Outbound HTTP access to the FelonyNet hub

---

## Step 1: Enable V3Net

Open the config editor and navigate to the system settings:

```
./config  →  1 — System Setup  →  2. Server Setup
```

Set **V3Net** to `Y` and press **S** to save. The keystore path, dedup DB path,
and registry URL are pre-configured with sensible defaults — you don't need to
change them.

> On first start with V3Net enabled, ViSiON/3 generates an Ed25519 keypair at
> the keystore path and derives your permanent node ID from it.

---

## Step 2: Add the FelonyNet Subscription

From the main config menu, open the V3Net networking section:

```
./config  →  6 — ViSiON/3 Networking (V3Net)
```

```
┌──────────────────────────────────────┐
│    ViSiON/3 Networking (V3Net)       │
│                                      │
│  1. Node Identity                    │
│  2. Subscriptions                    │
│  3. Networks                         │
│  Q. Return                           │
│                                      │
└──────────────────────────────────────┘
Enter - Select  |  ESC/Q - Return
```

Select **2. Subscriptions**, then press **B** to open the **Network Registry**:

```
┌──────────────────────────────────────────────────────────────────────┐
│                        Network Registry                              │
│  Network        Description                    Hub URL               │
│──────────────────────────────────────────────────────────────────────│
│  felonynet      Official ViSiON/3 BBS network  http://felonynet.org:8765 │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
Enter - Select  |  ESC - Back
```

Select **felonynet** and press **Enter**. The **Leaf Setup Wizard** opens with
the Hub URL and Network pre-filled:

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Leaf Setup — Join a Network                      │
│                                                                      │
│  Registry        : (press Enter to browse available networks)        │
│  Hub URL         : http://felonynet.org:8765                         │
│  Network         : felonynet                                         │
│  Areas           : (none — press Enter to browse)                    │
│  Poll Interval   : 5m                                                │
│  Origin          : My BBS - bbs.example.com                          │
│                                                                      │
│                         S - Save  |  ESC - Cancel                    │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  S - Save  |  ESC - Back
```

Fill in any remaining fields:

| Field | Value |
|-------|-------|
| Areas | Press **Enter** to open the area browser and subscribe |
| Poll Interval | `5m` is the default; messages arrive by push in between polls |
| Origin | Your BBS name and address, e.g. `My BBS - bbs.example.com` |

> **Tip:** You can also press **I** on the Subscriptions screen to open a blank
> wizard and type the Hub URL and Network manually.

Press **Enter** on **Areas** to open the area browser and choose which FelonyNet
areas to subscribe to:

```
┌──────────────────────────────────────────────────────────────────────┐
│                      Area Browser — felonynet                        │
│      Tag             Name             Status   Local Board           │
│──────────────────────────────────────────────────────────────────────│
│  [ ] fel.general     General                                         │
│  [ ] fel.phreaking   Phreaking                                       │
│  [ ] fel.art         ANSI/ASCII Art                                  │
│  [ ] fel.tech        Tech Talk                                       │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
Space - Subscribe/Unsubscribe  |  ESC - Done
```

Press **Space** to subscribe to each area you want. Press **ESC** when done,
then press **S** to save the subscription.

---

## Step 3: Back Up Your Identity

Your node ID is derived from an Ed25519 keypair. If you lose this key and have
no backup, you must re-register with all hubs from scratch.

**Do this before your first restart:**

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  1. Node Identity
```

```
┌────────────────────────────────────────────────────────────┐
│                    V3Net Node Identity                     │
│                                                            │
│  Node ID:    a3f9e1b2c4d5e6f7                              │
│  Public Key: MCowBQYDK2VdAyEA...                           │
│  Key File:   data/v3net.key                                │
│                                                            │
│  [S] Show recovery seed phrase                             │
│  [E] Export recovery seed phrase to file                   │
│  [R] Recover identity from seed phrase                     │
│                                                            │
└────────────────────────────────────────────────────────────┘
S - Show  |  E - Export  |  R - Recover  |  Q - Return
```

Press **E** to export the seed phrase to a file. Move that file off-server
immediately — password manager, encrypted USB, or printed copy in a safe.

See [V3Net Key Recovery](v3net/recovery.md) for full details.

---

## Step 4: Restart the BBS

Exit the config editor and restart:

```bash
# If running as a service
systemctl restart vision3

# If running manually
./vision3
```

On startup you should see a log line like:

```
INFO: V3Net service started (node_id=a3f9e1b2c4d5e6f7, hub=false, leaves=1)
```

Your node automatically subscribes to the FelonyNet hub on first connection.
If the hub auto-approves new nodes, the log shows `leaf: subscribed to hub`
and you start receiving messages immediately. Otherwise the leaf keeps
retrying and logs `leaf: subscribe failed, retrying` with
`subscription status "pending"` until the hub sysop approves your node in
their Node Management screen; nothing is needed from you but patience.

---

## After Joining

- **New areas come to you.** When FelonyNet adds an area, you are asked
  about it at your next login — `New Felonynet area: Music. Add?` — and
  **Yes** subscribes and creates the local area on the spot. See
  [New Areas on Your Networks](v3net/message-areas.md#new-areas-on-your-networks).
- **Chat is ready.** The `CHAT` command on the main menu offers `felonynet`
  in its network picker as soon as the subscription is saved. See
  [V3Net Chat](v3net/chat.md).
- **Check the setup any time** with `vision3 doctor`, which validates
  `v3net.json`, the key file and its permissions without starting the board.
  See [vision3 doctor](configuration/configuration.md#doctor).

---

## Updating an Existing FelonyNet Node

For a source checkout, pull the latest code, run `./build.sh`, and restart the
BBS. This rebuilds both the BBS and config editor. Keep your existing
configuration, node key, and data: updated clients sign subscriptions
automatically with that key, without re-registration or re-approval.

Hub and leaf updates can happen independently while the hub keeps
**Signed Subscribe** at its default **N**. A hub rebuild does not automatically
enforce signatures. Hub operators can enable enforcement later; older unsigned
clients must update before connecting to a hub that requires signatures.
See [Independent hub and leaf upgrades](v3net/configuration.md#upgrading-hub-and-leaf-nodes-independently)
for the hub procedure and compatibility setting.

---

## Troubleshooting

**"V3Net is disabled in server configuration"** on the V3Net screens — V3Net
is not enabled. Go back to Step 1 and set **V3Net** to `Y` in System Setup →
Server Setup, then restart.

**"message area not found, skipping"** — A subscribed area doesn't match any
local message base. Open the area browser (edit the subscription → Browse
Areas) and verify the local board mapping. `vision3 doctor` reports the same
mismatch as a warning.

**No messages arriving** — Check that `felonynet.org` is reachable. Look for
`leaf: poll failed` warnings in the log. A `leaf: subscribe failed, retrying`
line with `subscription status "pending"` means the hub has not approved your
node yet. The V3Net Status screen (`V3NETSTATUS`) shows your node ID and the
networks you subscribe to.

**A post of yours never shows "V3NET SENT"** — The hub could not be reached
when it was posted. The BBS retries every minute and logs the first failure
as `v3net outbound: sending to hub; will retry`; nothing is lost. See
[How Messages Flow](v3net/message-areas.md#how-messages-flow).

**"signed subscribe required" / subscription HTTP 401** — If the hub requires
signed subscriptions, update both the BBS and config editor and restart with
your existing key. Do not delete your registration or generate a new key.
If an updated client still gets 401, check the hub's error details: invalid
signatures or stale request dates may indicate a signing or clock problem.
The hub operator can restore older-client compatibility by setting **Signed
Subscribe** to **N** and restarting.

**Lost keypair** — If you have your 24-word recovery seed phrase, restore it
via `./config → 6 — ViSiON/3 Networking (V3Net) → 1. Node Identity → [R] Recover`.
If you have lost both the key file and the seed phrase, a new identity is
generated on next start and you must re-subscribe to all hubs.

---

## Hosting Your Own Network

To run your own V3Net hub (not FelonyNet), see the
[V3Net Configuration](v3net/configuration.md#hosting-a-hub) guide.

---

## Related Documentation

- [V3Net Configuration](v3net/configuration.md) — all V3Net config screens in the TUI
- [Network Area List (NAL)](v3net/nal.md) — area subscriptions, access modes, proposals
- [V3Net Key Recovery](v3net/recovery.md) — backing up and restoring your node identity
- [Manual Configuration Reference](v3net/manual-config.md) — direct JSON editing

---

## Technical Reference

What the wizard writes to `configs/v3net.json` for FelonyNet:

```json
{
  "enabled": true,
  "keystorePath": "data/v3net.key",
  "dedupDbPath": "data/v3net_dedup.sqlite",
  "registryUrl": "https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json",
  "leaves": [
    {
      "hubUrl": "http://felonynet.org:8765",
      "network": "felonynet",
      "boards": ["fel.general", "fel.tech"],
      "pollInterval": "5m",
      "origin": "My BBS - bbs.example.com"
    }
  ]
}
```

The shipped `templates/configs/v3net.json` already carries the FelonyNet
subscription with an empty `boards` list and `enabled` off, so a fresh board
only needs the switch, the areas and a restart. Every key is tabulated in the
[Manual Configuration Reference](v3net/manual-config.md).

| Log line | Meaning |
|----------|---------|
| `V3Net service started` | V3Net is up; shows `node_id`, `hub` and `leaves` |
| `leaf: SSE connected` / `leaf: SSE reconnecting` | The event stream to the hub came up / dropped and is being retried with backoff |
| `leaf: poll failed` | A periodic poll could not reach the hub |
| `V3Net leaf: message area not found, skipping` | A board tag in the subscription matches no local area |
| `NEW IDENTITY CREATED` | No key file was found and a new identity was generated |
