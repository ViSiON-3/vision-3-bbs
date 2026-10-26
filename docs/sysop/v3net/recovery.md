# V3Net Key Recovery

## What Is the Recovery Seed Phrase?

Your V3Net node identity is an Ed25519 keypair stored in a key file (typically
`data/v3net.key`). The **recovery seed phrase** is a set of 24 English words
that encode your private key. If your key file is ever lost or corrupted, you
can use these 24 words to reconstruct the exact same keypair — restoring your
original node ID with zero disruption. It is the same idea as the recovery
phrase for a hardware wallet: write it down once, keep it somewhere safe, and
your board's place on every network survives a dead disk.

The seed phrase is generated from your key at creation time and can be viewed
at any time from the config editor.

## Why It Matters

Your node ID is derived from your keypair. Every hub you subscribe to, every
message you've signed, and (for coordinators) the Network Area List — all
depend on this identity.

- **Leaf node**: Lose your key and you must re-register with every hub.
- **Hub operator**: Subscribers trust your node ID. A new key means a new hub
  identity — existing subscribers will not recognise you.
- **Coordinator**: The NAL is signed by the coordinator's key. Key loss bricks
  network governance entirely.

The seed phrase is your safety net. Without it, identity loss is permanent.

## Viewing Your Seed Phrase

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  1. Node Identity  →  [S] Show
```

The 24 words are displayed in a 4×6 grid. Press any key to dismiss.

## Exporting to a File

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  1. Node Identity  →  [E] Export
```

Enter a file path (default: `v3net-recovery.txt`). The file is written with
mode 0600 (owner-only read/write).

**After exporting:** Move the file off-server immediately. Store it somewhere
safe — a password manager, encrypted USB drive, or printed copy in a secure
location. Delete the copy from the server.

## Recovering from a Seed Phrase

```
./config  →  6 — ViSiON/3 Networking (V3Net)  →  1. Node Identity  →  [R] Recover
```

1. Enter your 24 words separated by spaces (case-insensitive).
2. The tool validates the words and checksum, then shows the resulting node ID.
3. If a key file already exists, you are asked to confirm the replacement.
4. On confirm, the key file is written and you are prompted to restart the BBS.

After restarting, your node resumes with the original identity. No hub-side
action is required.

## Recovery by Role

| Role | Recovery experience |
|------|-------------------|
| **Leaf** | Reconnects to hubs normally. Zero disruption. |
| **Hub** | If data directory survives: seamless. If data also lost: same identity, but leaves must re-subscribe. |
| **Coordinator** | NAL signing continues. Governance uninterrupted. |

## Storage Recommendations

| Do | Don't |
|----|-------|
| Password manager entry | Store on the same server as the key file |
| Printed copy in a safe | Email it to yourself |
| Encrypted USB drive | Save in a shared drive or cloud folder |
| Write it down by hand | Take a screenshot |

## Checking the Key File

`vision3 doctor` reports whether the key file at `keystorePath` exists, is a
valid Ed25519 keypair, and is readable only by the BBS user. It never
generates a replacement key, so running it on a board whose key is missing is
safe. See [vision3 doctor](configuration/configuration.md#doctor).

If the BBS starts with V3Net enabled and finds no key file, it creates a new
identity and logs `NEW IDENTITY CREATED — back up your recovery seed phrase`.
If that was not what you wanted — the file was lost, not new — stop the BBS,
recover from your seed phrase as above, and restart before the new identity
registers anywhere.

## The Hard Truth

If you lose both your key file **and** your seed phrase, your node identity is
gone permanently. There is no backdoor, no recovery service, no admin override.
The seed phrase is the root of trust — treat it accordingly.

---

## Technical Reference

| Item | Detail |
|------|--------|
| Key file | `keystorePath` in `configs/v3net.json`, normally `data/v3net.key`: a JSON file holding `privkey_b64` and `pubkey_b64`, written with mode `0600` |
| Node ID | The first 8 bytes (16 hex characters) of the SHA-256 hash of the public key |
| Seed phrase | 24 BIP39 words encoding the Ed25519 seed, with a checksum; `[R] Recover` rejects a phrase that fails it |
| Export file | Default name `v3net-recovery.txt`, mode `0600` |
| What a restart does | The key is loaded once at startup; a recovered key is used from the next start |

Permissions on the key file are the sysop's job (`chmod 600 data/v3net.key`
on Linux and macOS); `vision3 doctor` warns when group or other users can
read it.
