package main

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// encodingProbeTimeout bounds the wait for a terminal to answer the connect
// probe (menu.ProbeTerminal): the encoding probe and the device attributes
// queries share it. A terminal that never answers costs this once, at
// connect.
const encodingProbeTimeout = 750 * time.Millisecond

// probeEncoding reports whether the connect probe should ask the terminal
// for its encoding: only when the sysop left it to detection.
func probeEncoding(flag string) bool { return flag == "auto" }

// connectEncoding picks the session's encoding at connect. A forced output
// mode wins. In auto mode it is what the probe found (CP437 when the
// terminal did not answer); with no PTY there is no terminal to ask, and
// UTF-8 is used.
func connectEncoding(flag string, isPty bool, probed ansi.OutputMode) ansi.OutputMode {
	switch flag {
	case "utf8":
		return ansi.OutputModeUTF8
	case "cp437":
		return ansi.OutputModeCP437
	case "auto":
		if !isPty {
			return ansi.OutputModeUTF8
		}
		return probed
	}
	return ansi.OutputModeAuto
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
