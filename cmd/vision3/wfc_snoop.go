package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

// chatStartWait bounds how long a chat request waits for the caller's
// session to reach an input point.
const chatStartWait = 3 * time.Second

// nodeTap finds the tap of the caller on nodeID whose session started at
// connectedAt, refusing a node that now holds someone else.
func nodeTap(reg *session.SessionRegistry, nodeID int, connectedAt time.Time) (*session.BbsSession, *snoop.Tap, error) {
	s := reg.Get(nodeID)
	if s == nil {
		return nil, nil, fmt.Errorf("no caller on node %d", nodeID)
	}
	s.Mutex.RLock()
	started, tap := s.StartTime, s.Tap
	s.Mutex.RUnlock()
	if !connectedAt.IsZero() && !started.Equal(connectedAt) {
		return nil, nil, fmt.Errorf("node %d now has a different caller; select again", nodeID)
	}
	if tap == nil {
		return nil, nil, fmt.Errorf("node %d cannot be snooped", nodeID)
	}
	return s, tap, nil
}

func snoopTarget(reg *session.SessionRegistry) admin.SnoopTarget {
	return func(req admin.SnoopRequest) (*snoop.Tap, admin.SnoopHeader, error) {
		s, tap, err := nodeTap(reg, req.NodeID, req.ConnectedAt)
		if err != nil {
			return nil, admin.SnoopHeader{}, err
		}
		s.Mutex.RLock()
		hdr := admin.SnoopHeader{OutputMode: "utf8", Width: s.Width, Height: s.Height}
		if s.User != nil {
			hdr.Handle = s.User.Handle
		}
		s.Mutex.RUnlock()
		if tap.CP437() {
			hdr.OutputMode = "cp437"
		}
		return tap, hdr, nil
	}
}

func typeInHook(reg *session.SessionRegistry) func(string, int, time.Time, bool) error {
	return func(sysop string, nodeID int, connectedAt time.Time, on bool) error {
		_, tap, err := nodeTap(reg, nodeID, connectedAt)
		if err != nil {
			return err
		}
		if !on {
			held, injected := tap.ReleaseKeyboard(sysop)
			if held == 0 {
				return snoop.ErrNotHolder
			}
			slog.Info("wfc-snoop: type-in off", "sysop", sysop, "node", nodeID,
				"duration", held.Round(time.Second), "bytes", injected)
			return nil
		}
		if err := tap.TakeKeyboard(sysop); err != nil {
			return err
		}
		slog.Info("wfc-snoop: type-in on", "sysop", sysop, "node", nodeID)
		return nil
	}
}

func chatHook(reg *session.SessionRegistry) func(string, int, time.Time, bool) (bool, error) {
	return func(sysop string, nodeID int, connectedAt time.Time, start bool) (bool, error) {
		s, tap, err := nodeTap(reg, nodeID, connectedAt)
		if err != nil {
			return false, err
		}
		if !start {
			if err := tap.StopChat(sysop); err != nil {
				return false, err
			}
			slog.Info("wfc-snoop: chat end requested", "sysop", sysop, "node", nodeID)
			return false, nil
		}
		started, err := tap.RequestChat(sysop, chatStartWait)
		if err != nil {
			return false, err
		}
		if !started {
			return false, nil
		}
		slog.Info("wfc-snoop: chat requested", "sysop", sysop, "node", nodeID)
		if adminServer != nil {
			var handle string
			s.Mutex.RLock()
			if s.User != nil {
				handle = s.User.Handle
			}
			s.Mutex.RUnlock()
			adminServer.ClearPage(nodeID, handle, "answered")
		}
		return true, nil
	}
}

// wfcSnoopSubsystem serves one wfc-snoop channel. Authorization matches
// wfc-admin and is re-checked for the life of the channel.
func wfcSnoopSubsystem(sess ssh.Session) {
	handle, keyBytes := wfcStashedIdentity(sess.Context())
	if handle == "" || !authorizeAdminKey(handle, keyBytes) {
		slog.Warn("wfc-snoop: access denied", "user", handle, "addr", sess.RemoteAddr())
		_ = admin.WriteSnoopError(sess, "access denied") // best-effort notice to client
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A vanished console must release the keyboard it may hold.
	if conn, ok := sess.Context().Value(ssh.ContextKeyConn).(*gossh.ServerConn); ok && conn != nil {
		stopKA := admin.KeepAlive(conn, admin.DefaultKeepAliveInterval, admin.DefaultKeepAliveTimeout, func(err error) {
			slog.Info("wfc-snoop: console stopped responding, closing session",
				"user", handle, "addr", sess.RemoteAddr(), "reason", err)
			_ = sess.Close()
			_ = conn.Close()
		})
		defer stopKA()
	}
	stillAuthorized := func(h string) bool { return authorizeAdminKey(h, keyBytes) }
	go watchAdminAuthorization(ctx, handle, wfcReauthInterval, stillAuthorized, func() {
		slog.Warn("wfc-snoop: session revoked, disconnecting", "user", handle)
		_ = sess.Close()
	})
	audit := func(msg string, args ...any) { slog.Info("wfc-snoop: "+msg, args...) }
	if err := admin.ServeSnoop(sess, handle, snoopTarget(sessionRegistry), audit); err != nil {
		slog.Info("wfc-snoop: channel closed", "user", handle, "reason", err)
	}
}

// chatCreditHook reports the sysop chat time credited to the caller on a
// node, so the WFC time left matches what the caller is allowed.
func chatCreditHook(reg *session.SessionRegistry) func(int) time.Duration {
	return func(nodeID int) time.Duration {
		s := reg.Get(nodeID)
		if s == nil {
			return 0
		}
		s.Mutex.RLock()
		credit := s.ChatCredit
		s.Mutex.RUnlock()
		if credit == nil {
			return 0
		}
		return credit()
	}
}
