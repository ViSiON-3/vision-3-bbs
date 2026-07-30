package user

import "testing"

// NewUserMgrForTest must key its map the same way the JSON load path and every
// mutating method do — by lowercased handle — or GetUser (which lowercases the
// lookup) silently misses users seeded with any uppercase letter.
func TestNewUserMgrForTestMixedCaseHandleIsFindable(t *testing.T) {
	um := NewUserMgrForTest(&User{Handle: "SysOp", AccessLevel: 255})

	for _, lookup := range []string{"SysOp", "sysop", "SYSOP"} {
		got, ok := um.GetUser(lookup)
		if !ok {
			t.Fatalf("GetUser(%q) = not found, want the seeded user", lookup)
		}
		if got.Handle != "SysOp" {
			t.Errorf("GetUser(%q).Handle = %q, want %q", lookup, got.Handle, "SysOp")
		}
		if got.AccessLevel != 255 {
			t.Errorf("GetUser(%q).AccessLevel = %d, want 255", lookup, got.AccessLevel)
		}
	}
}

// Lowercase handles worked before the key was normalized; keep them covered so
// the fix can't regress the existing callers.
func TestNewUserMgrForTestLowercaseHandleStillFindable(t *testing.T) {
	um := NewUserMgrForTest(&User{Handle: "boss", AccessLevel: 250})

	got, ok := um.GetUser("boss")
	if !ok {
		t.Fatal("GetUser(\"boss\") = not found, want the seeded user")
	}
	if got.AccessLevel != 250 {
		t.Errorf("AccessLevel = %d, want 250", got.AccessLevel)
	}
}
