package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestCheckFTNRuntimeReportsInvalidLinkAddress(t *testing.T) {
	var checks []doctorCheck
	add := func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	}

	checkFTNRuntime(config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {
				OwnAddress: "21:1/100",
				Links:      []config.FTNLinkConfig{{Address: "not-an-address"}},
			},
		},
	}, t.TempDir(), add)

	if !hasDoctorCheck(checks, "FTN links", doctorWarn, "invalid address \"not-an-address\"") {
		t.Fatalf("expected invalid FTN link address warning, got %#v", checks)
	}
}

func TestCheckAreasAndConferencesReportsNetworkRoutingProblems(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoctorFixture(t, filepath.Join(configDir, "conferences.json"), `[]`)
	writeDoctorFixture(t, filepath.Join(configDir, "qwknet.json"), `{
  "networks": {
    "dovenet": {"enabled": true, "hubId": "HUB", "ownId": "DEMO", "host": "hub.example.invalid", "password": "fake"}
  }
}`)
	writeDoctorFixture(t, filepath.Join(configDir, "v3net.json"), `{
  "enabled": true,
  "leaves": [{"hubUrl":"https://hub.example.invalid", "network":"demo-net", "boards":["MISSING"], "pollInterval":"5m"}]
}`)
	writeDoctorFixture(t, filepath.Join(configDir, "message_areas.json"), `[
  {"tag":"QWK_ONE", "base_path":"msgbases/qwk_one", "area_type":"qwknet", "network":"dovenet", "qwk_conference":12},
  {"tag":"QWK_TWO", "base_path":"msgbases/qwk_two", "area_type":"qwknet", "network":"dovenet", "qwk_conference":12},
  {"tag":"V3_GENERAL", "base_path":"msgbases/v3_general", "area_type":"v3net", "network":"demo-net"}
]`)
	writeDoctorFixture(t, filepath.Join(configDir, "file_areas.json"), `[
  {"id":1, "tag":"FSX_FILES", "path":"fileecho", "network":"fsxnet", "file_echo":"FSX_TEST"},
  {"id":2, "tag":"BAD_PATH", "path":"../outside", "network":"fsxnet", "file_echo":"FSX_BAD"}
]`)

	var checks []doctorCheck
	add := func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	}
	checkAreasAndConferences(configDir, root, config.FTNConfig{
		SecureInboundPath: "",
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {
				OwnAddress: "21:1/100",
				Links:      []config.FTNLinkConfig{{Address: "21:1/200"}},
			},
		},
	}, add)

	for _, want := range []struct {
		name    string
		status  doctorSeverity
		message string
	}{
		{"message areas", doctorWarn, "both map to conference 12"},
		{"message areas", doctorWarn, "does not match a local message area tag"},
		{"file echo areas", doctorWarn, "without a tic_password"},
		{"file areas", doctorFail, "unsafe path \"../outside\""},
	} {
		if !hasDoctorCheck(checks, want.name, want.status, want.message) {
			t.Errorf("expected %s check with %q; checks: %#v", want.name, want.message, checks)
		}
	}
}

func TestCheckV3NetReportsInvalidLeafSettings(t *testing.T) {
	configDir := t.TempDir()
	root := filepath.Dir(configDir)
	writeDoctorFixture(t, filepath.Join(configDir, "v3net.json"), `{
  "enabled": true,
  "leaves": [{"hubUrl":"not-a-url", "network":"demo-net", "boards":["GENERAL"], "pollInterval":"soon"}]
}`)

	var checks []doctorCheck
	checkV3Net(configDir, root, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "V3Net configuration", doctorWarn, "invalid hubUrl") {
		t.Errorf("expected invalid hub URL warning, got %#v", checks)
	}
	if !hasDoctorCheck(checks, "V3Net configuration", doctorWarn, "invalid pollInterval") {
		t.Errorf("expected invalid poll interval warning, got %#v", checks)
	}
	if !hasDoctorCheck(checks, "V3Net identity", doctorWarn, "keystore is missing") {
		t.Errorf("expected missing identity warning without creating a replacement key, got %#v", checks)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "v3net.key")); !os.IsNotExist(err) {
		t.Errorf("doctor created a missing V3Net key or stat failed unexpectedly: %v", err)
	}
}

func TestCheckMenuFilesReportsDanglingCommandReferences(t *testing.T) {
	root := t.TempDir()
	menus := filepath.Join(root, "menus", "v3")
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(menus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoctorFixture(t, filepath.Join(menus, "MAIN.CFG"), `[
  {"KEYS":"A", "CMD":"GOTO:MISSING"},
  {"KEYS":"B", "CMD":"DOOR:UNKNOWN"},
  {"KEYS":"C", "CMD":"RUN:NOTAREALRUNNABLE"},
  {"KEYS":"D", "CMD":"MSGAREA:NO_AREA"}
]`)
	writeDoctorFixture(t, filepath.Join(menus, "MAIN.MNU"), `{}`)
	writeDoctorFixture(t, filepath.Join(configDir, "doors.json"), `{}`)
	writeDoctorFixture(t, filepath.Join(configDir, "message_areas.json"), `[]`)
	var checks []doctorCheck
	checkMenuFiles(menus, configDir, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	for _, ref := range []string{`menu "MISSING"`, `door "UNKNOWN"`, `RUN target "NOTAREALRUNNABLE"`, `message area "NO_AREA"`} {
		if !hasDoctorCheck(checks, "menu references", doctorWarn, ref) {
			t.Errorf("missing reference warning for %s: %#v", ref, checks)
		}
	}
}

func TestCheckBinkdAgreementReportsMismatchedHubAndNodeSettings(t *testing.T) {
	root := t.TempDir()
	cfg := config.FTNConfig{
		SecureInboundPath: filepath.Join(root, "secure"),
		InboundPath:       filepath.Join(root, "in"),
		BinkdOutboundPath: filepath.Join(root, "out"),
		Networks: map[string]config.FTNNetworkConfig{"fsxnet": {
			OwnAddress: "21:1/100",
			Links:      []config.FTNLinkConfig{{Address: "21:1/200", PacketPassword: "pkt", SessionPassword: "session", Hostname: "hub.example", Port: 24554}},
		}},
	}
	data := "domain fsxnet " + filepath.Join(root, "out") + " 21\n" +
		"address 21:1/100@fsxnet\nnode 21:1/200@fsxnet hub.example:24554 wrong-session\n" +
		"inbound " + filepath.Join(root, "secure") + "\ninbound-nonsecure " + filepath.Join(root, "in") + "\n"
	var checks []doctorCheck
	checkBinkdAgreement(data, cfg, root, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "binkd.conf agreement", doctorWarn, "session password does not match") {
		t.Fatalf("expected session-password mismatch: %#v", checks)
	}
}

func TestCheckInfoFormsChecksRequiredLoginHookAndTemplates(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	formsDir := filepath.Join(root, "data", "infoforms")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(formsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoctorFixture(t, filepath.Join(formsDir, "config.json"), `{"required_forms":"15"}`)
	writeDoctorFixture(t, filepath.Join(configDir, "login.json"), `[]`)
	var checks []doctorCheck
	checkInfoFormSetup(configDir, root, config.ServerConfig{SysOpLevel: 255, CoSysOpLevel: 250}, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "InfoForms", doctorWarn, "does not run INFOFORMREQUIRED") {
		t.Errorf("expected missing login hook warning: %#v", checks)
	}
	if !hasDoctorCheck(checks, "InfoForms", doctorWarn, "required form 1 template is missing") {
		t.Errorf("expected required form template warning: %#v", checks)
	}
	if !hasDoctorCheck(checks, "InfoForms", doctorWarn, "required form 5 template is missing") {
		t.Errorf("expected required form template warning: %#v", checks)
	}
}

func TestCheckDefaultSysopPasswordFindsBootstrapCredential(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data", "users", "users.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	writeDoctorFixture(t, path, `[{"handle":"Felonius","accessLevel":255,"passwordHash":"`+string(hash)+`"}]`)
	var checks []doctorCheck
	checkDefaultSysopPassword(root, config.ServerConfig{SysOpLevel: 255}, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "default sysop credentials", doctorWarn, "published default password") {
		t.Fatalf("expected default-password warning: %#v", checks)
	}
}

func TestCheckShippedMenuReferencesResolve(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	var checks []doctorCheck
	checkMenuFiles(filepath.Join(root, "menus", "v3"), filepath.Join(root, "templates", "configs"), func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	for _, check := range checks {
		if check.Name == "menu references" && check.Status != doctorOK {
			t.Errorf("shipped menu reference check is %s: %s", check.Status, check.Message)
		}
	}
}

func TestCheckBadUserNamesRequiresAtLeastOnePattern(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "badusers.txt")
	writeDoctorFixture(t, path, "; comments only\n# no rules\n\n")
	var checks []doctorCheck
	checkBadUserNames(configDir, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "bad user names", doctorWarn, "no blocked names") {
		t.Fatalf("expected empty-list warning: %#v", checks)
	}
	writeDoctorFixture(t, path, "admin*\n")
	checks = nil
	checkBadUserNames(configDir, func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "bad user names", doctorOK, "1 blocked name pattern") {
		t.Fatalf("expected non-empty list pass: %#v", checks)
	}
}

func TestCheckUnknownTopLevelKeysFindsNestedTypos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ftn.json")
	writeDoctorFixture(t, path, `{"_comment":"allowed metadata","networks":{"fsxnet":{"own_addres":"21:1/100"}}}`)
	var checks []doctorCheck
	checkUnknownTopLevelKeys(path, reflect.TypeOf(config.FTNConfig{}), func(name string, status doctorSeverity, message, fix string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Fix: fix})
	})
	if !hasDoctorCheck(checks, "configs/ftn.json", doctorWarn, "networks.fsxnet.own_addres") {
		t.Fatalf("expected nested key typo warning: %#v", checks)
	}
	if strings.Contains(checks[0].Message, "_comment") {
		t.Fatalf("expected _comment to be accepted: %#v", checks)
	}
}

func TestFixDoctorDirectoriesDryRunDoesNotCreatePaths(t *testing.T) {
	root := t.TempDir()
	fixes := fixDoctorDirectories(root, true)
	if len(fixes) != len(doctorDirectoryFixes) {
		t.Fatalf("got %d planned fixes, want %d", len(fixes), len(doctorDirectoryFixes))
	}
	for i, fix := range fixes {
		if fix.Status != "would-create" {
			t.Errorf("fix %q status = %q, want would-create", fix.Path, fix.Status)
		}
		if fix.Path != doctorDirectoryFixes[i] {
			t.Errorf("fix %d path = %q, want %q", i, fix.Path, doctorDirectoryFixes[i])
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(fix.Path))); !os.IsNotExist(err) {
			t.Errorf("dry run created %q or returned unexpected stat error: %v", fix.Path, err)
		}
	}
}

func TestFixDoctorDirectoriesCreatesMissingPaths(t *testing.T) {
	root := t.TempDir()
	fixes := fixDoctorDirectories(root, false)
	if len(fixes) != len(doctorDirectoryFixes) {
		t.Fatalf("got %d fixes, want %d", len(fixes), len(doctorDirectoryFixes))
	}
	for _, fix := range fixes {
		path := filepath.Join(root, filepath.FromSlash(fix.Path))
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat created directory %q: %v", fix.Path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("created path %q is not a directory", fix.Path)
		}
		if fix.Status != "fixed" {
			t.Errorf("fix %q status = %q, want fixed", fix.Path, fix.Status)
		}
	}
}

func TestJSONErrorLocationIncludesLineAndColumn(t *testing.T) {
	data := []byte("{\n  \"name\": true,\n  \"count\": ]\n}")
	var decoded struct {
		Name string `json:"name"`
	}
	err := json.Unmarshal(data, &decoded)
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	got := jsonErrorLocation(data, err)
	if !strings.Contains(got, "line 3, column") {
		t.Fatalf("location = %q, want line 3 and a column", got)
	}
}

func hasDoctorCheck(checks []doctorCheck, name string, status doctorSeverity, messagePart string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status && strings.Contains(check.Message, messagePart) {
			return true
		}
	}
	return false
}

func writeDoctorFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
