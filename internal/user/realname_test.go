package user

import (
	"errors"
	"testing"
)

// Signup has always demanded a real name, but nothing enforced it afterwards:
// the sysop editors and the scripting API set the field with no validation. A
// blanked real name silently turns off an area's real_name_only flag for that
// user, which for a network whose policy is real names is a policy breach
// nobody can see.
func TestValidateRealName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"Robbie Whiting", nil},
		{"Joe Blow", nil},
		{"mark p", nil},     // short but real; the rule is length + a space
		{"Xendrome .", nil}, // already in live data, must keep validating
		{"Anakin Skywalker", nil},
		{"  Joe Blow  ", nil}, // surrounding space is trimmed, not a failure

		{"", ErrRealNameRequired},
		{"   ", ErrRealNameRequired},  // whitespace only is blank
		{"\t\n", ErrRealNameRequired}, // so is other whitespace

		{"Bob", ErrRealNameTooShort}, // under 4 characters
		{"a b", ErrRealNameTooShort}, // has a space but still too short

		{"Robbie", ErrRealNameNeedsSpace}, // a handle, not a name
		{"J0hnnyA1pha", ErrRealNameNeedsSpace},
	} {
		got := ValidateRealName(tc.name)
		if !errors.Is(got, tc.want) {
			t.Errorf("ValidateRealName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Length is counted in runes: a four-character name in a non-ASCII script is a
// real name, and byte-counting would reject it.
func TestValidateRealNameCountsRunesNotBytes(t *testing.T) {
	// Four runes, eight bytes, with a space — valid by the rule.
	if err := ValidateRealName("日本 語学"); err != nil {
		t.Errorf("a four-rune name should be accepted, got %v", err)
	}
	// Three runes, six bytes: byte-counting would have let this through.
	if err := ValidateRealName("日 本"); !errors.Is(err, ErrRealNameTooShort) {
		t.Errorf("a three-rune name should be too short, got %v", err)
	}
}

// Every real name currently on a live board must keep validating, or the fix
// locks sysops out of editing existing accounts.
func TestValidateRealNameAcceptsExistingLiveData(t *testing.T) {
	for _, name := range []string{
		"Gavin Gavin", "Joe Blow", "Bill Brooks", "mark p", "Xendrome .",
		"Logan Smith", "mikhail ter", "Robbie Whiting", "Cam Marshall",
		"Octavio Delazavalos", "Anakin Skywalker",
	} {
		if err := ValidateRealName(name); err != nil {
			t.Errorf("existing real name %q rejected: %v", name, err)
		}
	}
}

func TestHasRealName(t *testing.T) {
	for _, tc := range []struct {
		real string
		want bool
	}{
		{"Robbie Whiting", true},
		{"x", true}, // HasRealName asks only whether there is something to use
		{"", false},
		{"   ", false},
	} {
		u := &User{RealName: tc.real}
		if got := u.HasRealName(); got != tc.want {
			t.Errorf("HasRealName(%q) = %v, want %v", tc.real, got, tc.want)
		}
	}
	// A nil user has no real name rather than panicking: the compose path
	// reaches this while deciding whether an area's flag can be honoured.
	var nilUser *User
	if nilUser.HasRealName() {
		t.Error("a nil user must not report having a real name")
	}
}
