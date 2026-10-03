package main

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// encodingProbeTimeout bounds the wait for a terminal to answer the encoding
// probe. A terminal that never answers costs this once, at connect.
const encodingProbeTimeout = 750 * time.Millisecond

// connectEncoding picks the session's encoding at connect: what the terminal
// showed it decodes, or the guess from its terminal type when the probe got
// no usable answer.
func connectEncoding(byName, probed ansi.OutputMode) ansi.OutputMode {
	if probed != ansi.OutputModeAuto {
		return probed
	}
	return byName
}

// loginEncoding applies the caller's saved encoding, if they forced one, over
// the encoding detected at connect.
func loginEncoding(detected ansi.OutputMode, pref string) ansi.OutputMode {
	switch pref {
	case "cp437":
		return ansi.OutputModeCP437
	case "utf8":
		return ansi.OutputModeUTF8
	}
	return detected
}

// needsFirstLoginSetup reports whether the caller has no saved screen size.
func needsFirstLoginSetup(u *user.User) bool {
	return u.ScreenWidth == 0 || u.ScreenHeight == 0
}
