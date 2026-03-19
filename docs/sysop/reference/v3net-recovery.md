# V3Net Key Recovery

## What Is the Recovery Seed Phrase?

Your V3Net node identity is an Ed25519 keypair stored in a key file (typically
`data/v3net.key`). The **recovery seed phrase** is a set of 24 English words
that encode your private key. If your key file is ever lost or corrupted, you
can use these 24 words to reconstruct the exact same keypair — restoring your
original node ID with zero disruption.

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
./config  →  V3Net  →  Node Identity  →  [S] Show
```

The 24 words are displayed in a 4×6 grid. Press any key to dismiss.

## Exporting to a File

```
./config  →  V3Net  →  Node Identity  →  [E] Export
```

Enter a file path (default: `v3net-recovery.txt`). The file is written with
mode 0600 (owner-only read/write).

**After exporting:** Move the file off-server immediately. Store it somewhere
safe — a password manager, encrypted USB drive, or printed copy in a secure
location. Delete the copy from the server.

## Recovering from a Seed Phrase

```
./config  →  V3Net  →  Node Identity  →  [R] Recover
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

## The Hard Truth

If you lose both your key file **and** your seed phrase, your node identity is
gone permanently. There is no backdoor, no recovery service, no admin override.
The seed phrase is the root of trust — treat it accordingly.
