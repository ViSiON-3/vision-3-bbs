package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// applyColorSlotDefaults works out the board's default for each caller
// colour slot from the theme as drawn (its theme.colors included) and the
// strings.json legacy defaults, and makes them what |C1 to |C7 draw where
// there is no caller: before login, in a menu's own .ANS art, anywhere pipe
// codes are expanded without a session. It runs whenever the theme,
// config.json or strings.json loads. The caller holds themeMu. A caller's
// view of another theme has its own, and leaves those of the board, which
// art and tools draw, alone.
func (e *MenuExecutor) applyColorSlotDefaults() {
	legacy := e.Strings().LegacyColorSlots
	var codes [7]string
	if th := e.themeCfg.Load(); th != nil {
		codes = th.ColorSlotCodes(legacy)
	} else {
		// No theme yet: the built-in colours, under any legacy ones.
		for i, c := range legacy {
			codes[i] = c
			if c == "" {
				codes[i] = ansi.ColorSlotDefaults[i]
			}
		}
	}
	e.slotDefaults.Store(&codes)
	if e.parent == nil {
		ansi.SetColorSlotDefaults(codes)
	}
}

// colorSlot returns the colour u sees for slot n (1 to user.ColorSlots), as
// a pipe code: u's own, else the board's default (the theme's caller colour
// role, else an older strings.json's defColorN, else the built-in one). u
// may be nil, for a screen before login.
func (e *MenuExecutor) colorSlot(u *user.User, n int) string {
	if c := u.ColorCode(n); c != "" {
		return c
	}
	if e != nil {
		if e.sharesTheme {
			e = e.parent
		}
		if d := e.slotDefaults.Load(); d != nil {
			return d[n-1]
		}
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
