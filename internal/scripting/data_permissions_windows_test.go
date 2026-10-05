package scripting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestDataStoreCustomizedDACLRejectsUpdates checks the fail-closed path with
// an explicit protected DACL instead of a directory-inherited descriptor.
func TestDataStoreCustomizedDACLRejectsUpdates(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.data.set("kept", 1)`)
	path := filepath.Join(h.dataDir, "test.json")
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{`v3.data.set("next", 2)`, `v3.data.delete("kept")`} {
		if got := h.evalErr(expr); !strings.Contains(got, "file-specific Windows DACL") {
			t.Errorf("%s: %s", expr, got)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatalf("store changed: %q, %v", after, err)
		}
		current, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil || current.String() != original.String() {
			t.Fatalf("DACL changed: %v", err)
		}
	}
	entries, err := os.ReadDir(h.dataDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}
