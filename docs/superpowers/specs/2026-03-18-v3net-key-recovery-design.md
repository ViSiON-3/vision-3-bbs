# V3Net Key Recovery via Mnemonic Seed Phrase

**Date**: 2026-03-18
**Status**: Design approved, pending implementation
**Scope**: Keystore package, config TUI, documentation

## Problem

V3Net node identity is an ed25519 keypair persisted in a single file (`v3net.key`). The `node_id` is derived from the public key. If the key file is lost, the node gets a new identity and must re-register with all hubs. For coordinator nodes, key loss bricks network governance entirely — the NAL is signed by the coordinator's key, and the coordinator transfer mechanism requires the old key to initiate.

This is an unacceptable single point of failure across all three roles (leaf, hub, coordinator).

## Solution

Encode the ed25519 private key seed as a 24-word BIP39 mnemonic phrase. The mnemonic is displayed to the sysop at key creation time and available on demand from the config TUI. Recovery from the mnemonic reconstructs the identical keypair, restoring the original `node_id` with zero protocol-level disruption.

**No protocol changes.** Because the same key comes back, hubs, leaves, NAL signatures, and all wire-level authentication continue working unchanged.

## Approach: Key-First Mnemonic Encoding

The keypair is generated from `crypto/rand` as today. The mnemonic is an encoding of the existing 32-byte ed25519 seed into human-readable words — not a derivation input. This preserves full entropy and keeps the key generation path unchanged.

### Encoding (private key to mnemonic)

1. Extract the 32-byte ed25519 seed from the private key (`privKey.Seed()`)
2. Compute SHA-256 of the seed, take the first byte (8 bits) as checksum
3. Append the checksum byte to the seed: 33 bytes (264 bits)
4. Split into 24 groups of 11 bits: 24 indices into the BIP39 word list
5. Return 24 space-separated words

### Decoding (mnemonic to private key)

1. Map each word to its 11-bit index (case-insensitive)
2. Concatenate: 264 bits = 33 bytes
3. Last byte is checksum — verify against SHA-256 of the first 32 bytes
4. Use the 32-byte seed with `ed25519.NewKeyFromSeed()` to reconstruct the full keypair

### BIP39 Word List

The standard BIP39 English word list (2048 words) is embedded as a Go source file. No external dependency. Total size ~12KB.

**File**: `internal/v3net/keystore/wordlist.go`

Contents: A `var wordlist = [2048]string{...}` array and a reverse lookup `map[string]int` built in `init()`.

## Keystore API Changes

**File**: `internal/v3net/keystore/keystore.go`

New exported functions:

```go
// Mnemonic returns the 24-word BIP39 recovery phrase for this keypair.
// The phrase is computed on-the-fly from the private key seed and is
// never stored on disk.
func (ks *Keystore) Mnemonic() (string, error)

// FromMnemonic reconstructs a Keystore from a 24-word BIP39 phrase.
// Returns an error if the word count is wrong, any word is not in
// the word list, or the checksum fails. Does not write to disk.
func FromMnemonic(mnemonic string) (*Keystore, error)

// RecoverToFile reconstructs a keypair from a mnemonic and saves it
// to the given path with mode 0600. Overwrites any existing file.
func RecoverToFile(mnemonic, path string) (*Keystore, error)
```

Existing functions (`Load`, `generate`, `NodeID`, `Sign`, etc.) are unchanged. The on-disk key file format is unchanged.

## BBS Startup Behavior

**File**: `internal/v3net/service.go`

When `keystore.Load()` creates a new key (file didn't exist), log a prominent warning:

```
V3Net identity created. Node ID: a3f9e1b2c4d5e6f7
IMPORTANT: Run ./config and go to V3Net > Node Identity to view
and back up your recovery seed phrase. This phrase is the ONLY way
to recover your node identity if the key file is lost.
```

No interactive prompt at startup — the BBS may be running headless.

## Config TUI Changes

### V3Net Category Menu

The V3Net category menu gains a new first item:

1. **Node Identity** (new)
2. Subscriptions (leaf)
3. Networks (hub)

### Node Identity Screen

A read-only info screen (not a form):

```
+-------------------------------------------------------+
|               V3Net Node Identity                      |
|                                                        |
|  Node ID:    a3f9e1b2c4d5e6f7                          |
|  Public Key: dGhpcyBpcyBhIGJhc2U2NCBlbmNvZGVkLi4u     |
|  Key File:   data/v3net.key                            |
|                                                        |
|  [S] Show recovery seed phrase                         |
|  [E] Export recovery seed phrase to file                |
|  [R] Recover identity from seed phrase                  |
|  [Q] Return                                            |
+-------------------------------------------------------+
```

If no key exists yet: "No V3Net identity configured. Set up a leaf subscription or hub network to generate one." and only `[Q]`.

**[S] Show seed phrase**: Replaces screen content with numbered 24-word grid (4 columns of 6). Press any key to return.

**[E] Export to file**: Prompts for file path (default: `v3net-recovery.txt`). Writes:

```
V3Net Recovery Seed Phrase
==========================
Node ID: a3f9e1b2c4d5e6f7
Generated: 2026-03-18

Words:
  1. abandon    7. crouch   13. maple   19. silver
  2. brick      8. dolphin  14. notify  20. timber
  3. canal      9. escape   15. ocean   21. unveil
  4. device    10. fossil   16. planet  22. voyage
  5. energy    11. guitar   17. quarter 23. width
  6. fever     12. ivory    18. rhythm  24. youth

Store this file safely and delete it from this server.
Anyone with these words can impersonate your BBS node.
```

After writing, shows confirmation: "Saved to v3net-recovery.txt — move this file off-server and delete the local copy."

**[R] Recover from seed phrase**: Text input for 24 words (space-separated, case-insensitive). On submit:

1. Validate mnemonic (word count, all words in word list, checksum)
2. Derive keypair, show resulting node ID
3. If a key file already exists: "This will replace your current key file. Node ID will become: a3f9e1b2c4d5e6f7. Continue? [Y/N]"
4. If no key exists: confirm and save
5. On confirm: write key file, reload keystore

### First-Run Wizard Interstitial

When a leaf or hub wizard completes and a key was just generated (first-time V3Net setup), show an interstitial screen before returning to the menu:

```
+-------------------------------------------------------+
|          V3Net Node Identity Created                   |
|                                                        |
|  Node ID: a3f9e1b2c4d5e6f7                             |
|                                                        |
|  Your recovery seed phrase:                            |
|                                                        |
|    1. abandon    7. crouch   13. maple   19. silver    |
|    2. brick      8. dolphin  14. notify  20. timber    |
|    3. canal      9. escape   15. ocean   21. unveil    |
|    4. device    10. fossil   16. planet  22. voyage    |
|    5. energy    11. guitar   17. quarter 23. width     |
|    6. fever     12. ivory    18. rhythm  24. youth     |
|                                                        |
|  Write down these 24 words and store them safely.      |
|  This phrase can restore your node identity if your    |
|  key file is ever lost.                                |
|                                                        |
|  [E] Export to file   [C] Continue                     |
+-------------------------------------------------------+
```

`[E]` prompts for file path and writes the same format as the Node Identity screen export. `[C]` continues to the menu.

## Recovery Scenarios

### Leaf loses key file

1. Sysop enters mnemonic via `./config > V3Net > Node Identity > [R]`
2. Same keypair restored, same `node_id`
3. On next BBS startup, leaf authenticates to hubs normally
4. **No hub-side action required. Zero disruption.**

### Hub loses key file

1. Sysop recovers key from mnemonic
2. Hub's `node_id` and pubkey restored
3. If hub data directory survives (SQLite databases intact): seamless recovery
4. If data directory also lost: hub comes back with same identity but empty subscriber list — leaves re-subscribe to the same trusted `node_id`

### Coordinator loses key file

1. Sysop recovers key from mnemonic
2. `coordinator_node_id` and `coordinator_pubkey_b64` in the NAL match the recovered key
3. Coordinator can immediately sign new NAL updates, approve proposals, manage areas
4. **Governance continues uninterrupted.**

### Sysop loses both key file AND mnemonic

Identity is permanently lost. This is by design — there must be a root of trust. The mnemonic is that root. Documentation makes this explicit.

## Testing

Additions to `internal/v3net/keystore/keystore_test.go`:

| Test | Description |
|---|---|
| `TestMnemonic_RoundTrip` | Generate key, encode to mnemonic, decode, assert same `node_id` and public key |
| `TestMnemonic_ChecksumValidation` | Swap one word for another valid word, assert checksum error |
| `TestMnemonic_InvalidWord` | Include a word not in the BIP39 list, assert descriptive error |
| `TestMnemonic_WrongWordCount` | 23 words and 25 words both return errors |
| `TestMnemonic_CaseInsensitive` | Uppercase/mixed-case input decodes to same key |
| `TestRecoverToFile_RoundTrip` | Recover to temp path, `Load()` that path, assert same `node_id` |
| `TestRecoverToFile_Overwrites` | Existing key file at path is replaced, new key matches mnemonic |

No integration test changes — recovery is entirely within the keystore package. Protocol, hub, and leaf are unaffected.

## Documentation

### New file: `docs/sysop/reference/v3net-recovery.md`

Sysop-facing, plain language:

- What the recovery seed phrase is and why it matters
- When it's shown (first run, config TUI)
- How to export it to a file
- How to recover (step-by-step via config TUI)
- What each role experiences during recovery
- The hard truth: lose both key file and mnemonic = permanent identity loss
- Storage recommendations (password manager, printed copy, not on the same server)

### Updates to existing docs

- `docs/sysop/configuration/v3net-config.md` — add Node Identity section, link to recovery doc
- `docs/felonynet.md` — note in setup instructions about backing up the seed phrase
- `AGENTS.v3net.md` — document mnemonic API in Phase 2 keystore section, note no protocol changes

## Files Changed

| File | Change |
|---|---|
| `internal/v3net/keystore/keystore.go` | Add `Mnemonic()`, `FromMnemonic()`, `RecoverToFile()` |
| `internal/v3net/keystore/wordlist.go` | New — BIP39 2048-word list + reverse map |
| `internal/v3net/keystore/keystore_test.go` | 7 new test cases |
| `internal/v3net/service.go` | Log warning on first key creation |
| `internal/configeditor/model.go` | Add Node Identity to V3Net category menu |
| `internal/configeditor/view_v3net_identity.go` | New — Node Identity screen rendering |
| `internal/configeditor/update_v3net_identity.go` | New — Node Identity screen input handling |
| `internal/configeditor/update_v3net_wizard.go` | Add seed phrase interstitial after first-time wizard |
| `docs/sysop/reference/v3net-recovery.md` | New — sysop recovery guide |
| `docs/sysop/configuration/v3net-config.md` | Add Node Identity section |
| `docs/felonynet.md` | Add seed phrase backup note |
| `AGENTS.v3net.md` | Document mnemonic API |

## Non-Goals

- Key rotation (changing to a new key while preserving subscriptions) — separate feature
- Multi-sig or threshold recovery — unnecessary complexity for BBS scale
- Hub-assisted recovery (peer vouching) — adds trust/social layer, doesn't solve coordinator case
- Deterministic key derivation from passphrase — weaker entropy, moves the problem instead of solving it
