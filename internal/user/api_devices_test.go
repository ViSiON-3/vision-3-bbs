package user

import (
	"errors"
	"testing"
	"time"
)

func newDeviceTestMgr(t *testing.T) (*UserMgr, string, *User) {
	t.Helper()
	dir := t.TempDir()
	um, err := NewUserManager(dir)
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Pocket", "Pocket Phone", "Here")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	return um, dir, u
}

func TestAPIDevices_PairPersistsAndIsFound(t *testing.T) {
	um, dir, u := newDeviceTestMgr(t)
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev1", Name: "Phone", PublicKey: "key1", CreatedAt: time.Now()}, 10); err != nil {
		t.Fatalf("AddAPIDevice: %v", err)
	}
	reloaded, err := NewUserManager(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	owner, d, ok := reloaded.FindAPIDevice("dev1")
	if !ok || owner.ID != u.ID || d.Name != "Phone" {
		t.Fatalf("FindAPIDevice after reload = %v %+v %v", owner, d, ok)
	}
	if d.PasswordStamp != PasswordStamp(owner.PasswordHash) {
		t.Error("device not stamped with the current password")
	}
}

// A session holding a copy from before the pairing must not drop the device
// when it saves that copy.
func TestAPIDevices_StaleCopyDoesNotDropDevice(t *testing.T) {
	um, _, _ := newDeviceTestMgr(t)
	stale, _ := um.GetUser("pocket")
	if err := um.AddAPIDevice(stale.ID, APIDevice{ID: "dev1", PublicKey: "key1"}, 10); err != nil {
		t.Fatal(err)
	}
	stale.RealName = "Changed In Session"
	if err := um.UpdateUser(stale); err != nil {
		t.Fatal(err)
	}
	if err := um.UpdateUserByID(stale); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := um.FindAPIDevice("dev1"); !ok {
		t.Error("saving a stale copy dropped the paired device")
	}
	if cur, _ := um.GetUser("pocket"); cur.RealName != "Changed In Session" {
		t.Errorf("session change lost: RealName = %q", cur.RealName)
	}
}

func TestAPIDevices_PasswordChangeVoidsDevices(t *testing.T) {
	um, _, u := newDeviceTestMgr(t)
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev1", PublicKey: "key1"}, 10); err != nil {
		t.Fatal(err)
	}
	cur, _ := um.GetUser("pocket")
	cur.PasswordHash = "a-new-hash"
	if err := um.UpdateUser(cur); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := um.FindAPIDevice("dev1"); ok {
		t.Fatal("device still valid after a password change")
	}
	// The voided device is dropped at the next change, not just hidden, so
	// pairing the same key again works.
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev2", PublicKey: "key1"}, 1); err != nil {
		t.Fatalf("re-pair after password change: %v", err)
	}
	after, _ := um.GetUser("pocket")
	if len(after.APIDevices) != 1 || after.APIDevices[0].ID != "dev2" {
		t.Errorf("devices = %+v, want just dev2", after.APIDevices)
	}
}

func TestAPIDevices_LimitsRemoveAndTouch(t *testing.T) {
	um, _, u := newDeviceTestMgr(t)
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev1", PublicKey: "key1"}, 2); err != nil {
		t.Fatal(err)
	}
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dup", PublicKey: "key1"}, 2); !errors.Is(err, ErrDeviceKeyExists) {
		t.Errorf("duplicate key: err = %v", err)
	}
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev2", PublicKey: "key2"}, 2); err != nil {
		t.Fatal(err)
	}
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev3", PublicKey: "key3"}, 2); !errors.Is(err, ErrTooManyDevices) {
		t.Errorf("over cap: err = %v", err)
	}
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if err := um.TouchAPIDevice(u.ID, "dev2", at); err != nil {
		t.Fatal(err)
	}
	if _, d, _ := um.FindAPIDevice("dev2"); !d.LastUsed.Equal(at) {
		t.Errorf("LastUsed = %v, want %v", d.LastUsed, at)
	}
	if removed, err := um.RemoveAPIDevice(u.ID, "dev1"); err != nil || !removed {
		t.Fatalf("RemoveAPIDevice = %v, %v", removed, err)
	}
	if removed, _ := um.RemoveAPIDevice(u.ID, "dev1"); removed {
		t.Error("removed the same device twice")
	}
	if _, _, ok := um.FindAPIDevice("dev1"); ok {
		t.Error("removed device still found")
	}
}

func TestAPIDevices_DeletedUserCannotPair(t *testing.T) {
	um, _, u := newDeviceTestMgr(t)
	cur, _ := um.GetUser("pocket")
	cur.DeletedUser = true
	if err := um.UpdateUser(cur); err != nil {
		t.Fatal(err)
	}
	if err := um.AddAPIDevice(u.ID, APIDevice{ID: "dev1", PublicKey: "key1"}, 10); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}
