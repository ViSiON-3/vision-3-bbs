package usereditor

import (
	"errors"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The sysop editor set RealName with no validation, so a blank could be saved —
// silently turning off real_name_only for that user in every area that sets it.
func TestRealNameFieldRejectsInvalid(t *testing.T) {
	u := &user.User{Handle: "someone", RealName: "Robbie Whiting"}

	var set func(*user.User, string) error
	var get func(*user.User) string
	for _, f := range editFields() {
		if f.Label == "Real Name" {
			set, get = f.Set, f.Get
		}
	}
	if set == nil {
		t.Fatal(`no "Real Name" field found`)
	}

	for _, bad := range []string{"", "   ", "Bob", "Robbie"} {
		if err := set(u, bad); err == nil {
			t.Errorf("set(%q) was accepted; want a rejection", bad)
		}
		if get(u) != "Robbie Whiting" {
			t.Fatalf("a rejected edit must leave the value alone, got %q", get(u))
		}
	}

	if err := set(u, "  Joe Blow  "); err != nil {
		t.Errorf("a valid name was rejected: %v", err)
	}
	if got := get(u); got != "Joe Blow" {
		t.Errorf("stored %q, want the trimmed name", got)
	}

	if err := set(u, ""); !errors.Is(err, user.ErrRealNameRequired) {
		t.Errorf("blank should report ErrRealNameRequired, got %v", err)
	}
}
