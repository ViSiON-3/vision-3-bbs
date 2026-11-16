package menu

import (
	"sync"
	"sync/atomic"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// acsCallInfo is what the ACS session terms read about a call that its
// connection doesn't carry: the node it is on, for NODE, and the width the
// menus render at, for WIDTH. The menu loop keeps it current.
type acsCallInfo struct {
	node, width atomic.Int64
}

// acsCallInfos maps an ssh.Session to its *acsCallInfo. It lives as long as
// the session's output mode, and is dropped with it (ClearSessionOutputMode).
var acsCallInfos sync.Map

// acsCallInfoFor returns s's call info, creating it.
func acsCallInfoFor(s ssh.Session) *acsCallInfo {
	v, _ := acsCallInfos.LoadOrStore(s, &acsCallInfo{})
	return v.(*acsCallInfo)
}

// setACSCall records s's node and the width its menus render at.
func setACSCall(s ssh.Session, node, width int) {
	info := acsCallInfoFor(s)
	info.node.Store(int64(node))
	info.width.Store(int64(width))
}

// clearACSCall drops s's call info when the session ends.
func clearACSCall(s ssh.Session) { acsCallInfos.Delete(s) }

// Node is the node the menu loop runs on, 0 before it starts.
func (a acsSession) Node() int {
	if v, ok := acsCallInfos.Load(a.s); ok {
		return int(v.(*acsCallInfo).node.Load())
	}
	return 0
}

// Width is the width the menus render at. Before the menu loop starts it
// is the terminal's.
func (a acsSession) Width() int {
	if v, ok := acsCallInfos.Load(a.s); ok {
		if w := v.(*acsCallInfo).width.Load(); w > 0 {
			return int(w)
		}
	}
	_, w := resolveTermSize(a.s)
	return w
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
