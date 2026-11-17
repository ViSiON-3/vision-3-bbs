package menu

import (
	"sync/atomic"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// acsCallInfo is what the ACS session terms read about a call that its
// connection doesn't carry: the node it is on, for NODE, and the width the
// screens render at, for WIDTH. It lives in the session's own context, so
// it goes away with the session, and wrapped sessions share it.
type acsCallInfo struct {
	node, width atomic.Int64
}

// acsCallKey is the session context key for a call's *acsCallInfo.
type acsCallKey struct{}

// acsCallInfoOf returns s's call info, or nil before it is recorded.
func acsCallInfoOf(s ssh.Session) *acsCallInfo {
	if s == nil {
		return nil
	}
	ctx := s.Context()
	if ctx == nil {
		return nil
	}
	info, _ := ctx.Value(acsCallKey{}).(*acsCallInfo)
	return info
}

// SetACSCall records the node s is on and the width its screens render at,
// clamped as the screens clamp it. The session handler calls it when the
// call connects, before any ACS check, and again once the caller's width
// is settled; the menu loop calls it as the width changes.
func SetACSCall(s ssh.Session, node, width int) {
	info := acsCallInfoOf(s)
	if info == nil {
		if s == nil || s.Context() == nil {
			return
		}
		info = &acsCallInfo{}
		s.Context().SetValue(acsCallKey{}, info)
	}
	info.node.Store(int64(node))
	info.width.Store(int64(user.ClampScreenWidth(width)))
}

// Node is the node the call is on, 0 when it isn't recorded.
func (a acsSession) Node() int {
	if info := acsCallInfoOf(a.s); info != nil {
		return int(info.node.Load())
	}
	return 0
}

// Width is the width the screens render at. When it isn't recorded it is
// the terminal's, clamped the same way.
func (a acsSession) Width() int {
	if info := acsCallInfoOf(a.s); info != nil {
		if w := info.width.Load(); w > 0 {
			return int(w)
		}
	}
	_, w := resolveTermSize(a.s)
	return user.ClampScreenWidth(w)
}

// Transport is how the caller connected: a local pipe or socket, telnet,
// else SSH.
func (a acsSession) Transport() string {
	switch {
	case isLocalTransport(a.s):
		return acs.TransportLocal
	case isTelnetSession(a.s):
		return acs.TransportTelnet
	}
	return acs.TransportSSH
}

// isLocalTransport reports whether s comes over a pipe or a Unix socket
// rather than the network.
func isLocalTransport(s ssh.Session) bool {
	addr := s.RemoteAddr()
	if addr == nil {
		return false
	}
	n := addr.Network()
	return n == "pipe" || n == "unix"
}

// Encoding is the session's output encoding.
func (a acsSession) Encoding() string {
	if sessionOutputMode(a.s) == ansi.OutputModeUTF8 {
		return acs.EncodingUTF8
	}
	return acs.EncodingCP437
}
