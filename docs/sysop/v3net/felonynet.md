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

Open the config editor and go to the system settings screen:

```
./config  →  1 — System Configuration  →  Server Setup
```

Scroll down to the V3Net section and set the following fields:

| Field | Value |
|-------|-------|
| V3Net | `Y` (enabled) |
| Keystore Path | `data/v3net.key` |
| Dedup DB Path | `data/v3net_dedup.sqlite` |
| Registry URL | `https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json` |

![V3Net system settings screen](images/v3net/system-settings.png)

Press **[S] Save** when done.

> On first start, ViSiON/3 generates an Ed25519 keypair at the keystore path and
> derives your permanent node ID from it.

---

## Step 2: Add the FelonyNet Subscription

From the main config menu, go to the V3Net networking section:

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Subscriptions
```

![V3Net subscriptions list (empty)](images/v3net/subscriptions-empty.png)

Press **[I] Insert** to launch the **Leaf Setup Wizard**.

### Wizard Screen 1 — Hub URL

Enter the FelonyNet hub address:

```
Hub URL: https://felonynet.org
```

![Leaf wizard: hub URL](images/v3net/leaf-wizard-hub-url.png)

Press **Enter** to continue.

### Wizard Screen 2 — Network & Board

| Field | Value |
|-------|-------|
| Network | `felonynet` |
| Board Tag | A short prefix for local area tags (e.g. `fn`) |

![Leaf wizard: network and board tag](images/v3net/leaf-wizard-network.png)

The board tag becomes the prefix for local message area names when areas
auto-create from the NAL. Press **Enter** to continue.

### Wizard Screen 3 — Poll Interval & Origin

| Field | Value |
|-------|-------|
| Poll Interval | `5m` (5 minutes — minimum enforced by hub is 60s) |
| Origin Line | Your BBS name and address, e.g. `My BBS - bbs.example.com` |

![Leaf wizard: poll interval and origin](images/v3net/leaf-wizard-origin.png)

The origin line identifies your BBS to readers on other nodes. Press **[S] Save**.

![Leaf wizard complete — subscription saved](images/v3net/subscriptions-saved.png)

---

## Step 3: Back Up Your Identity

Your node ID is derived from an Ed25519 keypair. If you lose this key and have
no backup, you must re-register with all hubs from scratch.

**Do this now, before your first restart:**

```
./config  →  4 — ViSiON/3 Networking (V3Net)  →  Node Identity  →  [E] Export
```

![Node identity screen](images/v3net/node-identity.png)

Enter a file path (default: `v3net-recovery.txt`). Move that file off-server
immediately — password manager, encrypted USB, or printed copy in a safe place.

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

On startup you should see:

```
INFO: V3Net service started (node_id=a3f9e1b2c4d5e6f7, hub=false, leaves=1)
```

Your node automatically subscribes to the FelonyNet hub on first connection.
FelonyNet uses auto-approve, so you start receiving messages immediately.

---

## Step 5: Subscribe to Areas

From the BBS sysop menu, select **V3Net > Area Subscriptions** (or run the
`V3NETAREAS` menu command). You'll see all areas FelonyNet offers:

![V3Net area browser](images/v3net/area-browser.png)

- Press **Space** to subscribe or unsubscribe to an area
- Press **E** to set the local message base name for that area
- Status shows `ACTIVE` (subscribed), `PENDING` (awaiting approval), or blank

Most FelonyNet areas use **open** access — subscribe and you're in immediately.

---

## Step 6: Verify

From the sysop admin menu, run the **V3Net Status** screen (`V3NETSTATUS`) to
confirm your node ID, subscription count, and connected networks:

![V3Net status screen](images/v3net/status-screen.png)

---

## Troubleshooting

**"V3Net networking disabled"** — V3Net is not enabled. Go back to Step 1 and
make sure **V3Net** is set to `Y` in System Configuration → Server Setup.

**"message area not found, skipping"** — A board tag in your subscription
doesn't match any configured message area. Check your area tags in the
Message Areas config screen.

**No messages arriving** — Check that `felonynet.org` is reachable from your
server. Look for `leaf: poll failed` in the log. Verify your node is approved
(check V3Net Status).

**Lost keypair** — If you have your 24-word recovery seed phrase, restore it
via `./config → 4 → Node Identity → [R] Recover`. If you have lost both the
key file and the seed phrase, a new identity is generated on next start and
you must re-subscribe to all hubs from scratch.

---

## Hosting Your Own Network

If you want to run your own V3Net hub (not FelonyNet), see the
[V3Net Configuration](v3net/configuration.md#hosting-a-hub) guide.

---

## Related Documentation

- [V3Net Configuration](v3net/configuration.md) — all V3Net config screens in the TUI
- [Network Area List (NAL)](v3net/nal.md) — area subscriptions, access modes, proposals
- [V3Net Key Recovery](v3net/recovery.md) — backing up and restoring your node identity
- [Manual Configuration Reference](v3net/manual-config.md) — direct JSON editing
