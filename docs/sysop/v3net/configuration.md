# V3Net Configuration

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice. Use it only if you are testing or contributing
> to V3Net development. Do not rely on it for a live BBS.

V3Net settings live in two places in the TUI config editor (`./config`):

- **System Configuration → Server Setup** — master enable/disable, file paths, and hub server settings
- **ViSiON/3 Networking (V3Net)** — subscriptions (leaf setup), hosted networks (hub setup), and node identity

> For a step-by-step guide to joining FelonyNet specifically, see
> [Joining FelonyNet](v3net/felonynet.md).

---

## System Configuration — Server Setup

```
./config  →  1 — System Configuration  →  Server Setup
```

Scroll down to the **V3Net** section. This is where you enable V3Net and set the
global paths and hub server parameters.

![V3Net fields in System Configuration → Server Setup](images/v3net/system-settings.png)

| Field | Description |
|-------|-------------|
| **V3Net** | Master on/off switch. Set to `Y` to activate V3Net on startup. |
| **Keystore Path** | Path to the Ed25519 keypair file. Auto-generated on first start if absent. Recommended: `data/v3net.key` |
| **Dedup DB Path** | Path to the SQLite message deduplication database. Recommended: `data/v3net_dedup.sqlite` |
| **Registry URL** | Central registry URL for network discovery. Leave blank to disable the registry browser. Default: `https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json` |
| **V3Net Hub** | Enable the built-in hub server. Only needed if you are hosting your own network. |
| **Hub Host** | Listen address for the hub. Blank means all interfaces. Set to `127.0.0.1` if behind a reverse proxy. |
| **Hub Port** | Listen port for the hub HTTP(S) server. Default: `8765`. |
| **Hub TLS Cert** | Path to TLS certificate (PEM). Leave blank to use plain HTTP or if TLS is terminated at a proxy. |
| **Hub TLS Key** | Path to TLS private key (PEM). Must match the certificate. |
| **Hub Data Dir** | Directory for hub database and NAL files. Recommended: `data/v3net_hub` |
| **Auto Approve** | When `Y`, new leaf subscriptions and area proposals are approved automatically. Useful for testing; leave `N` for production networks. |

Press **[S] Save** after making changes.

---

## V3Net Networking Menu

```
./config  →  4 — ViSiON/3 Networking (V3Net)
```

This menu has three items:

![V3Net networking menu](images/v3net/networking-menu.png)

| Item | Description |
|------|-------------|
| **Node Identity** | View your node ID and public key; show, export, or recover your seed phrase. |
| **Subscriptions** | Add, edit, and remove leaf entries (connections to remote hubs). Press **[I]** to launch the guided leaf setup wizard. |
| **Networks** | Add, edit, and remove hosted network definitions (only needed if running a hub). Press **[I]** to launch the guided hub setup wizard. |

---

## Subscriptions (Joining a Network)

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Subscriptions
```

Each subscription connects your BBS as a leaf node to one network on one hub.
You can add multiple subscriptions for different networks.

![Subscriptions list](images/v3net/subscriptions-list.png)

Press **[I] Insert** to open the **Leaf Setup Wizard**. It walks you through:

1. Hub URL (e.g. `https://felonynet.org`)
2. Network name (e.g. `felonynet`)
3. Board tag prefix (used when message areas auto-create from the NAL)
4. Poll interval (how often to check for new messages; default `5m`)
5. Origin line (your BBS name/address, appended to outbound messages)

![Leaf setup wizard](images/v3net/leaf-wizard-hub-url.png)

Press **[E] Edit** on an existing subscription to change its settings.
Press **[D] Delete** to remove a subscription.

---

## Hosting a Hub

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Networks
```

To host your own V3Net network, you need two things:

1. Enable the hub server in **System Configuration → Server Setup** (set **V3Net Hub** to `Y` and fill in **Hub Port** and **Hub Data Dir**)
2. Define at least one network in **Networks**

Press **[I] Insert** to open the **Hub Setup Wizard**. It walks you through:

1. Network name (short lowercase identifier, e.g. `mynet`)
2. Description (shown to subscribers)
3. Initial message areas to seed the NAL (tag and display name for each area)

![Hub setup wizard](images/v3net/hub-wizard.png)

After saving and restarting, the BBS automatically:
- Creates the data directory
- Registers the hub as an active subscriber for its own networks
- Seeds the initial NAL from the areas you defined

You do not need to run any bootstrap tools — the BBS handles it on first start.

For TLS setup, see [V3Net Hub TLS Setup](v3net/hub-tls.md).

---

## Node Identity

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Node Identity
```

Your V3Net node identity is an Ed25519 keypair. The 16-character hex node ID
derived from it is your permanent identity on all networks.

![Node identity screen](images/v3net/node-identity.png)

From this screen you can:

| Key | Action |
|-----|--------|
| **[S] Show** | Display the 24-word recovery seed phrase on screen |
| **[E] Export** | Write the seed phrase to a file (mode 0600) |
| **[R] Recover** | Restore a keypair from a previously saved seed phrase |

**Back up your seed phrase immediately after first startup.** If you lose
both the key file and the seed phrase, your node identity is permanently gone.

See [V3Net Key Recovery](v3net/recovery.md) for full details.

---

## File Locations

| File | Purpose |
|------|---------|
| `configs/v3net.json` | Generated by the config editor — do not edit manually unless necessary |
| `data/v3net.key` | Ed25519 keypair (auto-generated on first V3Net start) |
| `data/v3net_dedup.sqlite` | Message deduplication database |
| `data/v3net_hub/` | Hub data directory (SQLite DB, NAL files) |

---

## Related Documentation

- [Joining FelonyNet](v3net/felonynet.md) — step-by-step walkthrough for the FelonyNet network
- [Network Area List (NAL)](v3net/nal.md) — area subscriptions, access modes, and proposals
- [V3Net Hub TLS Setup](v3net/hub-tls.md) — enabling HTTPS on a hub
- [V3Net Key Recovery](v3net/recovery.md) — backing up and restoring your node identity
- [Manual Configuration Reference](v3net/manual-config.md) — JSON field reference for `v3net.json`
