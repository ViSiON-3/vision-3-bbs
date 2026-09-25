package configeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// zipLabField finds a field by label on a ZipLab sub-screen.
func zipLabField(t *testing.T, fields []fieldDef, label string) fieldDef {
	t.Helper()
	for _, f := range fields {
		if f.Label == label {
			return f
		}
	}
	t.Fatalf("no field %q", label)
	return fieldDef{}
}

func TestZipLabMenuScreensHaveFields(t *testing.T) {
	m := newRecordModel()
	m.configs.ZipLab = ziplab.DefaultConfig()
	var labels []string
	for _, it := range zipLabMenuItems() {
		labels = append(labels, it.Label)
		if len(it.Build(m)) == 0 {
			t.Errorf("screen %q has no fields", it.Label)
		}
	}
	if want := []string{"General", "Pipeline Steps", "Virus Scan"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("ZipLab screens = %v, want %v", labels, want)
	}
}

// Every field must write the setting it shows.
func TestZipLabFieldsRoundTrip(t *testing.T) {
	cfg := ziplab.DefaultConfig()
	general := zipLabFieldsGeneral(&cfg)
	steps := zipLabFieldsSteps(&cfg)
	scan := zipLabFieldsVirusScan(&cfg)

	sets := []struct {
		fields []fieldDef
		label  string
		val    string
	}{
		{general, "Run on Upload", "N"},
		{general, "Scan Failure", "quarantine"},
		{general, "Quarantine Path", "data/quarantine"},
		{steps, "Extract", "N"},
		{steps, "Patterns File", "ADS.TXT"},
		{steps, "Comment File", "C.TXT"},
		{steps, "File to Include", "MYBBS.AD"},
		{scan, "Enabled", "Y"},
		{scan, "Command", "clamdscan"},
		{scan, "Args", `["--fdpass","{WORKDIR}"]`},
		{scan, "Timeout Secs", "300"},
	}
	for _, s := range sets {
		f := zipLabField(t, s.fields, s.label)
		if err := f.Set(s.val); err != nil {
			t.Fatalf("Set %s = %q: %v", s.label, s.val, err)
		}
		if got := f.Get(); got != s.val {
			t.Errorf("%s: Get after Set(%q) = %q", s.label, s.val, got)
		}
	}

	want := ziplab.DefaultConfig()
	want.RunOnUpload = false
	want.ScanFailBehavior = "quarantine"
	want.QuarantinePath = "data/quarantine"
	want.Steps.ExtractToTemp.Enabled = false
	want.Steps.RemoveAds.PatternsFile = "ADS.TXT"
	want.Steps.AddComment.CommentFile = "C.TXT"
	want.Steps.IncludeFile.FilePath = "MYBBS.AD"
	want.Steps.VirusScan.Enabled = true
	want.Steps.VirusScan.Command = "clamdscan"
	want.Steps.VirusScan.Args = []string{"--fdpass", "{WORKDIR}"}
	want.Steps.VirusScan.Timeout = 300
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("config after edits:\n got  %+v\n want %+v", cfg, want)
	}
}

// Args takes the JSON array the other command editors use, or plain
// space-separated words; a malformed array is refused and changes nothing.
func TestZipLabArgsParsing(t *testing.T) {
	cfg := ziplab.DefaultConfig()
	f := zipLabField(t, zipLabFieldsVirusScan(&cfg), "Args")
	if err := f.Set(`--stdout {WORKDIR}`); err != nil {
		t.Fatalf("space-separated args: %v", err)
	}
	if want := []string{"--stdout", "{WORKDIR}"}; !reflect.DeepEqual(cfg.Steps.VirusScan.Args, want) {
		t.Errorf("Args = %q, want %q", cfg.Steps.VirusScan.Args, want)
	}
	if err := f.Set(`["--stdout", "{WORKDIR}"`); err == nil {
		t.Error("Args accepted a malformed JSON array")
	}
	if want := []string{"--stdout", "{WORKDIR}"}; !reflect.DeepEqual(cfg.Steps.VirusScan.Args, want) {
		t.Errorf("a rejected value changed Args to %q", cfg.Steps.VirusScan.Args)
	}
}

// The pipeline deletes on any value but "quarantine", so that is what the
// field must show for a hand-edited or empty value.
func TestZipLabScanFailureShowsDeleteForUnknownValues(t *testing.T) {
	cfg := ziplab.DefaultConfig()
	cfg.ScanFailBehavior = "Quarantine!"
	if got := zipLabField(t, zipLabFieldsGeneral(&cfg), "Scan Failure").Get(); got != "delete" {
		t.Errorf("Scan Failure = %q, want delete", got)
	}
}

func TestZipLabNotes(t *testing.T) {
	cfg := ziplab.DefaultConfig()
	if got := zipLabGeneralNote(&cfg); !strings.Contains(got, "nothing is quarantined") {
		t.Errorf("default general note = %q", got)
	}
	cfg.Steps.VirusScan.Enabled = true
	if got := zipLabGeneralNote(&cfg); got != "" {
		t.Errorf("general note with a working setup = %q, want none", got)
	}
	cfg.ScanFailBehavior = "quarantine"
	if got := zipLabGeneralNote(&cfg); !strings.Contains(got, "deleted") {
		t.Errorf("quarantine without a path: note = %q", got)
	}
	cfg.RunOnUpload = false
	if got := zipLabGeneralNote(&cfg); !strings.Contains(got, "not processed") {
		t.Errorf("run on upload off: note = %q", got)
	}

	if got := zipLabStepsNote(&cfg); got != "" {
		t.Errorf("steps note with extract on = %q, want none", got)
	}
	cfg.Steps.ExtractToTemp.Enabled = false
	if got := zipLabStepsNote(&cfg); !strings.Contains(got, "virus scan and DIZ/ads") {
		t.Errorf("extract off: steps note = %q", got)
	}
}

// A board that never touches ZipLab must not gain a ziplab.json just because
// something else was saved; one that edits it must get the file, holding
// only ZipLab's settings.
func TestSaveAllWritesZipLabOnlyWhenNeeded(t *testing.T) {
	dir := t.TempDir()
	ac, err := loadAllConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &Model{configPath: dir, dirty: true, configs: &ac}
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	path := filepath.Join(dir, "ziplab.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ziplab.json written for an untouched default config (stat err %v)", err)
	}

	m.configs.ZipLab.Steps.VirusScan.Enabled = true
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ziplab.json not written after an edit: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["archiveTypes"]; ok {
		t.Error("archiveTypes written to ziplab.json")
	}
	got, err := ziplab.ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Steps.VirusScan.Enabled {
		t.Error("the edit was not saved")
	}
}

// A ziplab.json that does not parse must stop the editor loading, as doors
// does, rather than present defaults that a save would write over it.
func TestLoadAllConfigsPropagatesZipLabError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ziplab.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAllConfigs(dir); err == nil {
		t.Error("expected an error for an unparseable ziplab.json")
	}
}

func TestZipLabScanNote(t *testing.T) {
	vs := ziplab.DefaultConfig().Steps.VirusScan
	if got := zipLabScanNote(&vs); got != "" {
		t.Errorf("note with the scan off = %q, want none", got)
	}
	vs.Enabled = true
	vs.Command = ""
	if got := zipLabScanNote(&vs); !strings.Contains(got, "No command") {
		t.Errorf("no command: note = %q", got)
	}
	vs.Command = "no-such-scanner-ziplab-test"
	if got := zipLabScanNote(&vs); !strings.Contains(got, "not found") {
		t.Errorf("missing scanner: note = %q", got)
	}
	vs.Command = os.Args[0] // the test binary: present on every platform
	if got := zipLabScanNote(&vs); got != "" {
		t.Errorf("scanner present: note = %q, want none", got)
	}
}
