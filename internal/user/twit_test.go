package user

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type twitUsers map[int]*User

// GetUserByID implements the test directory lookup by stable ID.
func (d twitUsers) GetUserByID(id int) (*User, bool) { u, ok := d[id]; return u, ok }

// GetUser implements the test directory lookup by handle.
func (d twitUsers) GetUser(name string) (*User, bool) {
	for _, u := range d {
		if equalTwitName(name, u.Handle) {
			return u, true
		}
	}
	return nil, false
}

// equalTwitName compares test directory names without case or surrounding whitespace.
func equalTwitName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// TestTwitMatcher checks author names, origin scoping, exemptions and anonymous visibility.
func TestTwitMatcher(t *testing.T) {
	directory := twitUsers{2: {ID: 2, Handle: "NewHandle", RealName: "Real Name", AccessLevel: 30}, 3: {ID: 3, Handle: "Sysop", AccessLevel: 255}}
	entries := []TwitEntry{{UserID: 2, Name: "OldHandle"}, {Name: "Remote"}, {Name: "Qualified", Addr: "21:1/100"}, {UserID: 3, Name: "Sysop"}, {Name: "Anonymous"}, {Name: "Masked"}}
	m := BuildTwitMatcher(entries, directory, TwitPolicy{}, "Masked")
	for _, tc := range []struct {
		from, addr string
		want       bool
	}{
		{"newhandle", "", true}, {" REAL NAME ", "remote", true}, {"OldHandle", "", true},
		{"remote", "anywhere", true}, {"Qualified", "21:1/100", true}, {"Qualified", "21:1/101", false},
		{"Other", "", false}, {"Sysop", "", false}, {"Anonymous", "", false}, {"MASKED", "", false},
	} {
		if got := m.Hidden(tc.from, tc.addr); got != tc.want {
			t.Errorf("Hidden(%q,%q)=%v want %v", tc.from, tc.addr, got, tc.want)
		}
	}
	directory[2].AccessLevel = 200
	if BuildTwitMatcher(entries, directory, TwitPolicy{ExemptLevel: 100}, "").Hidden("OldHandle", "") {
		t.Error("promoted user still hidden")
	}
	// A bare network entry has no local exemption.
	if !BuildTwitMatcher([]TwitEntry{{Name: "Sysop"}}, directory, TwitPolicy{}, "").Hidden("sysop", "") {
		t.Error("network name incorrectly exempted")
	}
	if !BuildTwitMatcher([]TwitEntry{{UserID: 99, Name: "Gone"}}, directory, TwitPolicy{}, "").Hidden("Gone", "") {
		t.Error("missing account lost saved name")
	}
	var none *TwitMatcher
	if none.Hidden("Anyone", "") {
		t.Error("nil matcher hides")
	}
}

// TestNewTwitEntry checks name validation, duplicate detection and the list cap.
func TestNewTwitEntry(t *testing.T) {
	owner := &User{ID: 1, Handle: "Me", RealName: "My Name"}
	directory := twitUsers{1: owner, 2: {ID: 2, Handle: "Other", AccessLevel: 30}, 3: {ID: 3, Handle: "Sysop", AccessLevel: 255}}
	for _, tc := range []struct {
		name string
		want TwitEntry
		err  error
	}{
		{" other ", TwitEntry{UserID: 2, Name: "Other"}, nil},
		{" Remote@21:1/100 ", TwitEntry{Name: "Remote", Addr: "21:1/100"}, nil},
		{"Sysop@network", TwitEntry{Name: "Sysop", Addr: "network"}, nil},
		{"me", TwitEntry{}, ErrTwitSelf}, {"My Name", TwitEntry{}, ErrTwitSelf},
		{"sysop", TwitEntry{}, ErrTwitExempt}, {"", TwitEntry{}, ErrTwitInvalid},
		{"Bad@", TwitEntry{}, ErrTwitInvalid}, {"Bad\x1b", TwitEntry{}, ErrTwitInvalid},
	} {
		got, err := NewTwitEntry(owner, directory, tc.name, TwitPolicy{})
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("%q: %+v, %v; want %+v, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
	owner.TwitList = []TwitEntry{{UserID: 2, Name: "BeforeRename"}}
	if _, err := NewTwitEntry(owner, directory, "Other", TwitPolicy{}); err != ErrTwitDuplicate {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := NewTwitEntry(owner, directory, "Remote", TwitPolicy{MaxEntries: 1}); err != ErrTwitFull {
		t.Fatalf("cap: %v", err)
	}
	if (TwitPolicy{}).Limit() != 50 {
		t.Error("default cap")
	}
}

// TestTwitJSONCompatibility checks old account files and entry serialization.
func TestTwitJSONCompatibility(t *testing.T) {
	var old User
	if err := json.Unmarshal([]byte(`{"id":1,"handle":"Old"}`), &old); err != nil || len(old.TwitList) != 0 {
		t.Fatal("old account incompatible", err)
	}
	old.TwitList = []TwitEntry{{UserID: 2, Name: "Local"}, {Name: "Remote", Addr: "21:1/1"}}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var loaded User
	if err = json.Unmarshal(data, &loaded); err != nil || len(loaded.TwitList) != 2 || loaded.TwitList[1].Addr != "21:1/1" {
		t.Fatal("round trip", err)
	}
}

// TestTwitListIsSessionOwnedAcrossExternalEdit preserves caller preferences alongside sysop edits.
func TestTwitListIsSessionOwnedAcrossExternalEdit(t *testing.T) {
	original := &User{ID: 2, Handle: "Reader", AccessLevel: 30, TwitList: []TwitEntry{{Name: "Old"}}}
	um, path := mgrWithUser(t, original)
	session, _ := um.GetUser("Reader")
	writeUsersFile(t, path, &User{ID: 2, Handle: "Reader", AccessLevel: 40, TwitList: []TwitEntry{{Name: "Old"}}})
	session.TwitList = []TwitEntry{{Name: "New"}}
	if err := um.UpdateUser(session); err != nil {
		t.Fatal(err)
	}
	stored := readUsersFile(t, path)["reader"]
	if stored.AccessLevel != 40 || len(stored.TwitList) != 1 || stored.TwitList[0].Name != "New" {
		t.Fatalf("merge lost session list or sysop level: %+v", stored)
	}
	resolved, _ := um.ResolveRecipient("Reader")
	resolved.TwitList[0].Name = "Alias"
	storedCopy, _ := um.GetUser("Reader")
	if storedCopy.TwitList[0].Name != "New" {
		t.Fatal("recipient result aliases stored list")
	}
}

// TestTwitLocalHandleWithAt keeps valid local handles out of network parsing.
func TestTwitLocalHandleWithAt(t *testing.T) {
	owner := &User{ID: 1, Handle: "Me@Home"}
	directory := twitUsers{1: owner, 2: {ID: 2, Handle: "Th3 D@rk", AccessLevel: 30}, 3: {ID: 3, Handle: "Admin@Home", AccessLevel: 255}}
	entry, err := NewTwitEntry(owner, directory, " Th3 D@rk ", TwitPolicy{})
	if err != nil || entry.UserID != 2 || entry.Name != "Th3 D@rk" || entry.Addr != "" {
		t.Fatalf("local @ handle: %+v, %v", entry, err)
	}
	for _, tc := range []struct {
		name string
		want error
	}{{"Me@Home", ErrTwitSelf}, {"Admin@Home", ErrTwitExempt}} {
		if _, err := NewTwitEntry(owner, directory, tc.name, TwitPolicy{}); err != tc.want {
			t.Errorf("%s: %v want %v", tc.name, err, tc.want)
		}
	}
	directory[2].Handle = "Renamed"
	matcher := BuildTwitMatcher([]TwitEntry{entry}, directory, TwitPolicy{}, "")
	if !matcher.Hidden("Renamed", "") || !matcher.Hidden("Th3 D@rk", "") {
		t.Fatal("rename lost local match")
	}
}
