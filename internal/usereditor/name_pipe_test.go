package usereditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// A sysop pressing Enter through an old account's fields must not be refused,
// but a new pipe is.
func TestNameFieldsRefuseNewPipes(t *testing.T) {
	for _, f := range editFields() {
		if f.Label != "Real Name" && f.Label != "Handle" {
			continue
		}
		u := &user.User{Handle: "Old|CL Name", RealName: "Old|CL Name"}
		if err := f.Set(u, "Old|CL Name"); err != nil {
			t.Errorf("%s: leaving the old value = %v", f.Label, err)
		}
		if err := f.Set(u, "New|CL Name"); err == nil {
			t.Errorf("%s: a new pipe was accepted", f.Label)
		}
		if err := f.Set(u, "Clean Name"); err != nil {
			t.Errorf("%s: a clean value = %v", f.Label, err)
		}
	}
}
