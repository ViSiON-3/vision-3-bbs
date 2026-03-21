# Joining FelonyNet

FelonyNet is a general-purpose public BBS message network running on **V3Net**,
ViSiON/3's native REST+SSE networking protocol. It coexists alongside
FTN-based echomail networks (FidoNet, fsxNet, etc.) — joining FelonyNet does
not affect your existing FTN configuration.

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

## What You Get

- **Message networking** — public message areas synced across all member BBSes
- **Real-time events** — see who logs on/off across the network instantly
- **Inter-BBS chat** — chat with users on other nodes in real time
- **Zero mailer software** — no FrontDoor, Binkd, or nodelist management
- **Firewall-friendly** — outbound HTTPS only (leaf nodes never need open ports)
- **5-minute setup** — a few screens in the config editor and a restart

## Requirements

- ViSiON/3 BBS (any version with V3Net support)
- Outbound HTTPS access to the FelonyNet hub

---

## Step 1: Enable V3Net

Open the config editor and navigate to the system settings:

```
./config  →  1 — System Configuration  →  2. Server Setup
```

```
┌──────────────────────────────────────────────────────────────────────┐
│                           Server Setup                               │
│                                                                      │
│  ...                                                                 │
│  V3Net           : Y                                                 │
│  Keystore Path   : data/v3net.key                                    │
│  Dedup DB Path   : data/v3net_dedup.sqlite                           │
│  Registry URL    : https://raw.githubusercontent.com/...             │
│                                                                      │
│  V3Net Hub       : N                                                 │
│  Hub Host        :                                                   │
│  Hub Port        : 8765                                              │
│  Hub TLS Cert    :                                                   │
│  Hub TLS Key     :                                                   │
│  Hub Data Dir    :                                                   │
│  Auto Approve    : N                                                 │
│                                                                      │
│                          Screen 2 of 8                               │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  PgUp/PgDn - Screens  |  ESC - Return
```

Set **V3Net** to `Y`, and fill in the **Keystore Path** and **Dedup DB Path**.
Press **S** to save.

> On first start with V3Net enabled, ViSiON/3 generates an Ed25519 keypair at
> the keystore path and derives your permanent node ID from it.

---

## Step 2: Add the FelonyNet Subscription

From the main config menu, open the V3Net networking section:

```
./config  →  4 — ViSiON/3 Networking (V3Net)
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

Select **2. Subscriptions**, then press **I** to launch the **Leaf Setup Wizard**:

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Leaf Setup — Join a Network                      │
│                                                                      │
│  Hub URL         : https://felonynet.org                             │
│  Network         : felonynet                                         │
│  Areas           : (none — press Enter to browse)                    │
│  Poll Interval   : 5m                                                │
│  Origin          : My BBS - bbs.example.com                          │
│                                                                      │
│                         S - Save  |  ESC - Cancel                    │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  S - Save  |  ESC - Back
```

Fill in the fields:

| Field | Value |
|-------|-------|
| Hub URL | `https://felonynet.org` |
| Network | `felonynet` |
| Areas | Press **Enter** to open the area browser and subscribe |
| Poll Interval | `5m` (minimum enforced by hub is 60s) |
| Origin | Your BBS name and address, e.g. `My BBS - bbs.example.com` |

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
./config  →  4 — ViSiON/3 Networking (V3Net)  →  1. Node Identity
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

See [V3Net Key Recovery](recovery.md) for full details.

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
FelonyNet uses auto-approve, so you start receiving messages immediately.

---

## Troubleshooting

**"V3Net networking disabled"** — V3Net is not enabled. Go back to Step 1 and
set **V3Net** to `Y` in System Configuration → Server Setup.

**"message area not found, skipping"** — A subscribed area doesn't match any
local message base. Open the area browser (edit the subscription → Areas) and
verify the local board mapping.

**No messages arriving** — Check that `felonynet.org` is reachable. Look for
`leaf: poll failed` warnings in the log. Verify your node is approved by
checking the V3Net Status screen (`V3NETSTATUS`).

**Lost keypair** — If you have your 24-word recovery seed phrase, restore it
via `./config → 4 → Node Identity → [R] Recover`. If you have lost both the
key file and the seed phrase, a new identity is generated on next start and
you must re-subscribe to all hubs.

---

## Hosting Your Own Network

To run your own V3Net hub (not FelonyNet), see the
[V3Net Configuration](configuration.md#hosting-a-hub) guide.

---

## Related Documentation

- [V3Net Configuration](configuration.md) — all V3Net config screens in the TUI
- [Network Area List (NAL)](nal.md) — area subscriptions, access modes, proposals
- [V3Net Key Recovery](recovery.md) — backing up and restoring your node identity
- [Manual Configuration Reference](manual-config.md) — direct JSON editing
