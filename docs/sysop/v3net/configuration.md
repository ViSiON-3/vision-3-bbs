# V3Net Configuration

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net settings live in two places in the TUI config editor (`./config`):

- **System Configuration → Server Setup** — master enable/disable, file paths, and hub server settings
- **ViSiON/3 Networking (V3Net)** — subscriptions (leaf setup), hosted networks (hub setup), and node identity

> For a step-by-step guide to joining FelonyNet, see [Joining FelonyNet](v3net/felonynet.md).

---

## System Configuration — Server Setup

```
./config  →  1 — System Configuration  →  2. Server Setup
```

```
┌──────────────────────────────────────────────────────────────────────┐
│                           Server Setup                               │
│                                                                      │
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

| Field | Description |
|-------|-------------|
| **V3Net** | Master on/off switch (`Y`/`N`). Must be `Y` for V3Net to start. |
| **Keystore Path** | Path to the Ed25519 keypair file. Auto-generated on first start if absent. Recommended: `data/v3net.key` |
| **Dedup DB Path** | Path to the SQLite message deduplication database. Recommended: `data/v3net_dedup.sqlite` |
| **Registry URL** | Central registry URL for network discovery. Default: `https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json` |
| **V3Net Hub** | Enable the built-in hub server (`Y`/`N`). Only needed if you are hosting your own network. |
| **Hub Host** | Listen address for the hub. Blank means all interfaces. Set to `127.0.0.1` if behind a reverse proxy. |
| **Hub Port** | Listen port for the hub HTTP(S) server. Default: `8765`. |
| **Hub TLS Cert** | Path to TLS certificate (PEM). Leave blank to use plain HTTP or if TLS is terminated at a proxy. |
| **Hub TLS Key** | Path to TLS private key (PEM). Must match the certificate. |
| **Hub Data Dir** | Directory for hub database and NAL files. Recommended: `data/v3net_hub` |
| **Auto Approve** | When `Y`, new leaf subscriptions and area proposals are approved automatically. |

Press **S** to save after making changes.

---

## V3Net Networking Menu

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

| Item | Description |
|------|-------------|
| **Node Identity** | View your node ID and public key; show, export, or recover your seed phrase. |
| **Subscriptions** | Add, edit, and remove leaf connections to remote hubs. Press **I** for the guided setup wizard. |
| **Networks** | Add, edit, and remove hosted network definitions (hub mode only). Press **I** for the hub setup wizard. |

---

## Subscriptions (Joining a Network)

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  2. Subscriptions
```

```
┌──────────────────────────────────────────────────────────────────────┐
│                        V3Net Subscriptions                           │
│   #  Hub URL                          Network        Board           │
│──────────────────────────────────────────────────────────────────────│
│   1  https://felonynet.org            felonynet      fn.general      │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  I - New (Wizard)  |  B - Registry  |  D - Delete  |  S - Save  |  ESC - Return
```

Press **B** to open the **Network Registry** and browse available networks, or
press **I** to open the **Leaf Setup Wizard** manually:

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Leaf Setup — Join a Network                      │
│                                                                      │
│  Registry        : (press Enter to browse available networks)        │
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

| Field | Description |
|-------|-------------|
| **Registry** | Opens the network registry browser. Selecting a network fills in Hub URL and Network automatically. |
| **Hub URL** | Full URL of the hub (e.g. `https://felonynet.org`) |
| **Network** | Network name to subscribe to (e.g. `felonynet`) |
| **Areas** | Press **Enter** to open the area browser and choose which areas to subscribe to |
| **Poll Interval** | How often to check for new messages. Accepts Go durations: `30s`, `5m`, `1h` |
| **Origin** | Origin line appended to outbound messages. Identifies your BBS to readers on other nodes. Blank defaults to BBS name. |

Press **Enter** on **Areas** to browse and subscribe. Press **S** to save.

---

## Hosting a Hub

To host your own V3Net network:

1. Enable the hub server in **System Configuration → Server Setup** (set **V3Net Hub** to `Y`, fill in **Hub Port** and **Hub Data Dir**)
2. Define at least one network via **V3Net Networking → 3. Networks**

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  3. Networks
```

Press **I** to open the **Hub Setup Wizard**:

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Hub Setup — Host a Network                       │
│                                                                      │
│  Network Name    : mynet                                             │
│  Description     : My BBS Network                                    │
│  Listen Port     : 8765                                              │
│  Auto-Approve    : N                                                 │
│                                                                      │
│  Initial Areas   : (none — press Enter to add)                       │
│                                                                      │
│                         S - Save  |  ESC - Cancel                    │
└──────────────────────────────────────────────────────────────────────┘
Enter - Edit  |  S - Save  |  ESC - Back
```

Press **Enter** on **Initial Areas** to add the message areas that will seed
the initial NAL. After saving and restarting, the BBS automatically creates
the data directory, registers the hub, and builds the NAL — no additional
bootstrap steps needed.

For TLS setup, see [V3Net Hub TLS Setup](v3net/hub-tls.md).

---

## Node Identity

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

Your node ID is derived from an Ed25519 keypair. It is your permanent identity
on all V3Net networks. **Back up your seed phrase immediately after first startup**
— if you lose both the key file and the seed phrase, your identity is gone permanently.

See [V3Net Key Recovery](v3net/recovery.md) for full details.

---

## File Locations

| File | Purpose |
|------|---------|
| `configs/v3net.json` | Generated and managed by the config editor |
| `data/v3net.key` | Ed25519 keypair (auto-generated on first V3Net start) |
| `data/v3net_dedup.sqlite` | Message deduplication database |
| `data/v3net_hub/` | Hub data directory (SQLite DB, NAL files) |

---

## Security Considerations

**TLS protects the wire, not the disk.** Enabling HTTPS encrypts traffic between leaf nodes and the hub. It does not encrypt data at rest. The following files are stored in plaintext:

- `data/v3net_dedup.sqlite` — deduplication database containing message content
- `data/v3net_hub/` — hub database (hub nodes only), fully readable by the hub operator and anyone with server access
- Local message base files — JAM or squish files written when messages are imported into boards

Restrict file permissions appropriately and consider the security of any backup systems that touch these paths.

**Protect your private key.** `data/v3net.key` and any exported seed phrase files should be readable only by the BBS process user (`chmod 600`). A compromised key allows an attacker to impersonate your node on all V3Net networks. Do not include the key file in unencrypted backups or commit it to version control.

**Hub operators can read all traffic.** V3Net is a federated public message network. TLS secures the connection to the hub, but the hub stores and forwards messages in plaintext. The hub sysop — and anyone with access to the hub server — can read all messages routed through it. This is the same trust model as FidoNet echomail and similar networks.

**Auto-approve.** Setting `Auto Approve: Y` allows any node to subscribe to your hub and propose new areas without review. Appropriate for open public networks; set it to `N` and manually approve nodes for curated or private networks.

**Public areas are public.** Messages posted to networked areas are replicated to every subscribed node and stored indefinitely on each. Do not post sensitive information in public message areas.

---

## Related Documentation

- [Joining FelonyNet](v3net/felonynet.md) — step-by-step walkthrough for the FelonyNet network
- [Network Area List (NAL)](v3net/nal.md) — area subscriptions, access modes, and proposals
- [V3Net Hub TLS Setup](v3net/hub-tls.md) — enabling HTTPS on a hub
- [V3Net Key Recovery](v3net/recovery.md) — backing up and restoring your node identity
- [Manual Configuration Reference](v3net/manual-config.md) — JSON field reference for `v3net.json`
