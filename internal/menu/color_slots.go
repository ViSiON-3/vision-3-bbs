package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// ApplyColorSlotDefaults makes the board's slot defaults from strings.json
// (an older file's defColorN) what |C1 to |C7 draw where there is no caller:
// before login, in a menu's own .ANS art, anywhere pipe codes are expanded
// without a session. The BBS calls it whenever it loads strings.json.
func ApplyColorSlotDefaults(s config.StringsConfig) {
	ansi.SetColorSlotDefaults(s.LegacyColorSlots)
}

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
// are filled in with the other placeholders. Each is the colour's escape
// sequence rather than its pipe code: a code put back into the text would be
// read again with what follows it, and |B1 before "0 files" is |B10.
func (e *MenuExecutor) addColorSlots(placeholders map[string]string, u *user.User) {
	for n := 1; n <= user.ColorSlots; n++ {
		placeholders[ansi.ColorSlotCode(n)] = string(ansi.ReplacePipeCodes([]byte(e.colorSlot(u, n))))
	}
}

// isColorSlotKey reports whether k is one of the caller colour slots, |C1 to
// |C7.
func isColorSlotKey(k string) bool {
	return len(k) == 3 && k[0] == '|' && k[1] == 'C' && k[2] >= '1' && k[2] <= '7'
}
