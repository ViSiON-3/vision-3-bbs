package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// boardWithBinkdConf lays out a BBS root holding the ftn.json the model was
// loaded from and a binkd.conf as the wizard first wrote it, pointing
// m.configPath at it. It returns the binkd.conf path and the content written,
// in which {OUT} has become the board's real outbound: a save repoints any
// other path, which would hide the change under test.
func boardWithBinkdConf(t *testing.T, m *Model, binkdConf string) (binkdPath, written string) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "configs")
	binkdPath = filepath.Join(root, "data", "ftn", "binkd.conf")
	written = strings.ReplaceAll(binkdConf, "{OUT}", filepath.Join(root, "data", "ftn", "out"))
	for _, dir := range []string{configPath, filepath.Dir(binkdPath)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveFTNConfig(configPath, m.configs.FTN); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binkdPath, []byte(written), 0600); err != nil {
		t.Fatal(err)
	}
	m.configPath = configPath
	return binkdPath, written
}

const fsxnetBinkdConf = `domain fsxnet {OUT} 21
address 21:4/158@fsxnet
sysname "Test BBS"
iport 24554

# --- fsxnet (added by FTN Setup Wizard) ---
node 21:1/100@fsxnet agency.bbs.nz:24554 sesspw
`

// TestConfirmFTNWizardEditUpdatesBinkdAddress is the reported bug: a node
// number mistyped at setup and corrected by re-running the wizard. The hub's
// node line already existed, so only that line was revisited and binkd.conf
// kept the mistyped address. binkd went on announcing it, and the hub held
// every netmail for the corrected one.
func TestConfirmFTNWizardEditUpdatesBinkdAddress(t *testing.T) {
	m := configuredModel()
	binkdPath, conf := boardWithBinkdConf(t, &m, fsxnetBinkdConf)

	m, _ = m.startFTNWizardEdit("fsxnet")
	m.ftnWizard.ownAddress = "21:4/159"
	m, _ = m.confirmFTNWizard()
	if strings.Contains(m.message, "ERROR") || strings.Contains(m.message, "Warning") {
		t.Fatalf("save did not succeed cleanly: %q", m.message)
	}

	got, err := os.ReadFile(binkdPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(conf, "address 21:4/158@fsxnet", "address 21:4/159@fsxnet", 1); string(got) != want {
		t.Errorf("binkd.conf after the edit:\n%s\nwant:\n%s", got, want)
	}
}

// The same correction made on the Echomail Networks screen goes through the
// ordinary save, with no wizard involved.
func TestSaveAllUpdatesBinkdAddressAfterOwnAddressEdit(t *testing.T) {
	cm := configuredModel()
	m := &cm
	binkdPath, _ := boardWithBinkdConf(t, m, fsxnetBinkdConf)

	setField(t, m.fieldsFTNLink(), "Own Address", "21:4/159")
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %q", m.message)
	}
	if strings.Contains(m.message, "warning") {
		t.Fatalf("save did not succeed cleanly: %q", m.message)
	}

	got, err := os.ReadFile(binkdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "address 21:4/159@fsxnet\n") || strings.Contains(string(got), "21:4/158") {
		t.Errorf("binkd.conf still declares the old address:\n%s", got)
	}

	// A second save has nothing left to change and must not disturb the file.
	before, err := os.Stat(binkdPath)
	if err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("second saveAll: %q", m.message)
	}
	after, err := os.Stat(binkdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("binkd.conf was rewritten by a save that changed nothing")
	}
}

// A sysop running a different AKA in binkd.conf than ftn.json names keeps it
// through an address edit: only the address the editor itself replaced is ever
// rewritten.
func TestSaveAllLeavesSysopBinkdAKA(t *testing.T) {
	cm := configuredModel()
	m := &cm
	binkdPath, conf := boardWithBinkdConf(t, m,
		strings.Replace(fsxnetBinkdConf, "address 21:4/158@fsxnet", "address 21:4/158.12@fsxnet", 1))

	setField(t, m.fieldsFTNLink(), "Own Address", "21:4/159")
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %q", m.message)
	}

	got, err := os.ReadFile(binkdPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != conf {
		t.Errorf("binkd.conf should be untouched:\n%s", got)
	}
}

// A binkd.conf that could not be synced leaves ftn.json saved with the new
// address all the same. The old one is then gone from disk, so the next save
// has to remember it, or the stale line outlives the failure that caused it.
func TestSaveAllRetriesBinkdAddressAfterFailedSync(t *testing.T) {
	cm := configuredModel()
	m := &cm
	binkdPath, conf := boardWithBinkdConf(t, m, fsxnetBinkdConf)

	// A directory where the file should be fails every read of it.
	if err := os.Remove(binkdPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(binkdPath, 0755); err != nil {
		t.Fatal(err)
	}

	setField(t, m.fieldsFTNLink(), "Own Address", "21:4/159")
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %q", m.message)
	}
	if !strings.Contains(m.message, "binkd.conf sync failed") {
		t.Fatalf("the sync was meant to fail, got: %q", m.message)
	}

	if err := os.Remove(binkdPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binkdPath, []byte(conf), 0600); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("second saveAll: %q", m.message)
	}

	got, err := os.ReadFile(binkdPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(conf, "address 21:4/158@fsxnet", "address 21:4/159@fsxnet", 1); string(got) != want {
		t.Errorf("binkd.conf after the retry:\n%s\nwant:\n%s", got, want)
	}
	if len(m.staleBinkdAddrs) != 0 {
		t.Errorf("nothing should be left to retry, got %v", m.staleBinkdAddrs)
	}
}
