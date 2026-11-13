package user

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// New-user signup has always demanded a real name: blank input is refused and
// the value must be at least four characters and contain a space. Nothing
// enforced it afterwards, so the sysop editors and the scripting API could set
// it to anything, including empty.
//
// That matters because an area flagged real_name_only falls back to the poster's
// handle when their real name is blank (see the compose path), silently. For a
// network whose policy is real names — FidoNet being the obvious one — a blanked
// field turns the flag off for that user with nothing to show it happened.
//
// The rule lives here rather than in the signup flow so every writer shares one
// definition instead of inventing its own.

// ErrRealNameRequired is returned when a real name is blank.
var ErrRealNameRequired = errors.New("real name cannot be blank: areas flagged real-name-only fall back to the handle without it")

// ErrRealNameTooShort is returned when a real name is under the minimum length.
var ErrRealNameTooShort = errors.New("real name must be at least 4 characters")

// ErrRealNameNeedsSpace is returned when a real name has no space in it, which
// is how signup distinguishes a name from a handle.
var ErrRealNameNeedsSpace = errors.New("real name must contain a space (first and last name)")

// ErrNamePipe is returned for a new handle or real name holding a pipe. Screens
// print names through the pipe-code expander, so a name like "|CL" could act on
// someone else's screen. Accounts that already have one keep it; the screens
// escape it instead. Shown in strings that expand pipe codes, where "|)" is not a
// code and prints as written.
var ErrNamePipe = errors.New("names cannot contain the pipe (|) character")

// ErrHandleGroupPrefix is returned for a new handle starting with '@', which
// mail addresses use for a user group ("@fsxnet-ops"). An account that
// already has one keeps it: a recipient that is exactly a handle is that
// user before it is a group.
var ErrHandleGroupPrefix = errors.New("handles cannot start with @, which addresses a user group")

// RealNameMinLen is the shortest accepted real name, in runes.
const RealNameMinLen = 4

// ValidateRealName reports whether name is acceptable as a user's real name,
// applying the same rule new-user signup has always applied. It returns a
// distinct error per failure so a caller can show the sysop which rule was
// missed rather than a generic rejection.
//
// The length is counted in runes, not bytes: a four-character name in a
// non-ASCII script is a real name, and byte-counting would reject it.
func ValidateRealName(name string) error {
	if err := validateRealNameForm(name); err != nil {
		return err
	}
	if strings.Contains(name, "|") {
		return ErrNamePipe
	}
	return nil
}

// ValidateRealNameEdit is ValidateRealName for changing a stored real name. An
// account made before pipes were refused keeps its name, so an edit that leaves
// the name as it was is not refused for the pipe.
func ValidateRealNameEdit(old, name string) error {
	if strings.TrimSpace(name) == strings.TrimSpace(old) {
		return validateRealNameForm(name)
	}
	return ValidateRealName(name)
}

// ValidateHandleEdit applies the pipe and '@' rules to a handle change.
// Existing accounts keep their handles, so leaving one as it was is never
// refused.
func ValidateHandleEdit(old, handle string) error {
	if strings.TrimSpace(handle) == strings.TrimSpace(old) {
		return nil
	}
	if strings.Contains(handle, "|") {
		return ErrNamePipe
	}
	if strings.HasPrefix(strings.TrimSpace(handle), "@") {
		return ErrHandleGroupPrefix
	}
	if hasControl(handle) {
		return ErrHandleControl
	}
	return nil
}

// ErrHandleControl refuses a handle with a control character (a line break,
// a tab, an escape). Signup already refuses them; the editors and AddUser
// do too, since a handle is written one per line in the retired list and
// shown on callers' screens.
var ErrHandleControl = errors.New("handles cannot contain control characters")

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validateRealNameForm(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrRealNameRequired
	}
	if utf8.RuneCountInString(trimmed) < RealNameMinLen {
		return ErrRealNameTooShort
	}
	if !strings.Contains(trimmed, " ") {
		return ErrRealNameNeedsSpace
	}
	return nil
}

// HasRealName reports whether u carries a usable real name. The compose path
// uses this to decide whether an area's real_name_only flag can be honoured at
// all, rather than silently posting under the handle.
func (u *User) HasRealName() bool {
	return u != nil && strings.TrimSpace(u.RealName) != ""
}
