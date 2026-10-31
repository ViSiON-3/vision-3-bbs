package user

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNamePipeRules(t *testing.T) {
	if err := ValidateRealName("Bad|CL Name"); !errors.Is(err, ErrNamePipe) {
		t.Errorf("ValidateRealName = %v, want ErrNamePipe", err)
	}
	if err := ValidateRealName("|CL"); !errors.Is(err, ErrRealNameTooShort) {
		t.Errorf("ValidateRealName(short) = %v, want the form error first", err)
	}
	if err := ValidateRealNameEdit("Old|CL Name", " Old|CL Name "); err != nil {
		t.Errorf("leaving an old real name alone = %v", err)
	}
	if err := ValidateRealNameEdit("Old|CL Name", "New|CL Name"); !errors.Is(err, ErrNamePipe) {
		t.Errorf("changing to a new pipe name = %v, want ErrNamePipe", err)
	}
	if err := ValidateRealNameEdit("Old|CL Name", "Clean Name"); err != nil {
		t.Errorf("cleaning an old real name = %v", err)
	}
	if err := ValidateHandleEdit("|CL", "|CL"); err != nil {
		t.Errorf("leaving an old handle alone = %v", err)
	}
	if err := ValidateHandleEdit("Plain", "Pl|CLain"); !errors.Is(err, ErrNamePipe) {
		t.Errorf("renaming to a pipe = %v, want ErrNamePipe", err)
	}
}

func TestAddUserRefusesPipes(t *testing.T) {
	um := newTestManager(t, nil)
	if _, err := um.AddUser("pw", "|CLhack", "Real Name", ""); !errors.Is(err, ErrNamePipe) {
		t.Errorf("handle: err = %v, want ErrNamePipe", err)
	}
	if _, err := um.AddUser("pw", "Fine", "Real|CL Name", ""); !errors.Is(err, ErrNamePipe) {
		t.Errorf("real name: err = %v, want ErrNamePipe", err)
	}
}

// An old handle holding a pipe must not stop a login or a save, and is not
// renamed. The file is the only way such a handle gets in now.
func TestOldPipeHandleStillWorks(t *testing.T) {
	um := newTestManager(t, nil)
	if _, err := um.AddUser("secret", "Plain", "Real Name", ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(um.path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte(`"Plain"`), []byte(`"|CL"`))
	if err := os.WriteFile(um.path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewUserManager(filepath.Dir(um.path))
	if err != nil {
		t.Fatal(err)
	}

	u, ok := reloaded.Authenticate("|CL", "secret")
	if !ok {
		t.Fatal("an old handle with a pipe could not log in")
	}
	u.GroupLocation = "Elsewhere"
	if err := reloaded.UpdateUserByID(u); err != nil {
		t.Fatalf("UpdateUserByID: %v", err)
	}
	if err := reloaded.UpdateUser(u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, ok := reloaded.GetUser("|CL")
	if !ok || got.Handle != "|CL" || got.GroupLocation != "Elsewhere" {
		t.Errorf("after saving: %+v", got)
	}

	u.Handle = "|CL2"
	if err := reloaded.UpdateUserByID(u); !errors.Is(err, ErrNamePipe) {
		t.Errorf("renaming to a new pipe handle = %v, want ErrNamePipe", err)
	}
	u.Handle = "Clean"
	if err := reloaded.UpdateUserByID(u); err != nil {
		t.Errorf("renaming to a clean handle: %v", err)
	}
}
