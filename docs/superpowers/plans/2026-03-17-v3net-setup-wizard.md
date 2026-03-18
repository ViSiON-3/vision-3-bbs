# V3Net Setup Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate manual JSON editing for V3Net setup by adding hub startup auto-init and guided TUI setup wizards in `./config`.

**Architecture:** Three layers: (1) config schema gains `InitialAreas` + `ConfigPath`; (2) `v3net.Service.New()` auto-inits the hub data dir, self-registers the hub node, and seeds the NAL from `InitialAreas`; (3) `./config` gains a new "E — V3Net Setup" menu item with leaf and hub wizard flows implemented in two new files.

**Tech Stack:** Go stdlib, BubbleTea (`github.com/charmbracelet/bubbletea`), SQLite (`modernc.org/sqlite`), `internal/v3net/*`, `internal/configeditor/*`, `internal/config`.

**Spec:** `docs/superpowers/specs/2026-03-17-v3net-setup-wizard-design.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/config/config.go` | Modify | Add `V3NetHubArea`, `InitialAreas`, `ConfigPath` |
| `cmd/vision3/main.go` | Modify | Populate `ConfigPath` before calling `v3net.New()` |
| `internal/v3net/hub/hub.go` | Modify | Add `NALStore()` accessor |
| `internal/v3net/service.go` | Modify | Add `hubAutoInit()`, call from `New()` |
| `internal/v3net/service_test.go` | Create | Tests for `hubAutoInit` |
| `internal/configeditor/model.go` | Modify | New modes, `wizardState`, `wizardArea`, menu item E, `selectTopMenuItem` update |
| `internal/configeditor/update_v3net_wizard.go` | Create | All wizard update/logic: fork, leaf steps, hub steps, confirm handlers, auto-fetch cmd |
| `internal/configeditor/view_v3net_wizard.go` | Create | Fork screen + wizard step rendering |
| `internal/configeditor/view.go` | Modify | Add wizard modes to `View()` switch |
| `internal/configeditor/wizard_test.go` | Create | Model update tests for wizard state transitions |

---

## Task 1: Config Schema — `V3NetHubArea`, `InitialAreas`, `ConfigPath`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/vision3/main.go:1795`

- [ ] **Step 1: Add `V3NetHubArea` struct and `InitialAreas` field**

  In `internal/config/config.go`, after the `V3NetHubNetwork` struct (line ~913), add:

  ```go
  // V3NetHubArea is an area spec written by the setup wizard and consumed
  // once at hub startup to seed the initial NAL.
  type V3NetHubArea struct {
      Tag  string `json:"tag"`
      Name string `json:"name"`
  }
  ```

  In `V3NetHubConfig` (line ~892), add after `Networks`:

  ```go
  InitialAreas []V3NetHubArea `json:"initialAreas,omitempty"`
  ```

- [ ] **Step 2: Add `ConfigPath` to `V3NetConfig`**

  In `V3NetConfig` (line ~882), add after `RegistryURL`:

  ```go
  // ConfigPath is the configs directory path. Set at runtime, not persisted.
  ConfigPath string `json:"-"`
  ```

  The `json:"-"` tag ensures it is never written to or read from `v3net.json`.

- [ ] **Step 3: Populate `ConfigPath` at call site in main.go**

  In `cmd/vision3/main.go`, after line 1795 (`v3netConfig, v3netCfgErr := config.LoadV3NetConfig(rootConfigPath)`), add:

  ```go
  v3netConfig.ConfigPath = rootConfigPath
  ```

- [ ] **Step 4: Verify compilation**

  ```bash
  go build ./...
  ```

  Expected: no errors.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/config/config.go cmd/vision3/main.go
  git commit -m "feat(v3net): add V3NetHubArea, InitialAreas, ConfigPath to config schema"
  ```

---

## Task 2: Hub `NALStore()` Accessor

**Files:**
- Modify: `internal/v3net/hub/hub.go`

- [ ] **Step 1: Add the accessor**

  In `internal/v3net/hub/hub.go`, after the existing `Subscribers()` method (line ~147), add:

  ```go
  // NALStore returns the NAL store (used for in-process seeding at startup).
  func (h *Hub) NALStore() *NALStore {
      return h.nalStore
  }
  ```

- [ ] **Step 2: Verify compilation**

  ```bash
  go build ./internal/v3net/hub/...
  ```

  Expected: no errors.

- [ ] **Step 3: Commit**

  ```bash
  git add internal/v3net/hub/hub.go
  git commit -m "feat(v3net): expose NALStore accessor on Hub for in-process seeding"
  ```

---

## Task 3: Hub Startup Auto-Init

**Files:**
- Modify: `internal/v3net/service.go`
- Create: `internal/v3net/service_test.go`

The `hubAutoInit` function performs three steps in order:
1. Create the hub data directory.
2. Self-register the hub node as an `active` subscriber for each network.
3. If `InitialAreas` is non-empty and no NAL exists, build + sign + store the NAL, then clear `InitialAreas` from the config file.

- [ ] **Step 1: Write failing tests**

  Create `internal/v3net/service_test.go`:

  ```go
  package v3net_test

  import (
      "os"
      "path/filepath"
      "testing"

      "github.com/ViSiON-3/vision-3-bbs/internal/config"
      "github.com/ViSiON-3/vision-3-bbs/internal/v3net"
      "github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
      "github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
  )

  // newTestService creates a minimal Service with hub enabled for testing auto-init.
  func newTestService(t *testing.T) (*v3net.Service, string) {
      t.Helper()
      dir := t.TempDir()
      configsDir := filepath.Join(dir, "configs")
      dataDir := filepath.Join(dir, "data")
      if err := os.MkdirAll(configsDir, 0755); err != nil {
          t.Fatal(err)
      }

      hubDataDir := filepath.Join(dataDir, "v3net_hub")
      keystorePath := filepath.Join(dataDir, "v3net.key")

      // Pre-generate keystore.
      if _, err := keystore.Load(keystorePath); err != nil {
          t.Fatalf("load keystore: %v", err)
      }

      cfg := config.V3NetConfig{
          Enabled:      true,
          KeystorePath: keystorePath,
          DedupDBPath:  filepath.Join(dataDir, "dedup.sqlite"),
          ConfigPath:   configsDir,
          Hub: config.V3NetHubConfig{
              Enabled:     true,
              Port:        0, // random port, not started
              DataDir:     hubDataDir,
              AutoApprove: true,
              Networks: []config.V3NetHubNetwork{
                  {Name: "testnet", Description: "Test network"},
              },
          },
      }

      svc, err := v3net.New(cfg)
      if err != nil {
          t.Fatalf("v3net.New: %v", err)
      }
      t.Cleanup(func() { svc.Close() })
      return svc, configsDir
  }

  func TestHubAutoInit_DataDirCreated(t *testing.T) {
      dir := t.TempDir()
      hubDataDir := filepath.Join(dir, "data", "v3net_hub")
      keystorePath := filepath.Join(dir, "v3net.key")

      if _, err := keystore.Load(keystorePath); err != nil {
          t.Fatal(err)
      }

      cfg := config.V3NetConfig{
          Enabled:      true,
          KeystorePath: keystorePath,
          DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
          ConfigPath:   dir,
          Hub: config.V3NetHubConfig{
              Enabled:  true,
              DataDir:  hubDataDir,
              Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
          },
      }

      svc, err := v3net.New(cfg)
      if err != nil {
          t.Fatalf("v3net.New: %v", err)
      }
      defer svc.Close()

      if _, err := os.Stat(hubDataDir); os.IsNotExist(err) {
          t.Error("expected hub data dir to be created, but it does not exist")
      }
  }

  func TestHubAutoInit_SelfRegistered(t *testing.T) {
      dir := t.TempDir()
      keystorePath := filepath.Join(dir, "v3net.key")

      ks, err := keystore.Load(keystorePath)
      if err != nil {
          t.Fatal(err)
      }

      cfg := config.V3NetConfig{
          Enabled:      true,
          KeystorePath: keystorePath,
          DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
          ConfigPath:   dir,
          Hub: config.V3NetHubConfig{
              Enabled:  true,
              DataDir:  dir,
              Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
          },
      }

      svc, err := v3net.New(cfg)
      if err != nil {
          t.Fatalf("v3net.New: %v", err)
      }
      defer svc.Close()

      // The hub's own node must be an active subscriber.
      sub := svc.Hub().Subscribers().Get(ks.NodeID(), "testnet")
      if sub == nil {
          t.Fatal("expected hub node to be registered as subscriber, got nil")
      }
      if sub.Status != "active" {
          t.Errorf("expected status=active, got %q", sub.Status)
      }
  }

  func TestHubAutoInit_NALSeeded(t *testing.T) {
      dir := t.TempDir()
      keystorePath := filepath.Join(dir, "v3net.key")

      if _, err := keystore.Load(keystorePath); err != nil {
          t.Fatal(err)
      }

      cfg := config.V3NetConfig{
          Enabled:      true,
          KeystorePath: keystorePath,
          DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
          ConfigPath:   dir,
          Hub: config.V3NetHubConfig{
              Enabled: true,
              DataDir: dir,
              Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
              InitialAreas: []config.V3NetHubArea{
                  {Tag: "test.general", Name: "General"},
              },
          },
      }

      svc, err := v3net.New(cfg)
      if err != nil {
          t.Fatalf("v3net.New: %v", err)
      }
      defer svc.Close()

      n, err := svc.Hub().NALStore().Get("testnet")
      if err != nil {
          t.Fatalf("get NAL: %v", err)
      }
      if n == nil {
          t.Fatal("expected NAL to be seeded, got nil")
      }
      if len(n.Areas) != 1 || n.Areas[0].Tag != "test.general" {
          t.Errorf("unexpected areas: %+v", n.Areas)
      }
  }

  func TestHubAutoInit_NALSeedIdempotent(t *testing.T) {
      dir := t.TempDir()
      keystorePath := filepath.Join(dir, "v3net.key")

      if _, err := keystore.Load(keystorePath); err != nil {
          t.Fatal(err)
      }

      cfg := config.V3NetConfig{
          Enabled:      true,
          KeystorePath: keystorePath,
          DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
          ConfigPath:   dir,
          Hub: config.V3NetHubConfig{
              Enabled: true,
              DataDir: dir,
              Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
              InitialAreas: []config.V3NetHubArea{
                  {Tag: "test.general", Name: "General"},
              },
          },
      }

      // First init — seeds NAL.
      svc1, err := v3net.New(cfg)
      if err != nil {
          t.Fatal(err)
      }
      svc1.Close()

      // Second init with same initialAreas — must not error and must not overwrite NAL.
      svc2, err := v3net.New(cfg)
      if err != nil {
          t.Fatalf("second v3net.New: %v", err)
      }
      defer svc2.Close()

      n, _ := svc2.Hub().NALStore().Get("testnet")
      if n == nil || len(n.Areas) != 1 {
          t.Error("expected original seeded NAL to remain after second init")
      }
  }
  ```

- [ ] **Step 2: Run tests to confirm they fail**

  ```bash
  go test ./internal/v3net/... -run TestHubAutoInit -v 2>&1 | head -30
  ```

  Expected: compile error — `svc.Hub()` not yet defined, tests can't compile.

- [ ] **Step 3: Add `Hub()` accessor and `hubAutoInit()` to service.go**

  In `internal/v3net/service.go`, add the following imports if not already present:
  `"os"`, `"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"`, `"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"`, `"github.com/ViSiON-3/vision-3-bbs/internal/config"`.

  Add a `Hub()` accessor (used by tests) after `LeafNetworks()`:

  ```go
  // Hub returns the hub instance, or nil if no hub is configured.
  func (s *Service) Hub() *hub.Hub {
      return s.hub
  }
  ```

  Add `hubAutoInit()` before `New()`:

  ```go
  // hubAutoInit performs idempotent hub initialization steps:
  // 1. Creates the hub data directory.
  // 2. Self-registers the hub node as an active subscriber for each network.
  // 3. Seeds the initial NAL from cfg.Hub.InitialAreas if none exists.
  func hubAutoInit(cfg config.V3NetConfig, h *hub.Hub, ks *keystore.Keystore) {
      // Step 1: ensure data dir exists (must happen before hub.New, but we call
      // this after hub.New so it's a belt-and-suspenders guard here too).
      if err := os.MkdirAll(cfg.Hub.DataDir, 0755); err != nil {
          slog.Warn("v3net: could not create hub data dir", "path", cfg.Hub.DataDir, "error", err)
      }

      // Step 2: self-register the hub node for each network.
      for _, n := range cfg.Hub.Networks {
          sub := hub.Subscriber{
              NodeID:    ks.NodeID(),
              Network:   n.Name,
              PubKeyB64: ks.PubKeyBase64(),
              BBSName:   "hub",
              BBSHost:   "",
              Status:    "active",
          }
          if _, err := h.Subscribers().Add(sub); err != nil {
              slog.Warn("v3net: hub self-registration failed", "network", n.Name, "error", err)
          } else {
              slog.Info("v3net: hub self-registered", "node_id", ks.NodeID(), "network", n.Name)
          }
      }

      // Step 3: seed NAL from InitialAreas if no NAL exists yet.
      if len(cfg.Hub.InitialAreas) == 0 {
          return
      }
      for _, n := range cfg.Hub.Networks {
          existing, err := h.NALStore().Get(n.Name)
          if err != nil {
              slog.Warn("v3net: could not check NAL for seeding", "network", n.Name, "error", err)
              continue
          }
          if existing != nil {
              continue // NAL already exists — skip seeding.
          }

          var areas []protocol.Area
          for _, a := range cfg.Hub.InitialAreas {
              areas = append(areas, protocol.Area{
                  Tag:              a.Tag,
                  Name:             a.Name,
                  Language:         "en",
                  ManagerNodeID:    ks.NodeID(),
                  ManagerPubKeyB64: ks.PubKeyBase64(),
                  Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
                  Policy: protocol.AreaPolicy{
                      MaxBodyBytes: 64000,
                      AllowANSI:    true,
                  },
              })
          }

          nalDoc := &protocol.NAL{
              V3NetNAL: "1.0",
              Network:  n.Name,
              Areas:    areas,
          }
          if err := nal.Sign(nalDoc, ks); err != nil {
              slog.Error("v3net: could not sign initial NAL", "network", n.Name, "error", err)
              continue
          }
          if err := h.NALStore().Put(n.Name, nalDoc); err != nil {
              slog.Error("v3net: could not store initial NAL", "network", n.Name, "error", err)
              continue
          }
          slog.Info("v3net: seeded initial NAL", "network", n.Name, "areas", len(areas))
      }

      // Clear initialAreas from the saved config file so we don't re-seed.
      updatedCfg := cfg
      updatedCfg.Hub.InitialAreas = nil
      if cfg.ConfigPath != "" {
          if err := config.SaveV3NetConfig(cfg.ConfigPath, updatedCfg); err != nil {
              slog.Warn("v3net: could not remove initialAreas from config after seeding", "error", err)
          }
      }
  }
  ```

  In `New()`, update the hub initialization block. Change:

  ```go
  if cfg.Hub.Enabled {
      // ... existing os.MkdirAll not yet present ...
      h, err := hub.New(hub.Config{ ... })
      if err != nil { ... }
      s.hub = h
  }
  ```

  To:

  ```go
  if cfg.Hub.Enabled {
      // Create data dir before hub.New opens the SQLite database.
      if err := os.MkdirAll(cfg.Hub.DataDir, 0755); err != nil {
          ix.Close()
          return nil, fmt.Errorf("v3net: create hub data dir: %w", err)
      }

      var networks []hub.NetworkConfig
      for _, n := range cfg.Hub.Networks {
          networks = append(networks, hub.NetworkConfig{
              Name:        n.Name,
              Description: n.Description,
          })
      }
      h, err := hub.New(hub.Config{
          ListenAddr:  cfg.Hub.ListenAddr(),
          TLSCertFile: cfg.Hub.TLSCert,
          TLSKeyFile:  cfg.Hub.TLSKey,
          DataDir:     cfg.Hub.DataDir,
          Keystore:    ks,
          AutoApprove: cfg.Hub.AutoApprove,
          Networks:    networks,
      })
      if err != nil {
          ix.Close()
          return nil, fmt.Errorf("v3net: create hub: %w", err)
      }
      hubAutoInit(cfg, h, ks)
      s.hub = h
  }
  ```

- [ ] **Step 4: Run tests**

  ```bash
  go test ./internal/v3net/... -run TestHubAutoInit -v
  ```

  Expected: all four `TestHubAutoInit_*` tests pass.

- [ ] **Step 5: Run the full test suite**

  ```bash
  go test ./...
  ```

  Expected: all tests pass.

- [ ] **Step 6: Commit**

  ```bash
  git add internal/v3net/service.go internal/v3net/service_test.go
  git commit -m "feat(v3net): add hub startup auto-init (data dir, self-register, NAL seed)"
  ```

---

## Task 4: Config Editor Model Updates

**Files:**
- Modify: `internal/configeditor/model.go`

This adds the wizard infrastructure to the model: two new modes, the `wizardState`/`wizardArea` structs, the new top-level menu item, and the updated `selectTopMenuItem` routing.

- [ ] **Step 1: Add new `editorMode` constants**

  In `internal/configeditor/model.go`, in the `const` block after `modeRecordReorder` (line ~34), add:

  ```go
  modeV3NetSetupFork  // V3Net setup fork screen (leaf or hub)
  modeV3NetWizardStep // Active wizard step
  ```

- [ ] **Step 2: Add `wizardArea` and `wizardState` structs**

  After the `sysConfigMenuItem` struct definition, add:

  ```go
  // wizardArea is a single area entry in the hub setup wizard.
  type wizardArea struct {
      Tag  string
      Name string
  }

  // wizardState holds all transient state for the V3Net setup wizard.
  type wizardState struct {
      flow string // "leaf" or "hub"
      step int    // current step index (0-based)

      // Leaf wizard fields (steps 0–4)
      hubURL       string
      networkName  string
      boardTag     string
      pollInterval string
      origin       string
      fetchError   string // set if auto-fetch failed

      // Hub wizard fields (steps 0–3)
      netName      string
      netDesc      string
      port         string
      autoApprove  bool
      areas        []wizardArea
      areaEditTag  string
      areaEditName string
      areaAdding   bool // true when the area tag/name sub-form is open
      areaCursor   int  // highlighted area in the area list
  }
  ```

- [ ] **Step 3: Add `wizard wizardState` field to the `Model` struct**

  In the `Model` struct, after `confirmYes bool`, add:

  ```go
  // V3Net setup wizard state
  wizard wizardState
  ```

- [ ] **Step 4: Add menu item E and update `selectTopMenuItem`**

  In `New()`, update `topItems` to insert `{"E", "V3Net Setup"}` between `D` and `Q`:

  ```go
  topItems := []topMenuItem{
      {"1", "System Configuration"},
      {"2", "Message Areas"},
      {"3", "File Areas"},
      {"4", "Conferences"},
      {"5", "Door Programs"},
      {"6", "Event Scheduler"},
      {"7", "Echomail Networks"},
      {"8", "Echomail Links"},
      {"9", "Transfer Protocols"},
      {"A", "Archivers"},
      {"B", "Login Sequence"},
      {"C", "V3Net Subscriptions"},
      {"D", "V3Net Networks"},
      {"E", "V3Net Setup"},
      {"Q", "Quit Program"},
  }
  ```

  Update `selectTopMenuItem()`. After inserting E, the cursor positions are:
  0=System, 1–10=record editors, 11=C(V3NetSubs), 12=D(V3NetNetworks),
  13=E(V3NetSetup, NEW), 14=Q(Quit). The `recordTypes` slice is **unchanged**
  (covers indices 0–12); case 13 handles E explicitly before the default runs:

  ```go
  func (m Model) selectTopMenuItem() (Model, tea.Cmd) {
      // Covers cursors 0–12 (System through V3Net Networks). Unchanged from original.
      recordTypes := []string{
          "", "msgarea", "filearea", "conference", "door",
          "event", "ftn", "ftnlink", "protocol", "archiver", "login",
          "v3netleaf", "v3nethub",
      }

      switch m.topCursor {
      case 0: // System Configuration
          m.mode = modeSysConfigMenu
          m.sysMenuCursor = 0
          return m, nil
      case 13: // V3Net Setup (E) — NEW
          m.wizard = wizardState{}
          m.mode = modeV3NetSetupFork
          return m, nil
      case 14: // Quit (was 13, shifted by E insertion)
          return m.tryExit()
      default:
          // Cursors 1–12 map to record list editors.
          if m.topCursor > 0 && m.topCursor < len(recordTypes) {
              m.recordType = recordTypes[m.topCursor]
              m.recordCursor = 0
              m.recordScroll = 0
              m.mode = modeRecordList
          }
          return m, nil
      }
  }
  ```

- [ ] **Step 5: Add wizard modes to `Update()` dispatch**

  In `Model.Update()`, add cases for the new modes after `modeHelp`:

  ```go
  case modeV3NetSetupFork:
      return m.updateV3NetSetupFork(msg)
  case modeV3NetWizardStep:
      return m.updateV3NetWizardStep(msg)
  ```

- [ ] **Step 6: Verify compilation**

  ```bash
  go build ./internal/configeditor/...
  ```

  Expected: compile errors for `updateV3NetSetupFork` and `updateV3NetWizardStep` not defined — that is expected at this stage.

- [ ] **Step 7: Note — hold model.go until Task 5 compiles**

  Do not commit yet. `model.go` references `updateV3NetSetupFork` and
  `updateV3NetWizardStep` which are defined in Task 5. Commit `model.go`
  together with the Task 5 files in Task 5 Step 8.

---

## Task 5: Wizard Update Logic

**Files:**
- Create: `internal/configeditor/update_v3net_wizard.go`
- Modify: `internal/configeditor/view.go`
- Create: `internal/configeditor/wizard_test.go`

- [ ] **Step 1: Write failing tests**

  Create `internal/configeditor/wizard_test.go`:

  ```go
  package configeditor

  import (
      "testing"

      tea "github.com/charmbracelet/bubbletea"
  )

  // newWizardModel returns a minimal Model in modeV3NetSetupFork for testing.
  func newWizardModel() Model {
      m := Model{
          mode:   modeV3NetSetupFork,
          width:  80,
          height: 25,
          topItems: []topMenuItem{
              {"Q", "Quit"},
          },
      }
      return m
  }

  func TestWizardFork_LeafSelected(t *testing.T) {
      m := newWizardModel()
      result, _ := m.updateV3NetSetupFork(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
      got := result.(Model)
      if got.mode != modeV3NetWizardStep {
          t.Errorf("expected modeV3NetWizardStep, got %v", got.mode)
      }
      if got.wizard.flow != "leaf" {
          t.Errorf("expected flow=leaf, got %q", got.wizard.flow)
      }
      if got.wizard.step != 0 {
          t.Errorf("expected step=0, got %d", got.wizard.step)
      }
  }

  func TestWizardFork_HubSelected(t *testing.T) {
      m := newWizardModel()
      result, _ := m.updateV3NetSetupFork(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
      got := result.(Model)
      if got.mode != modeV3NetWizardStep {
          t.Errorf("expected modeV3NetWizardStep, got %v", got.mode)
      }
      if got.wizard.flow != "hub" {
          t.Errorf("expected flow=hub, got %q", got.wizard.flow)
      }
  }

  func TestWizardFork_EscBack(t *testing.T) {
      m := newWizardModel()
      result, _ := m.updateV3NetSetupFork(tea.KeyMsg{Type: tea.KeyEscape})
      got := result.(Model)
      if got.mode != modeTopMenu {
          t.Errorf("expected modeTopMenu, got %v", got.mode)
      }
  }

  func TestLeafWizard_HubURLValidation(t *testing.T) {
      m := newWizardModel()
      m.mode = modeV3NetWizardStep
      m.wizard = wizardState{flow: "leaf", step: 0, hubURL: "notaurl"}
      // Trying to advance with an invalid URL should stay on step 0.
      result, _ := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyEnter})
      got := result.(Model)
      if got.wizard.step != 0 {
          t.Errorf("expected to stay on step 0 with invalid URL, got step %d", got.wizard.step)
      }
      if got.message == "" {
          t.Error("expected a validation error message")
      }
  }

  func TestLeafWizard_ValidURLAdvances(t *testing.T) {
      m := newWizardModel()
      m.mode = modeV3NetWizardStep
      m.wizard = wizardState{flow: "leaf", step: 0, hubURL: "https://hub.example.com"}
      result, cmd := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyEnter})
      got := result.(Model)
      if got.wizard.step != 1 {
          t.Errorf("expected step=1, got %d", got.wizard.step)
      }
      if cmd == nil {
          t.Error("expected a tea.Cmd for auto-fetch")
      }
  }

  func TestHubWizard_PortValidation(t *testing.T) {
      m := newWizardModel()
      m.mode = modeV3NetWizardStep
      m.wizard = wizardState{flow: "hub", step: 1, port: "99999"}
      result, _ := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyEnter})
      got := result.(Model)
      if got.wizard.step != 1 {
          t.Errorf("expected to stay on step 1 with invalid port, got step %d", got.wizard.step)
      }
  }

  func TestHubWizard_AutoApproveToggle(t *testing.T) {
      m := newWizardModel()
      m.mode = modeV3NetWizardStep
      m.wizard = wizardState{flow: "hub", step: 2, autoApprove: false}
      result, _ := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
      got := result.(Model)
      if !got.wizard.autoApprove {
          t.Error("expected autoApprove to be toggled to true")
      }
  }
  ```

- [ ] **Step 2: Run tests to confirm they fail (expected)**

  ```bash
  go test ./internal/configeditor/... -run TestWizard -v 2>&1 | head -20
  ```

  Expected: compile error — `updateV3NetSetupFork` and `updateV3NetWizardStep` undefined.

- [ ] **Step 3: Create `update_v3net_wizard.go`**

  Create `internal/configeditor/update_v3net_wizard.go`:

  ```go
  package configeditor

  import (
      "encoding/json"
      "fmt"
      "net/http"
      "strconv"
      "strings"
      "time"

      tea "github.com/charmbracelet/bubbletea"

      "github.com/ViSiON-3/vision-3-bbs/internal/config"
      "github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
  )

  // fetchNetworksMsg is the result of an auto-fetch of hub networks.
  type fetchNetworksMsg struct {
      names []string
      err   error
  }

  // fetchHubNetworks returns a tea.Cmd that GETs /v3net/v1/networks from the hub.
  func fetchHubNetworks(hubURL string) tea.Cmd {
      return func() tea.Msg {
          client := &http.Client{Timeout: 5 * time.Second}
          resp, err := client.Get(strings.TrimRight(hubURL, "/") + "/v3net/v1/networks")
          if err != nil {
              return fetchNetworksMsg{err: err}
          }
          defer resp.Body.Close()
          if resp.StatusCode != http.StatusOK {
              return fetchNetworksMsg{err: fmt.Errorf("status %d", resp.StatusCode)}
          }
          var summaries []struct {
              Name string `json:"name"`
          }
          if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
              return fetchNetworksMsg{err: err}
          }
          var names []string
          for _, s := range summaries {
              names = append(names, s.Name)
          }
          return fetchNetworksMsg{names: names}
      }
  }

  // --- Fork screen ---

  func (m Model) updateV3NetSetupFork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      switch msg.Type {
      case tea.KeyEscape:
          m.mode = modeTopMenu
          return m, nil
      case tea.KeyRunes:
          switch strings.ToLower(string(msg.Runes)) {
          case "j":
              m.wizard = wizardState{
                  flow:         "leaf",
                  step:         0,
                  pollInterval: "5m",
                  origin:       m.configs.Server.BoardName,
              }
              m.mode = modeV3NetWizardStep
              m.textInput.Reset()
              m.textInput.Focus()
          case "h":
              m.wizard = wizardState{
                  flow: "hub",
                  step: 0,
                  port: "8765",
              }
              m.mode = modeV3NetWizardStep
              m.textInput.Reset()
              m.textInput.Focus()
          }
      }
      return m, nil
  }

  // --- Wizard step dispatcher ---

  func (m Model) updateV3NetWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if m.wizard.flow == "leaf" {
          return m.updateLeafWizardStep(msg)
      }
      return m.updateHubWizardStep(msg)
  }

  // updateWizardTextInput handles generic text input for wizard steps that
  // use the shared textInput field. Returns the updated model.
  func (m Model) updateWizardTextInput(msg tea.KeyMsg) Model {
      var cmd tea.Cmd
      m.textInput, cmd = m.textInput.Update(msg)
      _ = cmd
      return m
  }

  // --- Leaf wizard ---

  const (
      leafStepHubURL      = 0
      leafStepNetwork     = 1
      leafStepBoard       = 2
      leafStepPollInterval = 3
      leafStepOrigin      = 4
  )

  func (m Model) updateLeafWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      // ESC anywhere in the leaf wizard returns to fork.
      if msg.Type == tea.KeyEscape {
          m.mode = modeV3NetSetupFork
          return m, nil
      }

      switch m.wizard.step {
      case leafStepHubURL:
          return m.updateLeafStepHubURL(msg)
      case leafStepNetwork:
          return m.updateLeafStepNetwork(msg)
      case leafStepBoard:
          return m.updateLeafStepBoard(msg)
      case leafStepPollInterval:
          return m.updateLeafStepPollInterval(msg)
      case leafStepOrigin:
          return m.updateLeafStepOrigin(msg)
      }
      return m, nil
  }

  func (m Model) updateLeafStepHubURL(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEnter {
          val := strings.TrimSpace(m.textInput.Value())
          if val == "" || (!strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://")) {
              m.message = "Hub URL must start with http:// or https://"
              return m, nil
          }
          m.wizard.hubURL = val
          m.wizard.step = leafStepNetwork
          m.wizard.fetchError = ""
          m.textInput.Reset()
          // Auto-fetch network list.
          return m, fetchHubNetworks(val)
      }
      m = m.updateWizardTextInput(msg)
      m.wizard.hubURL = m.textInput.Value()
      return m, nil
  }

  // NOTE: Do NOT define an Update method here — it already exists in model.go.
  // handleFetchNetworksMsg is called from model.go's Update() via the
  // fetchNetworksMsg case added in Step 4.

  func (m Model) handleFetchNetworksMsg(msg fetchNetworksMsg) (tea.Model, tea.Cmd) {
      if msg.err != nil || len(msg.names) == 0 {
          m.wizard.fetchError = "(could not reach hub — enter network name manually)"
          m.textInput.Reset()
          m.textInput.Focus()
          return m, nil
      }
      if len(msg.names) == 1 {
          m.wizard.networkName = msg.names[0]
          m.textInput.SetValue(msg.names[0])
          m.textInput.Focus()
          return m, nil
      }
      // Multiple networks — show picker.
      var items []LookupItem
      for _, name := range msg.names {
          items = append(items, LookupItem{Value: name, Display: name})
      }
      m.pickerItems = items
      m.pickerCursor = 0
      m.pickerScroll = 0
      m.pickerReturnMode = modeV3NetWizardStep
      m.mode = modeLookupPicker
      return m, nil
  }

  func (m Model) updateLeafStepNetwork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEnter {
          val := strings.TrimSpace(m.textInput.Value())
          if val == "" {
              m.message = "Network name cannot be empty"
              return m, nil
          }
          m.wizard.networkName = val
          m.wizard.step = leafStepBoard
          m.textInput.Reset()
          return m, nil
      }
      m = m.updateWizardTextInput(msg)
      m.wizard.networkName = m.textInput.Value()
      return m, nil
  }

  func (m Model) updateLeafStepBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEnter {
          val := strings.TrimSpace(m.textInput.Value())
          if val == "" {
              m.message = "Board tag cannot be empty"
              return m, nil
          }
          m.wizard.boardTag = val
          m.wizard.step = leafStepPollInterval
          m.textInput.SetValue(m.wizard.pollInterval)
          m.textInput.Focus()
          return m, nil
      }
      m = m.updateWizardTextInput(msg)
      m.wizard.boardTag = m.textInput.Value()
      return m, nil
  }

  func (m Model) updateLeafStepPollInterval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEnter {
          val := strings.TrimSpace(m.textInput.Value())
          d, err := time.ParseDuration(val)
          if err != nil || d <= 0 {
              m.message = "Poll interval must be a valid duration (e.g. 5m, 30s)"
              return m, nil
          }
          m.wizard.pollInterval = val
          m.wizard.step = leafStepOrigin
          m.textInput.SetValue(m.wizard.origin)
          m.textInput.Focus()
          return m, nil
      }
      m = m.updateWizardTextInput(msg)
      m.wizard.pollInterval = m.textInput.Value()
      return m, nil
  }

  func (m Model) updateLeafStepOrigin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEnter {
          m.wizard.origin = strings.TrimSpace(m.textInput.Value())
          return m.confirmLeafWizard()
      }
      m = m.updateWizardTextInput(msg)
      m.wizard.origin = m.textInput.Value()
      return m, nil
  }

  func (m Model) confirmLeafWizard() (Model, tea.Cmd) {
      // Duplicate check.
      for _, l := range m.configs.V3Net.Leaves {
          if l.HubURL == m.wizard.hubURL && l.Network == m.wizard.networkName {
              m.message = "Already subscribed to this network on this hub"
              m.mode = modeV3NetSetupFork
              return m, nil
          }
      }

      leaf := config.V3NetLeafConfig{
          HubURL:       m.wizard.hubURL,
          Network:      m.wizard.networkName,
          Board:        m.wizard.boardTag,
          PollInterval: m.wizard.pollInterval,
          Origin:       m.wizard.origin,
      }
      m.configs.V3Net.Leaves = append(m.configs.V3Net.Leaves, leaf)
      m.configs.V3Net.Enabled = true
      m.dirty = true
      m.saveAll()
      m.message = "Saved — restart the BBS to activate."
      m.mode = modeTopMenu
      return m, nil
  }

  // --- Hub wizard ---

  const (
      hubStepNetwork     = 0
      hubStepPort        = 1
      hubStepAutoApprove = 2
      hubStepAreas       = 3
  )

  func (m Model) updateHubWizardStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEscape && !m.wizard.areaAdding {
          m.mode = modeV3NetSetupFork
          return m, nil
      }

      switch m.wizard.step {
      case hubStepNetwork:
          return m.updateHubStepNetwork(msg)
      case hubStepPort:
          return m.updateHubStepPort(msg)
      case hubStepAutoApprove:
          return m.updateHubStepAutoApprove(msg)
      case hubStepAreas:
          return m.updateHubStepAreas(msg)
      }
      return m, nil
  }

  func (m Model) updateHubStepNetwork(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      // This step has two sub-fields: name (textInput) then description.
      // We use a simple convention: first Enter commits the name and focuses desc.
      // We track which sub-field with wizard.areaAdding (repurposed: false=name, true=desc).
      if msg.Type == tea.KeyEnter {
          if !m.wizard.areaAdding {
              // Committing name.
              val := strings.TrimSpace(m.textInput.Value())
              if val == "" {
                  m.message = "Network name cannot be empty"
                  return m, nil
              }
              // Validate: lowercase alphanumeric only.
              for _, c := range val {
                  if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
                      m.message = "Network name must be lowercase alphanumeric only"
                      return m, nil
                  }
              }
              m.wizard.netName = val
              m.wizard.areaAdding = true // now editing description
              m.textInput.SetValue(m.wizard.netDesc)
              m.textInput.Focus()
          } else {
              // Committing description.
              m.wizard.netDesc = strings.TrimSpace(m.textInput.Value())
              m.wizard.areaAdding = false
              m.wizard.step = hubStepPort
              m.textInput.SetValue(m.wizard.port)
              m.textInput.Focus()
          }
          return m, nil
      }
      m = m.updateWizardTextInput(msg)
      if !m.wizard.areaAdding {
          m.wizard.netName = m.textInput.Value()
      } else {
          m.wizard.netDesc = m.textInput.Value()
      }
      return m, nil
  }

  func (m Model) updateHubStepPort(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEnter {
          val := strings.TrimSpace(m.textInput.Value())
          p, err := strconv.Atoi(val)
          if err != nil || p < 1 || p > 65535 {
              m.message = "Port must be a number between 1 and 65535"
              return m, nil
          }
          m.wizard.port = val
          m.wizard.step = hubStepAutoApprove
          m.textInput.Reset()
          return m, nil
      }
      m = m.updateWizardTextInput(msg)
      m.wizard.port = m.textInput.Value()
      return m, nil
  }

  func (m Model) updateHubStepAutoApprove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      switch msg.Type {
      case tea.KeyEnter:
          m.wizard.step = hubStepAreas
          m.textInput.Reset()
          return m, nil
      case tea.KeyRunes:
          switch strings.ToLower(string(msg.Runes)) {
          case "y":
              m.wizard.autoApprove = true
          case "n":
              m.wizard.autoApprove = false
          }
      }
      return m, nil
  }

  func (m Model) updateHubStepAreas(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      // Sub-form open: collect tag then name.
      if m.wizard.areaAdding {
          return m.updateHubAreaSubForm(msg)
      }

      switch msg.Type {
      case tea.KeyEnter:
          if len(m.wizard.areas) == 0 {
              m.message = "At least one area is required"
              return m, nil
          }
          return m.confirmHubWizard()
      case tea.KeyUp:
          if m.wizard.areaCursor > 0 {
              m.wizard.areaCursor--
          }
      case tea.KeyDown:
          if m.wizard.areaCursor < len(m.wizard.areas)-1 {
              m.wizard.areaCursor++
          }
      case tea.KeyRunes:
          switch strings.ToUpper(string(msg.Runes)) {
          case "A":
              m.wizard.areaAdding = true
              m.wizard.areaEditTag = ""
              m.wizard.areaEditName = ""
              m.textInput.Reset()
              m.textInput.Focus()
          case "D":
              if len(m.wizard.areas) > 0 {
                  i := m.wizard.areaCursor
                  m.wizard.areas = append(m.wizard.areas[:i], m.wizard.areas[i+1:]...)
                  if m.wizard.areaCursor >= len(m.wizard.areas) && m.wizard.areaCursor > 0 {
                      m.wizard.areaCursor--
                  }
              }
          }
      }
      return m, nil
  }

  // areaSubFormPhase tracks which field is being entered in the area sub-form.
  // We reuse areaEditTag="" sentinel: if tag is empty we're on the tag field,
  // otherwise we're on the name field. This is tracked by whether areaEditTag
  // has been committed (non-empty) or not.
  // We use a separate bool by convention: entering tag phase first.
  // To avoid adding another field, we use areaEditName=="" as proxy for "on tag phase".

  func (m Model) updateHubAreaSubForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.Type == tea.KeyEscape {
          m.wizard.areaAdding = false
          m.textInput.Reset()
          return m, nil
      }
      if msg.Type == tea.KeyEnter {
          val := strings.TrimSpace(m.textInput.Value())
          if m.wizard.areaEditTag == "" {
              // Committing tag.
              if err := protocol.ValidateAreaTag(val); err != nil {
                  m.message = err.Error()
                  return m, nil
              }
              m.wizard.areaEditTag = val
              m.textInput.Reset()
              m.textInput.Focus()
              return m, nil
          }
          // Committing name.
          if val == "" {
              m.message = "Area name cannot be empty"
              return m, nil
          }
          m.wizard.areas = append(m.wizard.areas, wizardArea{
              Tag:  m.wizard.areaEditTag,
              Name: val,
          })
          m.wizard.areaCursor = len(m.wizard.areas) - 1
          m.wizard.areaAdding = false
          m.wizard.areaEditTag = ""
          m.wizard.areaEditName = ""
          m.textInput.Reset()
          return m, nil
      }
      m = m.updateWizardTextInput(msg)
      if m.wizard.areaEditTag == "" {
          // Still on the tag phase — live-update is intentionally deferred until Enter.
          // (areaEditTag stays empty until the user confirms the tag value.)
      } else {
          m.wizard.areaEditName = m.textInput.Value()
      }
      return m, nil
  }

  func (m Model) confirmHubWizard() (Model, tea.Cmd) {
      port, _ := strconv.Atoi(m.wizard.port)

      var initialAreas []config.V3NetHubArea
      for _, a := range m.wizard.areas {
          initialAreas = append(initialAreas, config.V3NetHubArea{Tag: a.Tag, Name: a.Name})
      }

      m.configs.V3Net.Enabled = true
      if m.configs.V3Net.KeystorePath == "" {
          m.configs.V3Net.KeystorePath = "data/v3net.key"
      }
      if m.configs.V3Net.DedupDBPath == "" {
          m.configs.V3Net.DedupDBPath = "data/v3net_dedup.sqlite"
      }
      m.configs.V3Net.Hub = config.V3NetHubConfig{
          Enabled:     true,
          Port:        port,
          DataDir:     "data/v3net_hub",
          AutoApprove: m.wizard.autoApprove,
          Networks: []config.V3NetHubNetwork{
              {Name: m.wizard.netName, Description: m.wizard.netDesc},
          },
          InitialAreas: initialAreas,
      }
      m.dirty = true
      m.saveAll()
      m.message = "Saved — start the BBS to initialize your hub and seed the NAL."
      m.mode = modeTopMenu
      return m, nil
  }
  ```

- [ ] **Step 4: Wire `fetchNetworksMsg` into `model.go`'s `Update()`**

  In `internal/configeditor/model.go`, in the `Update()` method's outer `switch msg := msg.(type)` block, before the `case tea.KeyMsg:` branch, add:

  ```go
  case fetchNetworksMsg:
      return m.handleFetchNetworksMsg(msg)
  ```

  Also remove the stub `Update` method accidentally defined in `update_v3net_wizard.go` above (the one that just returns `m, nil`) — it was illustrative only and will cause a compile error if left in.

- [ ] **Step 5: Add wizard cases to `view.go`'s `View()` switch**

  In `internal/configeditor/view.go`, in the `View()` method switch, add:

  ```go
  case modeV3NetSetupFork, modeV3NetWizardStep:
      return m.viewV3NetWizard()
  ```

- [ ] **Step 6: Run wizard tests**

  ```bash
  go test ./internal/configeditor/... -run TestWizard -v
  ```

  Expected: all `TestWizard*` tests pass.

- [ ] **Step 7: Run full test suite**

  ```bash
  go test ./...
  ```

  Expected: all tests pass.

- [ ] **Step 8: Commit (includes model.go from Task 4)**

  ```bash
  git add internal/configeditor/model.go \
          internal/configeditor/update_v3net_wizard.go \
          internal/configeditor/view.go \
          internal/configeditor/wizard_test.go
  git commit -m "feat(config): add V3Net setup wizard update logic and model state"
  ```

---

## Task 6: Wizard View Rendering

**Files:**
- Create: `internal/configeditor/view_v3net_wizard.go`

- [ ] **Step 1: Create the view file**

  Create `internal/configeditor/view_v3net_wizard.go`:

  ```go
  package configeditor

  import (
      "fmt"
      "strings"
  )

  // viewV3NetWizard renders the V3Net setup fork screen or active wizard step.
  func (m Model) viewV3NetWizard() string {
      if m.mode == modeV3NetSetupFork {
          return m.viewV3NetSetupFork()
      }
      if m.wizard.flow == "leaf" {
          return m.viewLeafWizardStep()
      }
      return m.viewHubWizardStep()
  }

  func (m Model) viewV3NetSetupFork() string {
      var b strings.Builder
      b.WriteString(m.globalHeaderLine())
      b.WriteByte('\n')

      bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
      boxW := 52
      boxH := 8
      extraV := maxInt(0, m.height-boxH-3)
      topPad := extraV / 2
      bottomPad := extraV - topPad

      for i := 0; i < topPad; i++ {
          b.WriteString(bgLine)
          b.WriteByte('\n')
      }

      padL := maxInt(0, (m.width-boxW-2)/2)
      padR := maxInt(0, m.width-padL-boxW-2)
      pad := func(s string) string {
          return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
              bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
      }

      b.WriteString(pad(menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))
      b.WriteByte('\n')
      b.WriteString(pad(menuBorderStyle.Render("│") +
          menuHeaderStyle.Render(centerText("V3Net Setup", boxW)) +
          menuBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(pad(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
      b.WriteByte('\n')

      items := []string{
          "  [J]  Join an existing network  (leaf node)",
          "  [H]  Host your own network     (hub operator)",
      }
      for _, item := range items {
          b.WriteString(pad(menuBorderStyle.Render("│") +
              menuItemStyle.Render(padRight(item, boxW)) +
              menuBorderStyle.Render("│")))
          b.WriteByte('\n')
      }

      b.WriteString(pad(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(pad(menuBorderStyle.Render("│") +
          helpStyle.Render(padRight("  ESC — Back", boxW)) +
          menuBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(pad(menuBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘")))
      b.WriteByte('\n')

      for i := 0; i < bottomPad; i++ {
          b.WriteString(bgLine)
          b.WriteByte('\n')
      }

      b.WriteString(m.renderMessage())
      b.WriteByte('\n')
      b.WriteString(m.renderHelpBar("J/H Select  ESC Back"))
      return b.String()
  }

  func (m Model) viewLeafWizardStep() string {
      titles := []string{
          "Step 1 of 5 — Hub URL",
          "Step 2 of 5 — Network Name",
          "Step 3 of 5 — Board Tag",
          "Step 4 of 5 — Poll Interval",
          "Step 5 of 5 — Origin Line",
      }
      helps := []string{
          "URL of the V3Net hub (e.g. https://hub.felonynet.org)",
          "Network name to subscribe to (e.g. felonynet)",
          "Local message area tag prefix for received messages",
          "How often to poll for new messages (e.g. 5m, 30s, 1h)",
          "Origin line identifying your BBS — leave blank to use BBS name",
      }
      title := "Leaf Setup"
      if m.wizard.step < len(titles) {
          title = titles[m.wizard.step]
      }
      help := ""
      if m.wizard.step < len(helps) {
          help = helps[m.wizard.step]
      }

      notice := ""
      if m.wizard.step == leafStepNetwork && m.wizard.fetchError != "" {
          notice = m.wizard.fetchError
      }

      return m.viewWizardInputBox("Join a Network — "+title, help, notice)
  }

  func (m Model) viewHubWizardStep() string {
      switch m.wizard.step {
      case hubStepNetwork:
          subField := "Network Name"
          if m.wizard.areaAdding { // repurposed: editing description
              subField = "Description"
          }
          return m.viewWizardInputBox(
              "Host a Network — Step 1 of 4 — "+subField,
              map[bool]string{
                  false: "Short lowercase alphanumeric identifier (e.g. felonynet)",
                  true:  "Human-readable description shown to subscribers",
              }[m.wizard.areaAdding],
              "",
          )
      case hubStepPort:
          return m.viewWizardInputBox(
              "Host a Network — Step 2 of 4 — Listen Port",
              "TCP port for the hub server (default: 8765)",
              "",
          )
      case hubStepAutoApprove:
          notice := "Yes = nodes join instantly (testing only)  /  No = sysop approves each node"
          current := "N (No)"
          if m.wizard.autoApprove {
              current = "Y (Yes)"
          }
          return m.viewWizardInputBox(
              "Host a Network — Step 3 of 4 — Auto-Approve",
              fmt.Sprintf("Auto-approve new nodes? Currently: %s   Press Y or N, then Enter", current),
              notice,
          )
      case hubStepAreas:
          return m.viewHubAreasStep()
      }
      return ""
  }

  func (m Model) viewHubAreasStep() string {
      var b strings.Builder
      b.WriteString(m.globalHeaderLine())
      b.WriteByte('\n')

      bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
      boxW := 60
      listH := maxInt(3, len(m.wizard.areas)+1)
      boxH := listH + 8
      extraV := maxInt(0, m.height-boxH-3)
      topPad := extraV / 2
      bottomPad := extraV - topPad

      for i := 0; i < topPad; i++ {
          b.WriteString(bgLine)
          b.WriteByte('\n')
      }

      padL := maxInt(0, (m.width-boxW-2)/2)
      padR := maxInt(0, m.width-padL-boxW-2)
      border := func(s string) string {
          return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
              bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
      }
      row := func(content string) string {
          return border(menuBorderStyle.Render("│") +
              menuItemStyle.Render(padRight(content, boxW)) +
              menuBorderStyle.Render("│"))
      }

      b.WriteString(border(menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))
      b.WriteByte('\n')
      b.WriteString(border(menuBorderStyle.Render("│") +
          menuHeaderStyle.Render(centerText("Host a Network — Step 4 of 4 — Initial Areas", boxW)) +
          menuBorderStyle.Render("│")))
      b.WriteByte('\n')

      if m.wizard.areaAdding {
          if m.wizard.areaEditTag == "" {
              b.WriteString(row("  Tag (e.g. net.general):"))
          } else {
              b.WriteString(row(fmt.Sprintf("  Tag: %s  Name:", m.wizard.areaEditTag)))
          }
          b.WriteString(row("  > " + m.textInput.View()))
      } else {
          if len(m.wizard.areas) == 0 {
              b.WriteString(row("  (no areas yet — press A to add)"))
          }
          for i, a := range m.wizard.areas {
              cursor := "  "
              if i == m.wizard.areaCursor {
                  cursor = "> "
              }
              b.WriteString(row(fmt.Sprintf("  %s%-20s %s", cursor, a.Tag, a.Name)))
              b.WriteByte('\n')
          }
      }

      b.WriteString(border(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(row("  A Add area  D Delete  Enter Confirm  ESC Back"))
      b.WriteString(border(menuBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘")))
      b.WriteByte('\n')

      for i := 0; i < bottomPad; i++ {
          b.WriteString(bgLine)
          b.WriteByte('\n')
      }
      b.WriteString(m.renderMessage())
      b.WriteByte('\n')
      b.WriteString(m.renderHelpBar("A Add  D Delete  Enter Confirm  ESC Back"))
      return b.String()
  }

  // viewWizardInputBox renders a generic single-field wizard step box.
  func (m Model) viewWizardInputBox(title, helpText, notice string) string {
      var b strings.Builder
      b.WriteString(m.globalHeaderLine())
      b.WriteByte('\n')

      bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
      boxW := 60
      boxH := 7
      if notice != "" {
          boxH = 9
      }
      extraV := maxInt(0, m.height-boxH-3)
      topPad := extraV / 2
      bottomPad := extraV - topPad

      for i := 0; i < topPad; i++ {
          b.WriteString(bgLine)
          b.WriteByte('\n')
      }

      padL := maxInt(0, (m.width-boxW-2)/2)
      padR := maxInt(0, m.width-padL-boxW-2)
      border := func(s string) string {
          return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
              bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
      }

      b.WriteString(border(editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))
      b.WriteByte('\n')
      b.WriteString(border(editBorderStyle.Render("│") +
          menuHeaderStyle.Render(centerText(title, boxW)) +
          editBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(border(editBorderStyle.Render("│") +
          helpStyle.Render(padRight("  "+helpText, boxW)) +
          editBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(border(editBorderStyle.Render("│") +
          fieldValueStyle.Render(padRight("  > "+m.textInput.View(), boxW)) +
          editBorderStyle.Render("│")))
      b.WriteByte('\n')

      if notice != "" {
          b.WriteString(border(editBorderStyle.Render("│") +
              helpStyle.Render(padRight("  "+notice, boxW)) +
              editBorderStyle.Render("│")))
          b.WriteByte('\n')
      }

      b.WriteString(border(editBorderStyle.Render("│") +
          helpStyle.Render(strings.Repeat(" ", boxW)) +
          editBorderStyle.Render("│")))
      b.WriteByte('\n')
      b.WriteString(border(editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘")))
      b.WriteByte('\n')

      for i := 0; i < bottomPad; i++ {
          b.WriteString(bgLine)
          b.WriteByte('\n')
      }
      b.WriteString(m.renderMessage())
      b.WriteByte('\n')
      b.WriteString(m.renderHelpBar("Enter Confirm  ESC Back"))
      return b.String()
  }
  ```

- [ ] **Step 2: Check for missing helper methods**

  The view uses `m.renderMessage()` and `m.renderHelpBar()`. Check whether these exist:

  ```bash
  grep -n "func (m Model) renderMessage\|func (m Model) renderHelpBar" internal/configeditor/*.go
  ```

  If they don't exist, look at how the existing views render their message/help lines (e.g. `view_list.go`) and use the same inline pattern instead.

- [ ] **Step 3: Verify compilation**

  ```bash
  go build ./internal/configeditor/...
  ```

  Expected: no errors. Fix any undefined symbol errors by checking the view helpers used in other view files (e.g. `helpStyle`, `fieldValueStyle`, `menuItemStyle`).

- [ ] **Step 4: Run full test suite**

  ```bash
  go test ./...
  ```

  Expected: all tests pass.

- [ ] **Step 5: Manual smoke test of `./config`**

  ```bash
  cd /opt/bbs && ./config
  ```

  Verify:
  - Top menu shows `E — V3Net Setup`
  - Pressing `E` opens the fork screen with `J` and `H` options
  - Pressing `J` starts a leaf wizard; entering an HTTPS URL advances to step 2
  - Pressing `H` starts a hub wizard; entering a network name advances to step 2
  - ESC returns to the fork or top menu as expected

- [ ] **Step 6: Commit**

  ```bash
  git add internal/configeditor/view_v3net_wizard.go
  git commit -m "feat(config): add V3Net setup wizard view rendering"
  ```

---

## Final Verification

- [ ] **Run `gofmt` on all changed files**

  ```bash
  gofmt -w internal/config/config.go \
            internal/v3net/service.go \
            internal/v3net/hub/hub.go \
            internal/configeditor/model.go \
            internal/configeditor/update_v3net_wizard.go \
            internal/configeditor/view_v3net_wizard.go \
            cmd/vision3/main.go
  ```

- [ ] **Run `go vet`**

  ```bash
  go vet ./...
  ```

  Expected: no issues.

- [ ] **Run full test suite with race detector**

  ```bash
  go test -race ./internal/v3net/... ./internal/configeditor/...
  ```

  Expected: all tests pass, no race conditions.

- [ ] **Final commit if any formatting changes**

  ```bash
  git add -u && git diff --cached --quiet || git commit -m "style: gofmt"
  ```
