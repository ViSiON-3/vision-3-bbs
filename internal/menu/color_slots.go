package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// colorSlot returns the colour u sees for slot n (1 to user.ColorSlots), as
// a pipe code: u's own, else the board's default from an older strings.json,
// else the built-in one. u may be nil, for a screen before login.
func (e *MenuExecutor) colorSlot(u *user.User, n int) string {
	if c := u.ColorCode(n); c != "" {
		return c
	}
	if c := e.Strings().LegacyColorSlots[n-1]; c != "" {
		return c
	}
	return ansi.ColorSlotDefaults[n-1]
}

// addColorSlots puts |C1 to |C7 into placeholders as u's colours, so they
// are filled in with the other placeholders and the pipe codes they become
// are then expanded as usual.
func (e *MenuExecutor) addColorSlots(placeholders map[string]string, u *user.User) {
	for n := 1; n <= user.ColorSlots; n++ {
		placeholders[ansi.ColorSlotCode(n)] = e.colorSlot(u, n)
	}
}
