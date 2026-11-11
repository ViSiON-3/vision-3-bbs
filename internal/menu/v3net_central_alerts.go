package menu

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/gliderlabs/ssh"
)

// V3Net Central activity alerts on the BBS: short live toasts that say a
// caller on another board logged on or off, joined a network chat room, or
// posted in a network message area.
//
// Two separate choices of the caller's govern them, per kind: whether
// other boards may be told when they do it here (Share), and whether they
// see the alerts other boards send (See). Both start off, and neither
// follows from the other or from any other Central switch. Each is read
// from the stored record at the moment it matters, so a switch turned off
// on another node stops the next alert. The sysop's switch for the kind on
// the network has to be on as well; the leaf enforces that both ways.
//
// An alert goes to the caller as a live page that expires with the alert,
// at most centralAlertPageTTL after it arrives: never queued for a later
// login, so a caller who comes back is not handed a backlog.

// centralAlertPageTTL is the longest an alert waits for a caller who
// cannot be shown it yet, such as one in a door.
const centralAlertPageTTL = 60 * time.Second

// CentralAlertInfo is activity alerts' state on a network.
type CentralAlertInfo struct {
	// Enabled is the sysop's switch for each kind (protocol.CentralAlert*);
	// Supported whether the hub relays alerts.
	Enabled   map[string]bool
	Supported bool
}

// centralAlertNetworks returns the networks where alerts of kind are
// offered: the sysop has them on and the hub relays them.
func centralAlertNetworks(p V3NetCentralProvider, kind string) (names []string) {
	if p == nil {
		return nil
	}
	for _, n := range p.CentralNetworks() {
		if n.Alerts.Supported && n.Alerts.Enabled[kind] {
			names = append(names, n.Network)
		}
	}
	return names
}

// centralAlertsOffered reports whether any network offers any kind.
func centralAlertsOffered(p V3NetCentralProvider) bool {
	for _, k := range protocol.CentralAlertKinds {
		if len(centralAlertNetworks(p, k)) > 0 {
			return true
		}
	}
	return false
}

// centralAlertsShared reports whether u shares some kind of alert that a
// network offers.
func centralAlertsShared(p V3NetCentralProvider, u *user.User) bool {
	for _, k := range protocol.CentralAlertKinds {
		if u.CentralAlerts.Shares(k) && len(centralAlertNetworks(p, k)) > 0 {
			return true
		}
	}
	return false
}

// centralAlertsSummary says, for My Preferences, how many of u's alert
// switches are on: what they share and what they see.
func centralAlertsSummary(u *user.User) string {
	share, see := 0, 0
	for _, k := range protocol.CentralAlertKinds {
		if u.CentralAlerts.Shares(k) {
			share++
		}
		if u.CentralAlerts.Sees(k) {
			see++
		}
	}
	if share == 0 && see == 0 {
		return "|07Off|07"
	}
	return fmt.Sprintf("|10On |07 |08(sharing %d, seeing %d of %d)|07", share, see, len(protocol.CentralAlertKinds))
}

// sendCentralAlert asks the service to announce u's action of kind, on
// network (or, for "", on every network), if u's stored record shares
// that kind.
// An invisible session announces nothing. room and area go with a chat
// join and a post.
func (e *MenuExecutor) sendCentralAlert(um *user.UserMgr, sess *session.BbsSession, u *user.User, network, kind, room, area string) {
	if e.V3NetCentral == nil || um == nil || u == nil {
		return
	}
	if sess != nil {
		sess.Mutex.RLock()
		invisible := sess.Invisible
		sess.Mutex.RUnlock()
		if invisible {
			return
		}
	}
	acct, ok := um.GetUserByID(u.ID)
	if !ok || acct.DeletedUser || !acct.CentralAlerts.Shares(kind) {
		return
	}
	handle := centralPreviewHandle(acct.Handle)
	if handle == "" {
		return
	}
	e.V3NetCentral.SendCentralAlert(network, kind, handle, room, area)
}

// SendCentralLogonAlert announces u's logon on every network that offers
// it, if they share logons. Call it once the login sequence is done.
func (e *MenuExecutor) SendCentralLogonAlert(um *user.UserMgr, sess *session.BbsSession, u *user.User) {
	e.sendCentralAlert(um, sess, u, "", protocol.CentralAlertLogon, "", "")
}

// SendCentralLogoffAlert announces that u's session has ended, if they
// share logoffs. Only a session that got past login should call it; a
// board losing its link to the hub never does.
func (e *MenuExecutor) SendCentralLogoffAlert(um *user.UserMgr, sess *session.BbsSession, u *user.User) {
	e.sendCentralAlert(um, sess, u, "", protocol.CentralAlertLogoff, "", "")
}

// sendChatJoinAlert announces the caller's successful join of room on a
// network chat, if they share chat joins.
func (e *MenuExecutor) sendChatJoinAlert(c *cmdCtx, network, room string) {
	room, err := chat.NormalizeRoom(room)
	if network == "" || err != nil {
		return
	}
	e.sendCentralAlert(c.userManager, e.nodeSession(c.nodeNumber), c.currentUser, network, protocol.CentralAlertChatJoin, room, "")
}

// sendPostAlert announces the caller's post in areaID, if the area is a
// V3Net network area and they share posts. A private or anonymous post is
// never announced.
func (e *MenuExecutor) sendPostAlert(um *user.UserMgr, nodeNumber int, u *user.User, areaID int, tag string, private, anonymous bool) {
	if private || anonymous || e.V3NetCentral == nil {
		return
	}
	network := e.V3NetCentral.NetworkForArea(areaID)
	if network == "" || !protocol.AreaTagRegexp.MatchString(tag) {
		return
	}
	e.sendCentralAlert(um, e.nodeSession(nodeNumber), u, network, protocol.CentralAlertPost, "", tag)
}

// centralAlertText is a's line for a caller, or "" when the sysop blanked
// the kind's string. area is the local area's name, for a post.
func (e *MenuExecutor) centralAlertText(a protocol.CentralAlert, areaName string) string {
	st := e.Strings()
	handle, bbs := threadEscapePipes(a.Handle), threadEscapePipes(a.BBS)
	network := threadEscapePipes(a.Network)
	switch a.Kind {
	case protocol.CentralAlertLogon:
		if st.CentralAlertLogon != "" {
			return fmt.Sprintf(st.CentralAlertLogon, handle, bbs, network)
		}
	case protocol.CentralAlertLogoff:
		if st.CentralAlertLogoff != "" {
			return fmt.Sprintf(st.CentralAlertLogoff, handle, bbs, network)
		}
	case protocol.CentralAlertChatJoin:
		if st.CentralAlertChatJoin != "" {
			return fmt.Sprintf(st.CentralAlertChatJoin, handle, bbs, threadEscapePipes(a.Room), network)
		}
	case protocol.CentralAlertPost:
		if st.CentralAlertPost != "" {
			return fmt.Sprintf(st.CentralAlertPost, handle, bbs, threadEscapePipes(areaName), network)
		}
	}
	return ""
}

// postAlertArea returns the local area a post alert names, when this board
// carries it for the alert's network.
func (e *MenuExecutor) postAlertArea(a protocol.CentralAlert) (*message.MessageArea, bool) {
	if e.MessageMgr == nil || e.V3NetCentral == nil {
		return nil, false
	}
	area, found := e.MessageMgr.GetAreaByTag(a.Area)
	if !found || e.V3NetCentral.NetworkForArea(area.ID) != a.Network {
		return nil, false
	}
	return area, true
}

// alertAreaName is how this board names area in a post alert.
func alertAreaName(area *message.MessageArea) string {
	if name := strings.TrimSpace(area.Name); name != "" {
		return name
	}
	return area.Tag
}

// mayReadAlertArea reports whether acct, in sess, may read area: its read
// ACS and its conference's, as message search checks them. Each is
// evaluated against the caller's own connection, so a term such as L or A
// reads it as the menus would.
func (e *MenuExecutor) mayReadAlertArea(acct *user.User, sess *session.BbsSession, start time.Time, area *message.MessageArea) bool {
	conn, _ := sess.Channel.(ssh.Session) // nil for a session with no connection
	if !CheckUserACS(area.ACSRead, acct, conn, start) {
		return false
	}
	if area.ConferenceID != 0 && e.ConferenceMgr != nil {
		if conf, ok := e.ConferenceMgr.GetByID(area.ConferenceID); ok && !CheckUserACS(conf.ACS, acct, conn, start) {
			return false
		}
	}
	return true
}

// DeliverCentralAlert shows a, an alert from another board that the
// service has verified, to every caller here who should see it: logged in
// and past login, asking (on their stored record) for alerts of its kind,
// not hiding its caller with their Twit List, and allowed to see what it
// is about. A post is shown only to callers who may read the area and
// its conference here, and only where this board carries it; a chat join
// not to a caller the SysOp barred from network chat. It never blocks.
func (e *MenuExecutor) DeliverCentralAlert(a protocol.CentralAlert, um *user.UserMgr) {
	if e.SessionRegistry == nil || um == nil {
		return
	}
	ttl := min(time.Until(a.Expires()), centralAlertPageTTL)
	if ttl <= 0 {
		return
	}
	var area *message.MessageArea
	var areaName string
	if a.Kind == protocol.CentralAlertPost {
		var ok bool
		if area, ok = e.postAlertArea(a); !ok {
			return
		}
		areaName = alertAreaName(area)
	}
	text := e.centralAlertText(a, areaName)
	if text == "" {
		return
	}
	shown := 0
	for _, sess := range e.SessionRegistry.ListActive() {
		sess.Mutex.RLock()
		u, start := sess.User, sess.StartTime
		sess.Mutex.RUnlock()
		if u == nil || !sess.ToastsReady() {
			continue
		}
		acct, ok := um.GetUserByID(u.ID)
		if !ok || acct.DeletedUser || !acct.CentralAlerts.Sees(a.Kind) {
			continue
		}
		if a.Kind == protocol.CentralAlertChatJoin && acct.CentralChatBarred {
			continue
		}
		if area != nil && !e.mayReadAlertArea(acct, sess, start, area) {
			continue
		}
		if twitMatcher(e, um, acct).Hidden(a.Handle, a.NodeID) {
			continue
		}
		sess.AddExpiringPage(text, ttl)
		shown++
	}
	slog.Debug("v3net central alert delivered", "network", a.Network, "kind", a.Kind, "node", a.NodeID, "sessions", shown)
}

// centralAlertLabels name each kind on the Alerts screen: what the caller
// shares, then what they see.
var centralAlertLabels = map[string][2]string{
	protocol.CentralAlertLogon:    {"I log on", "someone logs on"},
	protocol.CentralAlertLogoff:   {"I log off", "someone logs off"},
	protocol.CentralAlertChatJoin: {"I join network chat", "someone joins net chat"},
	protocol.CentralAlertPost:     {"I post a message", "someone posts"},
}

// runV3NetCentralAlerts is where the caller chooses their activity
// alerts: 1-4 what other boards may be told of them, 5-8 what they see of
// other boards. Each switch is offered only where some network has its
// kind on, and the screen shows the line other boards would see before a
// Share switch is turned on.
func runV3NetCentralAlerts(c *cmdCtx) (string, error) {
	e, s, terminal, outputMode := c.e, c.s, c.terminal, c.outputMode
	termWidth := c.termWidth
	u := c.currentUser
	if u == nil {
		return "", nil
	}
	board := strings.TrimSpace(e.GetServerConfig().BoardName)
	if board == "" {
		board = "this board"
	}
	kinds := protocol.CentralAlertKinds
	status := ""
	for {
		if c.userManager != nil {
			if stored, ok := c.userManager.GetUserByID(u.ID); ok {
				centralConsentFrom(u, stored)
			}
		}
		offered := map[string][]string{}
		for _, k := range kinds {
			offered[k] = centralAlertNetworks(e.V3NetCentral, k)
		}
		// Each state fits four columns, so both columns fit 80.
		unoffered := false
		state := func(on bool, kind string) string {
			switch {
			case len(offered[kind]) == 0 && on:
				unoffered = true
				return "|10On|08*|07"
			case len(offered[kind]) == 0:
				unoffered = true
				return "|08n/a|07"
			case on:
				return "|10On|07"
			default:
				return "|07Off|07"
			}
		}

		var buf strings.Builder
		buf.WriteString(centralHeader("Activity Alerts", termWidth))
		buf.Write(ansi.ReplacePipeCodes([]byte("|07  Alerts are short live notices of what callers do on other boards.\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  Sharing and seeing are separate; each switch starts off.|07\r\n\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  Tell other boards when...     Show me when, on other boards...|07\r\n")))
		for i, k := range kinds {
			left := fmt.Sprintf("|03  [|15%d|03] %-20s", i+1, centralAlertLabels[k][0])
			right := fmt.Sprintf("|03[|15%d|03] %-22s", i+5, centralAlertLabels[k][1])
			// Each column keeps its width whatever the state's colours.
			buf.Write(ansi.ReplacePipeCodes([]byte(left + " " + padPipes(state(u.CentralAlerts.Shares(k), k), 4) + "  " + right + " " + state(u.CentralAlerts.Sees(k), k) + "\r\n")))
		}
		if unoffered {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  n/a: not offered on this board.  On*: on, but not offered now.|07\r\n")))
		}

		handle := v3netClip(centralPreviewHandle(u.Handle), 30)
		b := v3netClip(board, 30)
		buf.Write(ansi.ReplacePipeCodes([]byte("\r\n|03  What other boards would see|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|07  |15" + handle + "|07 on |11" + b + "|07 logged on, logged off,\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|07  entered network chat |08(the room)|07, or posted in |08(the area)|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  Never a message, its subject, private mail or a private chat message.|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  Turning a switch off stops the next alert at once; one already sent|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  lasts two minutes at most. Alerts are live only, never saved for later.|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		line, next, err := v3netPromptLine(s, terminal, outputMode, "|07  Choice (1-8, Q quits): ")
		if err != nil {
			return next, err
		}
		line = strings.ToUpper(strings.TrimSpace(line))
		if line == "" || line == "Q" {
			return "", nil
		}
		var n int
		if _, err := fmt.Sscanf(line, "%d", &n); err != nil || n < 1 || n > 2*len(kinds) || fmt.Sprint(n) != line {
			status = "|04Enter 1 to 8, or Q.|07"
			continue
		}
		share := n <= len(kinds)
		kind := kinds[(n-1)%len(kinds)]
		on := u.CentralAlerts.Sees(kind)
		if share {
			on = u.CentralAlerts.Shares(kind)
		}
		// Turning off is always allowed; turning on only where offered.
		if !on && len(offered[kind]) == 0 {
			status = "|04Your SysOp does not offer that alert.|07"
			continue
		}
		mutate := func(x *user.User) {
			if share {
				x.CentralAlerts.SetShares(kind, !x.CentralAlerts.Shares(kind))
			} else {
				x.CentralAlerts.SetSees(kind, !x.CentralAlerts.Sees(kind))
			}
		}
		if c.userManager != nil {
			saved, err := c.userManager.SetCentralConsent(u.ID, mutate)
			if err != nil {
				slog.Error("failed to save V3Net Central alert preference", "node", c.nodeNumber, "error", err)
				status = "|04Could not save that; nothing changed.|07"
				continue
			}
			centralConsentFrom(u, saved)
		} else {
			mutate(u)
		}
		status = "|10Saved.|07"
	}
}

// padPipes pads s, pipe-coded text, with spaces to width visible
// characters.
func padPipes(s string, width int) string {
	visible := len([]rune(ansi.StripAnsi(string(ansi.ReplacePipeCodes([]byte(s))))))
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}
