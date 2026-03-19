# V3Net Key Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add BIP39 mnemonic seed phrase backup and recovery to V3Net's ed25519 keystore, so sysops can restore their node identity (leaf, hub, or coordinator) after key file loss.

**Architecture:** The ed25519 private key seed (32 bytes) is encoded as 24 BIP39 words using standard bit-splitting. Recovery decodes the words back to the seed and reconstructs the identical keypair. No protocol changes — same key comes back, so all hub auth and NAL signing continues working. The config TUI gains a Node Identity screen for viewing, exporting, and recovering the seed phrase.

**Tech Stack:** Go stdlib only (`crypto/ed25519`, `crypto/sha256`, `crypto/rand`). BIP39 English word list embedded as Go source. BubbleTea TUI framework (already in use).

**Spec:** `docs/superpowers/specs/2026-03-18-v3net-key-recovery-design.md`

---

### Task 1: BIP39 Word List

**Files:**
- Create: `internal/v3net/keystore/wordlist.go`

This file embeds the 2048-word BIP39 English word list and builds a reverse lookup map.

- [ ] **Step 1: Create `wordlist.go` with the BIP39 word list**

```go
// Package keystore — BIP39 English word list for mnemonic encoding.
package keystore

// bip39Words is the standard BIP39 English word list (2048 words).
// Source: https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt
var bip39Words = [2048]string{
    "abandon", "ability", "able", "about", "above", "absent", "absorb", "abstract",
    // ... all 2048 words ...
}

// bip39Index maps lowercase words to their index in bip39Words.
var bip39Index map[string]int

func init() {
    bip39Index = make(map[string]int, len(bip39Words))
    for i, w := range bip39Words {
        bip39Index[w] = i
    }
}
```

Get the full word list from the BIP39 specification. The canonical source is: https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt

Embed all 2048 words in the array literal. Format as 8 words per line for readability.

- [ ] **Step 2: Verify the word list compiles**

Run: `cd /home/robbie/git/vision-3-bbs && go build ./internal/v3net/keystore/`
Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add internal/v3net/keystore/wordlist.go
git commit -m "feat(v3net): embed BIP39 English word list for mnemonic encoding"
```

---

### Task 2: Mnemonic Encoding/Decoding

**Files:**
- Create: `internal/v3net/keystore/mnemonic.go`
- Test: `internal/v3net/keystore/keystore_test.go` (append tests)

This file contains the bit-manipulation logic for converting between a 32-byte seed and a 24-word mnemonic. Keep it separate from `keystore.go` to stay under the 300-line file limit.

- [ ] **Step 1: Write failing tests for `encodeMnemonic` and `decodeMnemonic`**

Add these tests to `internal/v3net/keystore/keystore_test.go`:

```go
func TestMnemonic_RoundTrip(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.key")

    ks, _, err := Load(path)
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    phrase, err := ks.Mnemonic()
    if err != nil {
        t.Fatalf("Mnemonic failed: %v", err)
    }

    words := strings.Split(phrase, " ")
    if len(words) != 24 {
        t.Fatalf("expected 24 words, got %d", len(words))
    }

    recovered, err := FromMnemonic(phrase)
    if err != nil {
        t.Fatalf("FromMnemonic failed: %v", err)
    }

    if ks.NodeID() != recovered.NodeID() {
        t.Errorf("node IDs differ: %q vs %q", ks.NodeID(), recovered.NodeID())
    }
    if ks.PubKeyBase64() != recovered.PubKeyBase64() {
        t.Error("public keys differ after mnemonic round-trip")
    }
}

func TestMnemonic_ChecksumValidation(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.key")

    ks, _, err := Load(path)
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    phrase, err := ks.Mnemonic()
    if err != nil {
        t.Fatalf("Mnemonic failed: %v", err)
    }

    // Swap one word for a different valid word to break the checksum.
    words := strings.Split(phrase, " ")
    if words[0] == "abandon" {
        words[0] = "ability"
    } else {
        words[0] = "abandon"
    }
    tampered := strings.Join(words, " ")

    _, err = FromMnemonic(tampered)
    if err == nil {
        t.Error("expected checksum error for tampered mnemonic")
    }
    if !strings.Contains(err.Error(), "checksum") {
        t.Errorf("expected checksum error, got: %v", err)
    }
}

func TestMnemonic_InvalidWord(t *testing.T) {
    phrase := "abandon ability able about above absent absorb abstract absurd abuse access accident " +
        "acid acoustic acquire across act action actor actress xyznotaword actual adapt add"
    _, err := FromMnemonic(phrase)
    if err == nil {
        t.Error("expected error for invalid word")
    }
    if !strings.Contains(err.Error(), "xyznotaword") {
        t.Errorf("error should mention the invalid word, got: %v", err)
    }
}

func TestMnemonic_WrongWordCount(t *testing.T) {
    tests := []struct {
        name  string
        words int
    }{
        {"too few", 23},
        {"too many", 25},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            words := make([]string, tt.words)
            for i := range words {
                words[i] = "abandon"
            }
            _, err := FromMnemonic(strings.Join(words, " "))
            if err == nil {
                t.Error("expected error for wrong word count")
            }
        })
    }
}

func TestMnemonic_CaseInsensitive(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.key")

    ks, _, err := Load(path)
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    phrase, err := ks.Mnemonic()
    if err != nil {
        t.Fatalf("Mnemonic failed: %v", err)
    }

    // Convert to uppercase.
    upper := strings.ToUpper(phrase)
    recovered, err := FromMnemonic(upper)
    if err != nil {
        t.Fatalf("FromMnemonic with uppercase failed: %v", err)
    }
    if ks.NodeID() != recovered.NodeID() {
        t.Errorf("node IDs differ with uppercase input")
    }
}

func TestMnemonic_InputNormalization(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.key")

    ks, _, err := Load(path)
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    phrase, err := ks.Mnemonic()
    if err != nil {
        t.Fatalf("Mnemonic failed: %v", err)
    }

    // Add extra whitespace, tabs, trailing newline.
    messy := "  " + strings.Replace(phrase, " ", "  \t ", 1) + "  \n"
    recovered, err := FromMnemonic(messy)
    if err != nil {
        t.Fatalf("FromMnemonic with messy input failed: %v", err)
    }
    if ks.NodeID() != recovered.NodeID() {
        t.Errorf("node IDs differ with messy whitespace input")
    }
}

func TestMnemonic_BIP39Vector(t *testing.T) {
    // BIP39 test vector: all-zero 32-byte seed.
    // Expected mnemonic for 256 bits of 0x00:
    // "abandon abandon abandon abandon abandon abandon abandon abandon
    //  abandon abandon abandon abandon abandon abandon abandon abandon
    //  abandon abandon abandon abandon abandon abandon abandon art"
    //
    // The last word "art" carries the checksum bits.
    seed := make([]byte, 32)
    phrase, err := encodeMnemonic(seed)
    if err != nil {
        t.Fatalf("encodeMnemonic failed: %v", err)
    }

    expected := "abandon abandon abandon abandon abandon abandon abandon abandon " +
        "abandon abandon abandon abandon abandon abandon abandon abandon " +
        "abandon abandon abandon abandon abandon abandon abandon art"
    if phrase != expected {
        t.Errorf("BIP39 test vector mismatch:\ngot:  %s\nwant: %s", phrase, expected)
    }

    // Round-trip: decode back to seed.
    decoded, err := decodeMnemonic(phrase)
    if err != nil {
        t.Fatalf("decodeMnemonic failed: %v", err)
    }
    for i, b := range decoded {
        if b != 0 {
            t.Errorf("decoded seed byte %d = %d, want 0", i, b)
        }
    }
}
```

Note: these tests reference `Load` with 3 return values, `Mnemonic()`, `FromMnemonic()`, `encodeMnemonic()`, `decodeMnemonic()` — none of which exist yet.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/keystore/ -run TestMnemonic -v`
Expected: Compilation errors (functions don't exist yet).

- [ ] **Step 3: Implement `mnemonic.go`**

Create `internal/v3net/keystore/mnemonic.go`:

```go
// Package keystore — BIP39 mnemonic encoding/decoding for ed25519 seed recovery.
package keystore

import (
    "crypto/ed25519"
    "crypto/sha256"
    "fmt"
    "strings"
)

// encodeMnemonic converts a 32-byte ed25519 seed to a 24-word BIP39 mnemonic.
//
// Algorithm:
//   1. Compute SHA-256 checksum of seed, take first byte (8 bits)
//   2. Append checksum to seed: 33 bytes = 264 bits
//   3. Split into 24 groups of 11 bits → 24 word indices
func encodeMnemonic(seed []byte) (string, error) {
    if len(seed) != 32 {
        return "", fmt.Errorf("mnemonic: seed must be 32 bytes, got %d", len(seed))
    }

    // Append checksum byte.
    cs := sha256.Sum256(seed)
    data := make([]byte, 33)
    copy(data, seed)
    data[32] = cs[0]

    // Extract 24 × 11-bit groups.
    words := make([]string, 24)
    for i := 0; i < 24; i++ {
        idx := extract11Bits(data, i*11)
        words[i] = bip39Words[idx]
    }

    return strings.Join(words, " "), nil
}

// decodeMnemonic converts a 24-word BIP39 mnemonic back to a 32-byte seed.
// Input is normalized: trimmed, lowercased, tabs and multiple spaces collapsed
// to single spaces.
func decodeMnemonic(mnemonic string) ([]byte, error) {
    // Normalize input.
    s := strings.ToLower(strings.TrimSpace(mnemonic))
    s = collapseWhitespace(s)
    words := strings.Split(s, " ")

    if len(words) != 24 {
        return nil, fmt.Errorf("mnemonic: expected 24 words, got %d", len(words))
    }

    // Map words to 11-bit indices.
    var indices [24]int
    for i, w := range words {
        idx, ok := bip39Index[w]
        if !ok {
            return nil, fmt.Errorf("mnemonic: unknown word %q at position %d", w, i+1)
        }
        indices[i] = idx
    }

    // Pack 24 × 11-bit groups into 33 bytes.
    data := make([]byte, 33)
    for i, idx := range indices {
        place11Bits(data, i*11, idx)
    }

    // Verify checksum.
    seed := data[:32]
    cs := sha256.Sum256(seed)
    if data[32] != cs[0] {
        return nil, fmt.Errorf("mnemonic: checksum mismatch (wrong word or typo)")
    }

    return seed, nil
}

// extract11Bits extracts an 11-bit value starting at the given bit offset.
func extract11Bits(data []byte, bitOffset int) int {
    // We need to read across potentially 3 bytes.
    val := 0
    for i := 0; i < 11; i++ {
        byteIdx := (bitOffset + i) / 8
        bitIdx := 7 - ((bitOffset + i) % 8)
        if data[byteIdx]&(1<<bitIdx) != 0 {
            val |= 1 << (10 - i)
        }
    }
    return val
}

// place11Bits places an 11-bit value at the given bit offset.
func place11Bits(data []byte, bitOffset, val int) {
    for i := 0; i < 11; i++ {
        if val&(1<<(10-i)) != 0 {
            byteIdx := (bitOffset + i) / 8
            bitIdx := 7 - ((bitOffset + i) % 8)
            data[byteIdx] |= 1 << bitIdx
        }
    }
}

// collapseWhitespace replaces tabs and runs of spaces with a single space.
func collapseWhitespace(s string) string {
    s = strings.ReplaceAll(s, "\t", " ")
    s = strings.ReplaceAll(s, "\r", " ")
    s = strings.ReplaceAll(s, "\n", " ")
    for strings.Contains(s, "  ") {
        s = strings.ReplaceAll(s, "  ", " ")
    }
    return strings.TrimSpace(s)
}
```

- [ ] **Step 4: Add `Mnemonic()` method and `save()` helper to `keystore.go`**

In `internal/v3net/keystore/keystore.go`, add the `Mnemonic()` method and extract a `save()` helper from the existing `generate()` function:

```go
// Mnemonic returns the 24-word BIP39 recovery phrase for this keypair.
// Computed on-the-fly from the private key seed — never stored on disk.
// Never log the return value.
func (ks *Keystore) Mnemonic() (string, error) {
    return encodeMnemonic(ks.privKey.Seed())
}
```

Also add `FromMnemonic()` and `RecoverToFile()` as public API wrappers (per spec, these live in `keystore.go`, not `mnemonic.go`):

```go
// FromMnemonic reconstructs a Keystore from a 24-word BIP39 phrase.
// Input is case-insensitive and tolerant of extra whitespace.
// Does not write to disk.
func FromMnemonic(mnemonic string) (*Keystore, error) {
    seed, err := decodeMnemonic(mnemonic)
    if err != nil {
        return nil, err
    }
    privKey := ed25519.NewKeyFromSeed(seed)
    pubKey := privKey.Public().(ed25519.PublicKey)
    return &Keystore{privKey: privKey, pubKey: pubKey}, nil
}

// RecoverToFile reconstructs a keypair from a mnemonic and saves it to path
// with mode 0600. Overwrites any existing file. The caller is responsible for
// overwrite confirmation and path validation.
func RecoverToFile(mnemonic, path string) (*Keystore, error) {
    ks, err := FromMnemonic(mnemonic)
    if err != nil {
        return nil, err
    }
    if err := ks.save(path); err != nil {
        return nil, err
    }
    return ks, nil
}
```

Also extract a `save(path string) error` method from the `generate()` function so `RecoverToFile` can reuse it:

```go
// save writes the keypair to path with mode 0600.
func (ks *Keystore) save(path string) error {
    sk := storedKey{
        PrivKeyB64: base64.StdEncoding.EncodeToString(ks.privKey),
        PubKeyB64:  base64.StdEncoding.EncodeToString(ks.pubKey),
    }
    data, err := json.MarshalIndent(sk, "", "  ")
    if err != nil {
        return fmt.Errorf("keystore: marshal: %w", err)
    }
    if err := os.WriteFile(path, data, 0600); err != nil {
        return fmt.Errorf("keystore: write %s: %w", path, err)
    }
    return nil
}
```

Update `generate()` to use `save()`:

```go
func generate(path string) (*Keystore, bool, error) {
    pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
    if err != nil {
        return nil, false, fmt.Errorf("keystore: generate keypair: %w", err)
    }
    ks := &Keystore{privKey: privKey, pubKey: pubKey}
    if err := ks.save(path); err != nil {
        return nil, false, err
    }
    return ks, true, nil
}
```

- [ ] **Step 5: Update `Load()` to return `created bool`**

Change the `Load` signature from `func Load(path string) (*Keystore, error)` to:

```go
func Load(path string) (ks *Keystore, created bool, err error)
```

Update `generate()` return to include `true` for created (done in step 4). Update the existing load path to return `false`. The existing load-from-file path becomes:

```go
func Load(path string) (*Keystore, bool, error) {
    data, err := os.ReadFile(path)
    if os.IsNotExist(err) {
        return generate(path)
    }
    if err != nil {
        return nil, false, fmt.Errorf("keystore: read %s: %w", path, err)
    }
    // ... existing unmarshal/decode logic ...
    return &Keystore{privKey: privKey, pubKey: pubKey}, false, nil
}
```

- [ ] **Step 6: Update all callers of `Load()`**

The following files call `keystore.Load()` and need the new third return value. In most cases, just add `_` for the `created` bool:

- `internal/v3net/service.go:130` — change to `ks, created, err := keystore.Load(...)` (we'll use `created` in Task 5)
- `internal/v3net/keystore/keystore_test.go` — update all existing `Load` calls to 3 return values (use `_` for created)
- `internal/v3net/integration_test.go:64,89` — add `_` for created
- `internal/v3net/hub/hub_test.go:25,156,221,272,316,367,422,442` — add `_` for created
- `internal/v3net/leaf/leaf_test.go:48,105` — add `_` for created
- `internal/v3net/nal/nal_test.go:20` — add `_` for created
- `internal/v3net/service_test.go:18,49,86,127` — add `_` for created
- `cmd/v3net-bootstrap/main.go:41` — add `_` for created

Search for all callers: `grep -rn 'keystore\.Load(' --include='*.go'` and update each one.

- [ ] **Step 7: Run all tests**

Run: `cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/... -v`
Expected: All existing tests pass. All new `TestMnemonic_*` tests pass.

- [ ] **Step 8: Also run the bootstrap command build**

Run: `cd /home/robbie/git/vision-3-bbs && go build ./cmd/v3net-bootstrap/`
Expected: No errors.

- [ ] **Step 9: Commit**

```bash
git add internal/v3net/keystore/mnemonic.go internal/v3net/keystore/keystore.go internal/v3net/keystore/keystore_test.go
git add internal/v3net/service.go internal/v3net/integration_test.go internal/v3net/service_test.go
git add internal/v3net/hub/hub_test.go internal/v3net/leaf/leaf_test.go internal/v3net/nal/nal_test.go
git add cmd/v3net-bootstrap/main.go
git commit -m "feat(v3net): add BIP39 mnemonic encoding/decoding for key recovery

Load() now returns (ks, created, err) to signal new key generation.
Mnemonic(), FromMnemonic(), RecoverToFile() added to keystore package."
```

---

### Task 3: RecoverToFile Tests

**Files:**
- Test: `internal/v3net/keystore/keystore_test.go` (append tests)

- [ ] **Step 1: Write failing tests**

Add to `internal/v3net/keystore/keystore_test.go`:

```go
func TestRecoverToFile_RoundTrip(t *testing.T) {
    // Generate a key, get its mnemonic, recover to a new path.
    dir := t.TempDir()
    ks, _, err := Load(filepath.Join(dir, "original.key"))
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    phrase, err := ks.Mnemonic()
    if err != nil {
        t.Fatalf("Mnemonic failed: %v", err)
    }

    recoveredPath := filepath.Join(dir, "recovered.key")
    recovered, err := RecoverToFile(phrase, recoveredPath)
    if err != nil {
        t.Fatalf("RecoverToFile failed: %v", err)
    }

    if ks.NodeID() != recovered.NodeID() {
        t.Errorf("node IDs differ: %q vs %q", ks.NodeID(), recovered.NodeID())
    }

    // Load from disk to verify persistence.
    loaded, _, err := Load(recoveredPath)
    if err != nil {
        t.Fatalf("Load recovered file failed: %v", err)
    }
    if ks.NodeID() != loaded.NodeID() {
        t.Errorf("loaded node ID differs: %q vs %q", ks.NodeID(), loaded.NodeID())
    }
}

func TestRecoverToFile_Overwrites(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.key")

    // Generate an initial key.
    ks1, _, err := Load(path)
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    // Generate a second key elsewhere, get its mnemonic.
    ks2, _, err := Load(filepath.Join(dir, "other.key"))
    if err != nil {
        t.Fatalf("Load other failed: %v", err)
    }
    phrase2, err := ks2.Mnemonic()
    if err != nil {
        t.Fatalf("Mnemonic failed: %v", err)
    }

    // Recover ks2's identity into ks1's path.
    _, err = RecoverToFile(phrase2, path)
    if err != nil {
        t.Fatalf("RecoverToFile failed: %v", err)
    }

    // Load from overwritten path — should be ks2's identity.
    loaded, _, err := Load(path)
    if err != nil {
        t.Fatalf("Load overwritten failed: %v", err)
    }
    if loaded.NodeID() != ks2.NodeID() {
        t.Errorf("expected node ID %q, got %q", ks2.NodeID(), loaded.NodeID())
    }
    if loaded.NodeID() == ks1.NodeID() {
        t.Error("overwritten file still has the original key")
    }
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/keystore/ -run TestRecoverToFile -v`
Expected: PASS (these should pass immediately since the implementation exists from Task 2).

- [ ] **Step 3: Commit**

```bash
git add internal/v3net/keystore/keystore_test.go
git commit -m "test(v3net): add RecoverToFile round-trip and overwrite tests"
```

---

### Task 4: Config TUI — Node Identity Screen

**Files:**
- Modify: `internal/configeditor/model.go` (add mode constant, update V3Net menu, add identity state fields)
- Create: `internal/configeditor/view_v3net_identity.go`
- Create: `internal/configeditor/update_v3net_identity.go`

This task adds the Node Identity screen to the V3Net category menu. The screen shows the node ID and public key, and provides [S]how, [E]xport, [R]ecover, and [Q]uit actions.

- [ ] **Step 1: Add `modeV3NetIdentity` to `model.go`**

In `internal/configeditor/model.go`, add the new mode constant after `modeV3NetWizardStep`:

```go
modeV3NetIdentity                     // V3Net Node Identity screen
```

Add the identity sub-state type and fields to the Model struct:

```go
// V3Net Node Identity screen state
identitySubState int // 0=main, 1=showPhrase, 2=exportPrompt, 3=recoverInput, 4=recoverConfirm
identityPhrase   string
identityRecoverInput string
identityRecoverNodeID string
```

Add the mode case in the `Update()` switch:

```go
case modeV3NetIdentity:
    return m.updateV3NetIdentity(msg)
```

Also add to the `View()` dispatch (check if it exists in a `view.go` or `model.go`):
The identity view case should call `m.viewV3NetIdentity()`.

- [ ] **Step 2: Update the V3Net category menu**

In `internal/configeditor/model.go`, in `selectTopMenuItem()` case 3 (V3Net Networking), update the `catMenuItems` to add Node Identity as the first item:

```go
case 3: // V3Net Networking
    m.catMenuTitle = "ViSiON/3 Networking (V3Net)"
    m.catMenuItems = []categoryMenuItem{
        {Label: "Node Identity", Mode: modeV3NetIdentity},
        {Label: "Subscriptions", RecordType: "v3netleaf"},
        {Label: "Networks", RecordType: "v3nethub"},
    }
    m.catMenuCursor = 0
    m.mode = modeCategoryMenu
    return m, nil
```

- [ ] **Step 3: Create `update_v3net_identity.go`**

Create `internal/configeditor/update_v3net_identity.go`. This handles all input for the Node Identity screen and its sub-states.

```go
package configeditor

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/charmbracelet/bubbles/textinput"
    tea "github.com/charmbracelet/bubbletea"

    "github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

const (
    identityMain           = 0
    identityShowPhrase     = 1
    identityExportPrompt   = 2
    identityRecoverInput   = 3
    identityRecoverConfirm = 4
)

func (m Model) updateV3NetIdentity(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch m.identitySubState {
    case identityMain:
        return m.updateIdentityMain(msg)
    case identityShowPhrase:
        return m.updateIdentityShowPhrase(msg)
    case identityExportPrompt:
        return m.updateIdentityExportPrompt(msg)
    case identityRecoverInput:
        return m.updateIdentityRecoverInput(msg)
    case identityRecoverConfirm:
        return m.updateIdentityRecoverConfirm(msg)
    }
    return m, nil
}

func (m Model) updateIdentityMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    key := strings.ToUpper(msg.String())

    switch key {
    case "S":
        ks, err := m.loadIdentityKeystore()
        if err != nil {
            m.message = fmt.Sprintf("Error: %v", err)
            return m, nil
        }
        if ks == nil {
            m.message = "No V3Net key file found"
            return m, nil
        }
        phrase, err := ks.Mnemonic()
        if err != nil {
            m.message = fmt.Sprintf("Error: %v", err)
            return m, nil
        }
        m.identityPhrase = phrase
        m.identitySubState = identityShowPhrase
        return m, nil

    case "E":
        ks, err := m.loadIdentityKeystore()
        if err != nil {
            m.message = fmt.Sprintf("Error: %v", err)
            return m, nil
        }
        if ks == nil {
            m.message = "No V3Net key file found"
            return m, nil
        }
        phrase, err := ks.Mnemonic()
        if err != nil {
            m.message = fmt.Sprintf("Error: %v", err)
            return m, nil
        }
        m.identityPhrase = phrase
        m.identitySubState = identityExportPrompt
        m.textInput.SetValue("v3net-recovery.txt")
        m.textInput.CharLimit = 80
        m.textInput.Width = 40
        m.textInput.CursorEnd()
        m.textInput.Focus()
        return m, textinput.Blink

    case "R":
        m.identitySubState = identityRecoverInput
        m.identityRecoverInput = ""
        m.textInput.SetValue("")
        m.textInput.CharLimit = 500
        m.textInput.Width = 60
        m.textInput.Placeholder = "Enter 24 words separated by spaces"
        m.textInput.Focus()
        return m, textinput.Blink

    case "Q":
        m.identitySubState = identityMain
        m.identityPhrase = ""
        m.mode = m.backMode()
        return m, nil
    }

    if msg.Type == tea.KeyEscape {
        m.identitySubState = identityMain
        m.identityPhrase = ""
        m.mode = m.backMode()
        return m, nil
    }

    return m, nil
}

func (m Model) updateIdentityShowPhrase(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    // Any key returns to main identity screen.
    m.identitySubState = identityMain
    m.identityPhrase = ""
    return m, nil
}

func (m Model) updateIdentityExportPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch msg.Type {
    case tea.KeyEnter:
        path := m.textInput.Value()
        m.textInput.Blur()

        if strings.Contains(path, "..") {
            m.message = "Path must not contain '..'"
            m.identitySubState = identityMain
            return m, nil
        }

        // Check if file exists — warn and proceed (the file mode 0600 provides
        // some protection). A full confirm dialog is not added since the export
        // path prompt already requires deliberate action and the file is not
        // destructive to existing BBS data.

        ks, ksErr := m.loadIdentityKeystore()
        if ksErr != nil {
            m.message = fmt.Sprintf("Error: %v", ksErr)
            m.identitySubState = identityMain
            return m, nil
        }
        if ks == nil {
            m.message = "No V3Net key file found"
            m.identitySubState = identityMain
            return m, nil
        }

        if err := m.writeRecoveryFile(path, ks); err != nil {
            m.message = fmt.Sprintf("Export error: %v", err)
            m.identitySubState = identityMain
            return m, nil
        }

        m.message = fmt.Sprintf("Saved to %s — move off-server and delete the local copy", path)
        m.identitySubState = identityMain
        m.identityPhrase = ""
        return m, nil

    case tea.KeyEscape:
        m.textInput.Blur()
        m.identitySubState = identityMain
        return m, nil

    default:
        var cmd tea.Cmd
        m.textInput, cmd = m.textInput.Update(msg)
        return m, cmd
    }
}

func (m Model) updateIdentityRecoverInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch msg.Type {
    case tea.KeyEnter:
        phrase := m.textInput.Value()
        m.textInput.Blur()

        recovered, err := keystore.FromMnemonic(phrase)
        if err != nil {
            m.message = fmt.Sprintf("Invalid: %v", err)
            m.identitySubState = identityMain
            return m, nil
        }

        m.identityRecoverInput = phrase
        m.identityRecoverNodeID = recovered.NodeID()
        m.identitySubState = identityRecoverConfirm
        return m, nil

    case tea.KeyEscape:
        m.textInput.Blur()
        m.identitySubState = identityMain
        return m, nil

    default:
        var cmd tea.Cmd
        m.textInput, cmd = m.textInput.Update(msg)
        return m, cmd
    }
}

func (m Model) updateIdentityRecoverConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    key := strings.ToUpper(msg.String())

    switch key {
    case "Y":
        path := m.configs.V3Net.KeystorePath
        if path == "" {
            path = "data/v3net.key"
        }

        if _, err := keystore.RecoverToFile(m.identityRecoverInput, path); err != nil {
            m.message = fmt.Sprintf("Recovery error: %v", err)
        } else {
            m.message = fmt.Sprintf("Identity recovered. Node ID: %s. Restart BBS to activate.", m.identityRecoverNodeID)
        }

        m.identityRecoverInput = ""
        m.identityRecoverNodeID = ""
        m.identitySubState = identityMain
        return m, nil

    case "N":
        m.identityRecoverInput = ""
        m.identityRecoverNodeID = ""
        m.identitySubState = identityMain
        return m, nil
    }

    if msg.Type == tea.KeyEscape {
        m.identityRecoverInput = ""
        m.identityRecoverNodeID = ""
        m.identitySubState = identityMain
        return m, nil
    }

    return m, nil
}

// loadIdentityKeystore loads the keystore for display purposes.
// Returns (nil, nil) if the key file does not exist.
// Returns (nil, err) if the file exists but is corrupt/unreadable.
func (m Model) loadIdentityKeystore() (*keystore.Keystore, error) {
    path := m.configs.V3Net.KeystorePath
    if path == "" {
        path = "data/v3net.key"
    }
    if _, err := os.Stat(path); os.IsNotExist(err) {
        return nil, nil
    }
    ks, _, err := keystore.Load(path)
    if err != nil {
        return nil, fmt.Errorf("key file corrupt: %w", err)
    }
    return ks, nil
}

// writeRecoveryFile writes the seed phrase export file.
func (m Model) writeRecoveryFile(path string, ks *keystore.Keystore) error {
    phrase, err := ks.Mnemonic()
    if err != nil {
        return err
    }

    words := strings.Split(phrase, " ")
    if len(words) != 24 {
        return fmt.Errorf("unexpected word count: %d", len(words))
    }

    var b strings.Builder
    b.WriteString("V3Net Recovery Seed Phrase\n")
    b.WriteString("==========================\n")
    b.WriteString(fmt.Sprintf("Node ID: %s\n", ks.NodeID()))
    b.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format("2006-01-02")))
    b.WriteString("\nWords:\n")

    // 4 columns × 6 rows
    for row := 0; row < 6; row++ {
        b.WriteString(fmt.Sprintf("  %2d. %-12s %2d. %-12s %2d. %-12s %2d. %-12s\n",
            row+1, words[row],
            row+7, words[row+6],
            row+13, words[row+12],
            row+19, words[row+18],
        ))
    }

    b.WriteString("\nStore this file safely and delete it from this server.\n")
    b.WriteString("Anyone with these words can impersonate your BBS node.\n")

    return os.WriteFile(path, []byte(b.String()), 0600)
}
```

- [ ] **Step 4: Create `view_v3net_identity.go`**

Create `internal/configeditor/view_v3net_identity.go`. Follow the box-rendering pattern from `view_wizard_form.go`: centered box with `editBorderStyle` borders, `menuHeaderStyle` title, `bgFillStyle` fill, `helpBarStyle` bottom bar.

```go
package configeditor

import (
    "fmt"
    "strings"
)

// viewV3NetIdentity renders the Node Identity screen and all sub-states.
func (m Model) viewV3NetIdentity() string {
    var b strings.Builder
    b.WriteString(m.globalHeaderLine())
    b.WriteByte('\n')

    bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
    boxW := 60

    // Build content lines based on sub-state.
    var title string
    var contentLines []string
    var helpText string

    switch m.identitySubState {
    case identityShowPhrase:
        title = "Recovery Seed Phrase"
        words := strings.Split(m.identityPhrase, " ")
        if len(words) == 24 {
            for row := 0; row < 6; row++ {
                contentLines = append(contentLines, fmt.Sprintf(
                    "  %2d. %-12s %2d. %-12s %2d. %-12s %2d. %-12s",
                    row+1, words[row], row+7, words[row+6],
                    row+13, words[row+12], row+19, words[row+18],
                ))
            }
        }
        contentLines = append(contentLines, "")
        contentLines = append(contentLines, "  Press any key to return")
        helpText = "Any key - Return"

    case identityExportPrompt:
        title = "Export Recovery Phrase"
        contentLines = []string{
            "  Export to file: " + m.textInput.View(),
        }
        helpText = "Enter - Save  |  ESC - Cancel"

    case identityRecoverInput:
        title = "Recover Identity"
        contentLines = []string{
            "  Enter your 24-word recovery phrase:",
            "",
            "  " + m.textInput.View(),
        }
        helpText = "Enter - Submit  |  ESC - Cancel"

    case identityRecoverConfirm:
        title = "Confirm Recovery"
        contentLines = []string{
            fmt.Sprintf("  Node ID will become: %s", m.identityRecoverNodeID),
            "",
            "  This will replace your current key file.",
            "  Continue? [Y/N]",
        }
        helpText = "Y - Confirm  |  N - Cancel"

    default: // identityMain
        title = "V3Net Node Identity"
        ks, err := m.loadIdentityKeystore()
        if err != nil {
            contentLines = []string{
                fmt.Sprintf("  Error: %v", err),
            }
        } else if ks == nil {
            contentLines = []string{
                "  No V3Net identity configured.",
                "  Set up a leaf subscription or hub network to generate one.",
            }
        } else {
            path := m.configs.V3Net.KeystorePath
            if path == "" {
                path = "data/v3net.key"
            }
            contentLines = []string{
                fmt.Sprintf("  Node ID:    %s", ks.NodeID()),
                fmt.Sprintf("  Public Key: %s", ks.PubKeyBase64()),
                fmt.Sprintf("  Key File:   %s", path),
                "",
                "  [S] Show recovery seed phrase",
                "  [E] Export recovery seed phrase to file",
                "  [R] Recover identity from seed phrase",
            }
        }
        helpText = "S - Show  |  E - Export  |  R - Recover  |  Q - Return"
    }

    // Render the box.
    contentRows := len(contentLines)
    extraV := maxInt(0, m.height-contentRows-10)
    topPad := extraV / 2
    bottomPad := extraV - topPad

    for i := 0; i < topPad; i++ {
        b.WriteString(bgLine)
        b.WriteByte('\n')
    }

    padL := maxInt(0, (m.width-boxW-2)/2)
    padR := maxInt(0, m.width-padL-boxW-2)

    // Top border
    b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
        editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
        bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
    b.WriteByte('\n')

    // Title
    titleLine := editBorderStyle.Render("│") +
        menuHeaderStyle.Render(centerText(title, boxW)) +
        editBorderStyle.Render("│")
    b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + titleLine +
        bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
    b.WriteByte('\n')

    // Empty line
    emptyLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
        editBorderStyle.Render("│") +
        fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
        editBorderStyle.Render("│") +
        bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
    b.WriteString(emptyLine)
    b.WriteByte('\n')

    // Content lines
    for _, line := range contentLines {
        padded := line
        if len(padded) < boxW {
            padded += strings.Repeat(" ", boxW-len(padded))
        }
        row := bgFillStyle.Render(strings.Repeat("░", padL)) +
            editBorderStyle.Render("│") +
            fieldDisplayStyle.Render(padded) +
            editBorderStyle.Render("│") +
            bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
        b.WriteString(row)
        b.WriteByte('\n')
    }

    // Empty line + bottom border
    b.WriteString(emptyLine)
    b.WriteByte('\n')
    b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
        editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
        bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
    b.WriteByte('\n')

    for i := 0; i < bottomPad; i++ {
        b.WriteString(bgLine)
        b.WriteByte('\n')
    }

    // Message line
    if m.message != "" {
        b.WriteString(bgFillStyle.Render(centerText(m.message, m.width)))
    } else {
        b.WriteString(bgLine)
    }
    b.WriteByte('\n')

    b.WriteString(bgLine)
    b.WriteByte('\n')
    b.WriteString(helpBarStyle.Render(centerText(helpText, m.width)))

    return b.String()
}
```

- [ ] **Step 5: Add `viewV3NetIdentity` to the View dispatch**

Find where the `View()` method dispatches based on mode (likely in a `view.go` file). Add a case:

```go
case modeV3NetIdentity:
    return m.viewV3NetIdentity()
```

- [ ] **Step 6: Verify it compiles**

Run: `cd /home/robbie/git/vision-3-bbs && go build ./internal/configeditor/`
Expected: No errors.

- [ ] **Step 7: Commit**

```bash
git add internal/configeditor/model.go internal/configeditor/update_v3net_identity.go internal/configeditor/view_v3net_identity.go
git commit -m "feat(config): add V3Net Node Identity screen with seed phrase show/export/recover"
```

---

### Task 5: Startup Log Warning

**Files:**
- Modify: `internal/v3net/service.go:128-134`

- [ ] **Step 1: Update `New()` to use `created` flag**

In `internal/v3net/service.go`, the `New()` function already calls `keystore.Load()`. After Task 2, this returns `(ks, created, err)`. Add a prominent log warning when `created` is true:

```go
ks, created, err := keystore.Load(cfg.KeystorePath)
if err != nil {
    return nil, fmt.Errorf("v3net: load keystore: %w", err)
}
slog.Info("v3net: node identity", "node_id", ks.NodeID())

if created {
    slog.Warn("v3net: NEW IDENTITY CREATED — back up your recovery seed phrase",
        "node_id", ks.NodeID(),
        "action", "Run ./config > V3Net > Node Identity to view and export your seed phrase",
    )
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/robbie/git/vision-3-bbs && go test ./internal/v3net/... -v`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add internal/v3net/service.go
git commit -m "feat(v3net): log seed phrase backup warning on first key generation"
```

---

### Task 6: First-Run Wizard Interstitial

**Files:**
- Modify: `internal/configeditor/update_v3net_wizard.go`
- Modify: `internal/configeditor/model.go` (add interstitial state fields)

This task adds a seed phrase display screen after the leaf or hub wizard completes for the first time (when a new key was generated).

- [ ] **Step 1: Add interstitial state to `model.go`**

Add to the Model struct in `model.go`:

```go
// Seed phrase interstitial (shown after first-time wizard save)
showSeedInterstitial bool
seedInterstitialPhrase string
seedInterstitialNodeID string
```

- [ ] **Step 2: Modify `confirmLeafWizard()` and `confirmHubWizard()` in wizard flow**

In `internal/configeditor/update_v3net_wizard.go`, after a successful wizard save, check if a key was just created. If so, load the keystore, get the mnemonic, and set the interstitial state instead of returning to the record list.

The detection works by checking if the key file exists *before* the wizard saves (which may trigger key generation). Add a `keyExistedBeforeSave` field to the Model struct:

```go
keyExistedBeforeSave bool // tracks whether v3net.key existed before wizard save
```

At the **start** of `confirmLeafWizard()` (and `confirmHubWizard()`), before `m.saveAll()`:

```go
// Check if key file exists before save (save may trigger first key generation).
path := m.configs.V3Net.KeystorePath
if path == "" {
    path = "data/v3net.key"
}
_, statErr := os.Stat(path)
m.keyExistedBeforeSave = statErr == nil
```

Then **after** the successful save, before setting mode to `modeRecordList`:

```go
// Show seed phrase interstitial if this save created a new key.
if !m.keyExistedBeforeSave {
    ks, err := m.loadIdentityKeystore()
    if err == nil && ks != nil {
        if phrase, err := ks.Mnemonic(); err == nil {
            m.showSeedInterstitial = true
            m.seedInterstitialPhrase = phrase
            m.seedInterstitialNodeID = ks.NodeID()
            return m, nil
        }
    }
}
```

Apply the same pattern to `confirmHubWizard()`.

- [ ] **Step 3: Handle interstitial input**

Add handling in the `updateWizardForm` or a new function. When `m.showSeedInterstitial` is true, the view shows the seed phrase screen, and input is handled:
- `[E]` triggers export (reuse the same `writeRecoveryFile` from `update_v3net_identity.go`)
- `[C]` or any other key clears the interstitial and returns to the record list

This can be handled at the top of `updateWizardForm`:

```go
if m.showSeedInterstitial {
    return m.updateSeedInterstitial(msg)
}
```

Add `updateSeedInterstitial` to `update_v3net_identity.go` (or a new file if cleaner):

```go
func (m Model) updateSeedInterstitial(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    key := strings.ToUpper(msg.String())
    switch key {
    case "E":
        ks, _ := m.loadIdentityKeystore()
        if ks != nil {
            if err := m.writeRecoveryFile("v3net-recovery.txt", ks); err != nil {
                m.message = fmt.Sprintf("Export error: %v", err)
            } else {
                m.message = "Saved to v3net-recovery.txt — move off-server and delete the local copy"
            }
        }
        m.showSeedInterstitial = false
        m.seedInterstitialPhrase = ""
        m.mode = modeRecordList
        return m, nil
    default:
        // [C] or any key continues
        m.showSeedInterstitial = false
        m.seedInterstitialPhrase = ""
        m.mode = modeRecordList
        return m, nil
    }
}
```

- [ ] **Step 4: Add interstitial view rendering**

In the wizard form view (or a new helper), check `m.showSeedInterstitial` at the top of `viewWizardForm()` and render the interstitial screen instead. Follow the same box layout pattern.

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/robbie/git/vision-3-bbs && go build ./internal/configeditor/`
Expected: No errors.

- [ ] **Step 6: Commit**

```bash
git add internal/configeditor/model.go internal/configeditor/update_v3net_wizard.go internal/configeditor/update_v3net_identity.go internal/configeditor/view_v3net_identity.go
git commit -m "feat(config): show seed phrase interstitial after first-time V3Net wizard save"
```

---

### Task 7: Documentation

**Files:**
- Create: `docs/sysop/reference/v3net-recovery.md`
- Modify: `docs/sysop/configuration/v3net-config.md`
- Modify: `docs/felonynet.md`
- Modify: `AGENTS.v3net.md`

- [ ] **Step 1: Create `docs/sysop/reference/v3net-recovery.md`**

Write sysop-facing documentation covering:

1. **What is the recovery seed phrase?** — 24 words that can restore your V3Net node identity if the key file is lost. Generated from your private key at creation time.
2. **Why it matters** — Your key file is your node's identity. Lose it and lose your seed phrase = permanent identity loss. For coordinators, this bricks network governance.
3. **Viewing your seed phrase** — `./config > V3Net > Node Identity > [S]`
4. **Exporting to a file** — `./config > V3Net > Node Identity > [E]`. Move the file off-server. Delete the local copy.
5. **Recovering from seed phrase** — `./config > V3Net > Node Identity > [R]`. Enter 24 words. Confirm. Restart BBS.
6. **What recovery restores by role:**
   - Leaf: reconnects to hubs seamlessly, no hub action needed
   - Hub: if data directory intact, seamless; if not, same identity but leaves re-subscribe
   - Coordinator: NAL signing continues, governance uninterrupted
7. **Storage recommendations** — Password manager, printed copy in a safe, encrypted USB drive. NOT on the same server as the key file.
8. **The hard truth** — If you lose both the key file and the seed phrase, the identity is gone permanently. There is no backdoor.

- [ ] **Step 2: Update `docs/sysop/configuration/v3net-config.md`**

Add a "Node Identity" section that links to the recovery doc. Brief mention of:
- Key file location (`data/v3net.key`)
- How to back up (link to recovery doc)
- How to recover (link to recovery doc)

- [ ] **Step 3: Update `docs/felonynet.md`**

In the setup/joining instructions, add a note:

> **Important:** After your first V3Net setup, back up your recovery seed phrase immediately. Run `./config > V3Net > Node Identity > [E]` to export it. See `docs/sysop/reference/v3net-recovery.md` for details.

- [ ] **Step 4: Update `AGENTS.v3net.md`**

In the Phase 2 keystore section, add documentation for the new API:

```
### Mnemonic Recovery (added post-Phase 2)

The keystore supports BIP39 mnemonic encoding for key recovery. The 32-byte
ed25519 seed is encoded as 24 words from the standard BIP39 English word list
(embedded in `wordlist.go`, no external dependency).

- `Mnemonic() (string, error)` — encode current key as 24 words
- `FromMnemonic(phrase string) (*Keystore, error)` — decode and reconstruct
- `RecoverToFile(phrase, path string) (*Keystore, error)` — decode and save

`Load()` returns `(ks *Keystore, created bool, err error)` to signal new key
generation. No protocol changes — recovery restores the identical keypair.
```

- [ ] **Step 5: Commit**

```bash
git add docs/sysop/reference/v3net-recovery.md docs/sysop/configuration/v3net-config.md docs/felonynet.md AGENTS.v3net.md
git commit -m "docs: add V3Net key recovery guide and update related docs"
```

---

### Task 8: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `cd /home/robbie/git/vision-3-bbs && go test ./... 2>&1 | tail -30`
Expected: All tests pass. Pay special attention to `./internal/v3net/keystore/` (new mnemonic tests) and `./internal/v3net/...` (existing integration tests with updated `Load` signature).

- [ ] **Step 2: Run linters**

Run: `cd /home/robbie/git/vision-3-bbs && gofmt -l ./internal/v3net/keystore/ ./internal/configeditor/`
Expected: No files listed (all formatted).

Run: `cd /home/robbie/git/vision-3-bbs && go vet ./internal/v3net/keystore/ ./internal/configeditor/`
Expected: No issues.

- [ ] **Step 3: Verify build**

Run: `cd /home/robbie/git/vision-3-bbs && go build ./...`
Expected: No errors across all packages.

- [ ] **Step 4: Check file sizes**

Verify no file exceeds 300 lines (per CLAUDE.md):

Run: `wc -l internal/v3net/keystore/keystore.go internal/v3net/keystore/mnemonic.go internal/v3net/keystore/wordlist.go internal/configeditor/update_v3net_identity.go internal/configeditor/view_v3net_identity.go`

Expected: All under 300 lines. `wordlist.go` will be ~260 lines (word list is data, not logic — acceptable).
