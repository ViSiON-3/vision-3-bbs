package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central phase 2: network Who's Online and Last Callers, and the
// caller's own switches for them. Viewing either feed sends nothing about
// the viewer; only the switches on My Preferences do, each off until the
// caller turns it on, and only on networks where the SysOp offers the
// feature.

// centralActivityNetworks returns the networks where kind can be offered
// now: Central and the feature enabled by the SysOp, and served by the hub.
func centralActivityNetworks(p V3NetCentralProvider, kind string) []CentralNetworkInfo {
	if p == nil {
		return nil
	}
	var out []CentralNetworkInfo
	for _, n := range p.CentralNetworks() {
		if a := n.Activity(kind); n.Enabled && a.Enabled && a.Supported {
			out = append(out, n)
		}
	}
	return out
}

// centralOfferedNetworks returns the names of the networks where the SysOp
// enabled kind, served by the hub or not yet known to be.
func centralOfferedNetworks(p V3NetCentralProvider, kind string) (names []string) {
	if p == nil {
		return nil
	}
	for _, n := range p.CentralNetworks() {
		if a := n.Activity(kind); n.Enabled && a.Enabled && (a.Supported || !n.SupportKnown) {
			names = append(names, n.Network)
		}
	}
	return names
}

// centralSharedNetworks returns the networks offering kind where u is
// shared: by their own opt-in, or, until they choose, by the network's
// Callers Default.
func centralSharedNetworks(p V3NetCentralProvider, kind string, u *user.User) (names []string) {
	if p == nil {
		return nil
	}
	for _, n := range p.CentralNetworks() {
		a := n.Activity(kind)
		if !n.Enabled || !a.Enabled || (!a.Supported && n.SupportKnown) {
			continue
		}
		var on bool
		if kind == protocol.CentralKindPresence {
			on = u.CentralPresenceFor(a.CallersDefault)
		} else {
			_, on = u.CentralLastCallsFrom(a.CallersDefault)
		}
		if on {
			names = append(names, n.Network)
		}
	}
	return names
}

// centralSharingOn reports whether a Sharing switch is on for u, which is
// what pressing it turns off: shared on some offering network, or, when no
// network offers kind now, the caller's own stored opt-in, so a caller can
// still withdraw one the SysOp has since stopped offering.
func centralSharingOn(p V3NetCentralProvider, kind string, u *user.User) bool {
	if len(centralOfferedNetworks(p, kind)) == 0 {
		if kind == protocol.CentralKindPresence {
			return u.CentralPresence
		}
		return u.CentralLastCallsSince != nil
	}
	return len(centralSharedNetworks(p, kind, u)) > 0
}

// fetchCentralActivity fetches kind from each of networks, returning the
// boards per network and one display error per network that failed.
func fetchCentralActivity(p V3NetCentralProvider, networks []string, kind string, force bool) (map[string][]protocol.CentralActivityBoard, []string) {
	ctx, cancel := context.WithTimeout(context.Background(), centralFetchTimeout)
	defer cancel()
	out := make(map[string][]protocol.CentralActivityBoard, len(networks))
	var errs []string
	for _, net := range networks {
		boards, err := p.CentralActivity(ctx, net, kind, force)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", net, err))
			continue
		}
		out[net] = boards
	}
	return out, errs
}

// centralWhoRow is one caller on network Who's Online: a handle on a board,
// with how many sessions they have there.
type centralWhoRow struct {
	network, board, handle, availability string
	sessions                             int
}

// centralWhoRows groups the presence feeds into one row per caller per
// board, sorted by board then handle. Sessions are counted, not listed: a
// caller on two nodes shows once.
func centralWhoRows(feeds map[string][]protocol.CentralActivityBoard) []centralWhoRow {
	idx := make(map[string]int)
	var rows []centralWhoRow
	for net, boards := range feeds {
		for _, b := range boards {
			for _, e := range b.Activity.Entries {
				key := net + "\x00" + b.Activity.NodeID + "\x00" + strings.ToLower(e.Handle)
				if i, ok := idx[key]; ok {
					rows[i].sessions++
					if rows[i].availability == "" {
						rows[i].availability = e.Availability
					}
					continue
				}
				idx[key] = len(rows)
				rows = append(rows, centralWhoRow{network: net, board: b.Activity.BBS, handle: e.Handle, availability: e.Availability, sessions: 1})
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		bi, bj := strings.ToLower(rows[i].board), strings.ToLower(rows[j].board)
		if bi != bj {
			return bi < bj
		}
		if rows[i].network != rows[j].network {
			return rows[i].network < rows[j].network
		}
		return strings.ToLower(rows[i].handle) < strings.ToLower(rows[j].handle)
	})
	return rows
}

// runV3NetCentralWho is network Who's Online: the callers on participating
// boards who chose to be shown, with their board and availability. args
// names one network; blank covers every one where it is available.
func runV3NetCentralWho(c *cmdCtx, args string) (*user.User, string, error) {
	e, s, terminal, outputMode := c.e, c.s, c.terminal, c.outputMode
	termWidth, termHeight := c.termWidth, c.termHeight
	if c.currentUser == nil {
		return nil, "", nil
	}
	showMessage := func(msg string) (*user.User, string, error) {
		terminalio.WriteProcessedBytes(terminal, []byte(centralHeader("Who's Online", termWidth)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("  "+msg+"\r\n")), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}
	if e.V3NetCentral == nil {
		return showMessage("|08V3Net is not enabled on this system.|07")
	}
	var networks []string
	for _, n := range centralActivityNetworks(e.V3NetCentral, protocol.CentralKindPresence) {
		if args == "" || strings.EqualFold(strings.TrimSpace(args), n.Network) {
			networks = append(networks, n.Network)
		}
	}
	if len(networks) == 0 {
		return showMessage("|08Network Who's Online is not available on any network.|07")
	}

	terminalio.WriteProcessedBytes(terminal, []byte(centralHeader("Who's Online", termWidth)+string(ansi.ReplacePipeCodes([]byte("|08  Fetching...|07\r\n")))), outputMode)
	feeds, errs := fetchCentralActivity(e.V3NetCentral, networks, protocol.CentralKindPresence, false)
	rows := centralWhoRows(feeds)

	if tpl, ok := e.loadCentralTemplate("WHONET"); ok {
		// Footer: "...and N more", the errors, the pause prompt and the
		// break it may start with.
		shown := rows
		if fit := tpl.rowsThatFit(termHeight, 3+len(errs)); len(shown) > fit {
			shown = shown[:fit]
		}
		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.WriteString(renderWhoTemplate(tpl, shown, len(rows), networks))
		if len(rows) == 0 && len(errs) == 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  Nobody who chose to be shown is online right now.|07\r\n")))
		}
		if len(rows) > len(shown) {
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|08  ...and %d more.|07\r\n", len(rows)-len(shown)))))
		}
		for _, msg := range errs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + v3netClip(msg, termWidth-4) + "|07\r\n")))
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}

	multi := len(networks) > 1
	handleW, boardW, netW := 22, 26, 0
	if multi {
		netW = 12
	}
	var buf strings.Builder
	buf.WriteString(centralHeader("Who's Online", termWidth))
	hdr := fmt.Sprintf("|03  %-*s %-*s ", handleW, "Caller", boardW, "Board")
	if multi {
		hdr += fmt.Sprintf("%-*s ", netW, "Network")
	}
	buf.Write(ansi.ReplacePipeCodes([]byte(hdr + "Availability|07\r\n")))
	// Header 3 rows, column header 1, footer (errors, note, pause) about 4.
	fit := max(termHeight-8-len(errs), 1)
	shown := rows
	if len(shown) > fit {
		shown = shown[:fit]
	}
	for _, r := range shown {
		avail := centralAvailabilityText(r.availability)
		if r.sessions > 1 {
			avail += fmt.Sprintf(" |08(%d nodes)|07", r.sessions)
		}
		line := "  |15" + v3netCell(r.handle, handleW) + "|07 |11" + v3netCell(r.board, boardW) + "|07 "
		if multi {
			line += "|08" + v3netCell(r.network, netW) + "|07 "
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(line + avail + "\r\n")))
	}
	switch {
	case len(rows) == 0 && len(errs) == 0:
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  Nobody who chose to be shown is online right now.|07\r\n")))
	case len(rows) > len(shown):
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|08  ...and %d more.|07\r\n", len(rows)-len(shown)))))
	}
	for _, msg := range errs {
		buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + v3netClip(msg, termWidth-4) + "|07\r\n")))
	}
	buf.Write(ansi.ReplacePipeCodes([]byte("|08  Only callers who turned on Who's Online are shown.|07\r\n")))
	terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
	return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
}

// centralCallRow is one call on the Last Callers screen: a call to this
// board (local) or one a board on a network published.
type centralCallRow struct {
	local   bool
	network string
	// location is where the call came from: the board's name for a
	// network call, the caller's location field for a local one.
	location     string
	handle       string
	began, ended time.Time
}

// lastCallersMode is what a Last Callers screen covers.
type lastCallersMode int

const (
	lastCallersLocal   lastCallersMode = iota // this board only (the LASTCALL templates)
	lastCallersNetwork                        // V3Net boards only
	lastCallersAll                            // both, each row flagged
)

// parseLastCallersArgs reads LASTCALLERS' arguments: an optional count,
// and --network (V3Net calls only) or --all (local and V3Net together).
func parseLastCallersArgs(args string) (limit int, mode lastCallersMode) {
	limit = defaultLastCallerRows
	for _, f := range strings.Fields(args) {
		switch strings.ToLower(f) {
		case "--network", "--net", "--v3net", "-n":
			mode = lastCallersNetwork
		case "--all", "-a":
			mode = lastCallersAll
		default:
			if n, err := strconv.Atoi(f); err == nil && n > 0 {
				limit = n
			}
		}
	}
	return limit, mode
}

// centralLastCallsNetworks names the networks whose Last Callers this
// board offers and the hub serves; none when V3Net is off.
func centralLastCallsNetworks(e *MenuExecutor) []string {
	if e.V3NetCentral == nil {
		return nil
	}
	var networks []string
	for _, n := range centralActivityNetworks(e.V3NetCentral, protocol.CentralKindLastCallers) {
		networks = append(networks, n.Network)
	}
	return networks
}

// runV3NetCentralLastCallers is network Last Callers as its own command:
// args is an optional count and --all to include this board's callers.
func runV3NetCentralLastCallers(c *cmdCtx, args string) (*user.User, string, error) {
	limit, mode := parseLastCallersArgs(args)
	return runCentralLastCallers(c, limit, mode == lastCallersAll)
}

// centralCallRows builds the rows of the Last Callers screen: the calls
// published on the networks (leaving out this board's own snapshot when
// its local calls are shown, since those are the same calls), plus this
// board's visible calls when includeLocal is set. The most recent limit
// are kept, oldest first, as the local list shows them.
func centralCallRows(feeds map[string][]protocol.CentralActivityBoard, selfNode string, local []user.CallRecord, includeLocal bool, limit int) []centralCallRow {
	var rows []centralCallRow
	now := time.Now()
	for net, boards := range feeds {
		for _, b := range boards {
			if includeLocal && selfNode != "" && b.Activity.NodeID == selfNode {
				continue
			}
			for _, e := range b.Activity.Entries {
				// The screen redraws from one fetch; a call that passes the
				// retention limit meanwhile is not drawn again.
				if now.Sub(e.Began()) > protocol.MaxCentralLastCallRetention {
					continue
				}
				rows = append(rows, centralCallRow{network: net, location: b.Activity.BBS, handle: e.Handle, began: e.Began(), ended: e.Ended()})
			}
		}
	}
	if includeLocal {
		for _, r := range local {
			rows = append(rows, centralCallRow{local: true, location: strings.TrimSpace(r.GroupLocation), handle: r.Handle, began: r.ConnectTime, ended: r.DisconnectTime})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].began.Before(rows[j].began) })
	if limit > 0 && len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows
}

// runCentralLastCallers is the Last Callers screen for V3Net: calls
// published by participating boards, and with includeLocal this board's
// own callers too, each row flagged with where the call was. A toggles
// this board's callers in and out, R fetches again, any other key leaves.
func runCentralLastCallers(c *cmdCtx, limit int, includeLocal bool) (*user.User, string, error) {
	e, s, terminal, outputMode := c.e, c.s, c.terminal, c.outputMode
	termWidth, termHeight := c.termWidth, c.termHeight
	if c.currentUser == nil {
		return nil, "", nil
	}
	title := "Last Callers"
	networks := centralLastCallsNetworks(e)
	if len(networks) == 0 && !includeLocal {
		msg := "|08Network Last Callers is not available on any network.|07"
		if e.V3NetCentral == nil {
			msg = "|08V3Net is not enabled on this system.|07"
		}
		terminalio.WriteProcessedBytes(terminal, []byte(centralHeader(title, termWidth)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("  "+msg+"\r\n")), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}
	selfNode := ""
	if e.V3NetCentral != nil {
		selfNode = e.V3NetCentral.NodeID()
	}
	timeLoc := getLastCallerTimeLocation(strings.TrimSpace(e.GetServerConfig().Timezone))

	terminalio.WriteProcessedBytes(terminal, []byte(centralHeader(title, termWidth)+string(ansi.ReplacePipeCodes([]byte("|08  Fetching...|07\r\n")))), outputMode)
	var feeds map[string][]protocol.CentralActivityBoard
	var errs []string
	fetch := func(force bool) {
		if len(networks) > 0 {
			feeds, errs = fetchCentralActivity(e.V3NetCentral, networks, protocol.CentralKindLastCallers, force)
		}
	}
	fetch(false)

	ih := getSessionIH(s)
	handleW, boardW, srcW := 20, 24, 10
	tpl, haveTpl := e.loadCentralTemplate("LASTNET")
	for {
		var local []user.CallRecord
		if includeLocal && c.userManager != nil {
			local = visibleCallRecords(c.userManager.GetLastCallers())
		}
		var buf strings.Builder
		var rows []centralCallRow
		if haveTpl {
			// Footer: the errors, the rule, and the key help the cursor is
			// left on.
			fit := tpl.rowsThatFit(termHeight, 2+len(errs))
			rows = centralCallRows(feeds, selfNode, local, includeLocal, min(limit, fit))
			buf.WriteString(ansi.ClearScreen())
			buf.WriteString(renderLastCallsTemplate(tpl, rows, networks, includeLocal, timeLoc))
		} else {
			// Title 2 rows, legend 1, column header 1, footer 2, plus errors.
			fit := max(termHeight-6-len(errs), 1)
			rows = centralCallRows(feeds, selfNode, local, includeLocal, min(limit, fit))
			buf.WriteString(centralHeader(title, termWidth))
			writeBuiltinCallRows(&buf, rows, includeLocal, timeLoc, handleW, boardW, srcW)
		}
		if len(rows) == 0 && len(errs) == 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  No calls to show.|07\r\n")))
		}
		for _, msg := range errs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + v3netClip(msg, termWidth-4) + "|07\r\n")))
		}
		toggle := "Show this board's callers too"
		if includeLocal {
			toggle = "Hide this board's callers"
		}
		// With no network to show, there is nothing to toggle or refresh:
		// only the way out is offered.
		footer := "|08 "
		if len(networks) > 0 {
			footer += " [|15A|08] " + toggle + "  [|15R|08] Refresh "
		}
		footer += " [|15any key|08] Continue|07"
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		buf.WriteString(clipColumns(string(ansi.ReplacePipeCodes([]byte(footer))), termWidth-1, outputMode))
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, editor.ErrIdleTimeout) {
				return nil, "LOGOFF", editor.ErrIdleTimeout
			}
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}
		switch key {
		case 'a', 'A':
			if len(networks) == 0 {
				// Not offered: nothing but this board's callers to show.
				return nil, "", nil
			}
			includeLocal = !includeLocal
		case 'r', 'R':
			if len(networks) == 0 {
				return nil, "", nil
			}
			fetch(true)
		default:
			return nil, "", nil
		}
	}
}

// runV3NetCentralPrefs is where the caller decides what V3Net Central
// shares about them: their presence on network Who's Online (and an
// availability beside it), and their calls on network Last Callers. Each
// switch is off until the caller turns it on, is offered only where the
// SysOp enabled the feature, and shows exactly what other boards would
// see before it is turned on.
func runV3NetCentralPrefs(c *cmdCtx, args string) (*user.User, string, error) {
	e, s, terminal, outputMode := c.e, c.s, c.terminal, c.outputMode
	termWidth := c.termWidth
	u := c.currentUser
	if u == nil {
		return nil, "", nil
	}
	board := strings.TrimSpace(e.GetServerConfig().BoardName)
	if board == "" {
		board = "this board"
	}
	status := ""
	for {
		// Show the stored consent: this session's copy may be older than
		// a change made on another node.
		if c.userManager != nil {
			if stored, ok := c.userManager.GetUserByID(u.ID); ok {
				centralConsentFrom(u, stored)
			}
		}
		whoNets := centralOfferedNetworks(e.V3NetCentral, protocol.CentralKindPresence)
		lastNets := centralOfferedNetworks(e.V3NetCentral, protocol.CentralKindLastCallers)

		chatNets := centralChatOfferedNetworks(e.V3NetCentral)

		whoOn := centralSharedNetworks(e.V3NetCentral, protocol.CentralKindPresence, u)
		lastOn := centralSharedNetworks(e.V3NetCentral, protocol.CentralKindLastCallers, u)
		// Chat is shared only where it is offered and the caller agreed;
		// the switch still shows On, with no network, for an agreement
		// the caller can withdraw after the SysOp stopped offering it.
		var chatOn []string
		if u.CentralChat && !u.CentralChatBarred {
			chatOn = chatNets
		}

		// Activity alerts have a screen of their own, offered here where
		// any network has them on, or while the caller has one on.
		alertsShared := centralAlertsShared(e.V3NetCentral, u)
		alertsEntry := centralAlertsOffered(e.V3NetCentral) || u.CentralAlerts != (user.CentralAlertPrefs{})

		var buf strings.Builder
		buf.WriteString(centralHeader("Sharing", termWidth))
		if len(whoOn) == 0 && len(lastOn) == 0 && len(chatOn) == 0 && !alertsShared {
			buf.Write(ansi.ReplacePipeCodes([]byte("|15  Nothing about you is shared through V3Net Central.|07\r\n")))
		} else {
			buf.Write(ansi.ReplacePipeCodes([]byte("|15  You share what is switched on below, and nothing else.|07\r\n")))
		}
		// No blank line under this: with every feature offered, the screen
		// takes all 24 rows.
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  Browsing boards, Who's Online and Last Callers sends nothing about you.|07\r\n")))

		onOff := func(on, offered []string) string {
			switch {
			case len(offered) == 0:
				return "|08not offered here|07"
			case len(on) > 0:
				return "|10On |07 |08(" + v3netClip(strings.Join(on, ", "), 30) + ")|07"
			default:
				return "|07Off|07"
			}
		}
		availText := map[string]string{"": "|07Not shown|07", protocol.CentralAvailable: "|10Available|07", protocol.CentralBusy: "|14Busy|07"}[u.CentralAvailability]
		if availText == "" {
			availText = "|07Not shown|07"
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|151|03] Show me on Who's Online        " + onOff(whoOn, whoNets) + "\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|152|03] My availability               " + availText + "\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|153|03] Show my calls on Last Callers " + onOff(lastOn, lastNets) + "\r\n")))
		chatSwitch := onOff(chatOn, chatNets)
		if len(chatNets) == 0 && u.CentralChat {
			chatSwitch = "|10On |07 |08(not offered here now)|07"
		}
		if u.CentralChatBarred {
			chatSwitch = "|07Off |08(set by the SysOp)|07"
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|154|03] Take part in network chat     " + chatSwitch + "\r\n")))
		if alertsEntry {
			buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|155|03] Activity alerts               " + centralAlertsSummary(u) + "\r\n")))
		}
		buf.WriteString("\r\n")

		handle := centralPreviewHandle(u.Handle)
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  What other boards would see|07\r\n")))
		alertsOffered := centralAlertsOffered(e.V3NetCentral)
		switch {
		case len(whoNets) == 0 && len(lastNets) == 0 && len(chatNets) == 0 && !alertsOffered:
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  Nothing: this board offers no feature that shares anything about you.\r\n")))
		case len(whoNets) == 0 && len(lastNets) == 0 && len(chatNets) == 0:
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  Only activity alerts you share: see [5] for what they say.\r\n")))
		}
		// Each preview is two lines: who and where, then what else goes
		// with it, so a long handle and board name still fit 80 columns.
		if len(whoNets) > 0 {
			detail := "while you are on"
			if u.CentralAvailability != "" {
				detail = "shown as " + u.CentralAvailability + ", while you are on"
			}
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  Who's Online: |15" + v3netClip(handle, 30) + "|07 on |11" + v3netClip(board, 30) + "|07\r\n")))
			buf.Write(ansi.ReplacePipeCodes([]byte("|08                " + detail + "|07\r\n")))
		}
		if len(lastNets) > 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  Last Callers: |15" + v3netClip(handle, 30) + "|07 on |11" + v3netClip(board, 30) + "|07\r\n")))
			if len(lastOn) > 0 && u.CentralLastCallsSince == nil {
				// Counted in by the network's default: no start of
				// their own, so the feed's whole 24 hours goes.
				buf.Write(ansi.ReplacePipeCodes([]byte("|08                when each call began and ended; your calls from the|07\r\n")))
				buf.Write(ansi.ReplacePipeCodes([]byte("|08                last 24 hours, each kept 24 hours|07\r\n")))
			} else {
				buf.Write(ansi.ReplacePipeCodes([]byte("|08                when each call began and ended; only calls from when you|07\r\n")))
				buf.Write(ansi.ReplacePipeCodes([]byte("|08                turn it on, each kept 24 hours|07\r\n")))
			}
		}
		if len(chatNets) > 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  Network chat: |15" + v3netClip(handle, 30) + "|07 on |11" + v3netClip(board, 30) + "|07\r\n")))
			buf.Write(ansi.ReplacePipeCodes([]byte("|08                in the user list and each room message; kept in room history|07\r\n")))
		}
		if alertsOffered {
			// Alerts say what a caller does when shared, as chat's joins,
			// leaves and lines do: whatever is switched on above.
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  Never your real name, location or user number; activity only as switched on.|07\r\n")))
		} else {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  Never your real name, location, user number, or what you are doing.|07\r\n")))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  Turning a switch off takes you off at once on connected boards; an|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  offline board drops it within minutes (online) or 24 hours (calls).|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		prompt := "|07  Choice (1-4, Q quits): "
		if alertsEntry {
			prompt = "|07  Choice (1-5, Q quits): "
		}
		line, next, err := v3netPromptLine(s, terminal, outputMode, prompt)
		if err != nil {
			return u, next, err
		}
		// Each change is made to the stored account, from its stored
		// values, not this session's copy: another node may have changed
		// them since this call began.
		var mutate func(x *user.User)
		switch strings.ToUpper(line) {
		case "", "Q":
			return u, "", nil
		case "1":
			if len(whoNets) == 0 && !u.CentralPresence {
				status = "|04Your SysOp does not offer network Who's Online.|07"
				continue
			}
			// The switch flips what the caller sees, which may be the
			// network's default; from then on it is their own choice.
			mutate = func(x *user.User) {
				x.CentralPresence = !centralSharingOn(e.V3NetCentral, protocol.CentralKindPresence, x)
				x.CentralPresenceChosen = true
			}
		case "2":
			mutate = func(x *user.User) {
				x.CentralAvailability = map[string]string{"": protocol.CentralAvailable, protocol.CentralAvailable: protocol.CentralBusy}[x.CentralAvailability]
			}
		case "3":
			if len(lastNets) == 0 && u.CentralLastCallsSince == nil {
				status = "|04Your SysOp does not offer network Last Callers.|07"
				continue
			}
			mutate = func(x *user.User) {
				// Read what the caller shares before recording the
				// choice: once it is theirs, the default no longer counts.
				on := centralSharingOn(e.V3NetCentral, protocol.CentralKindLastCallers, x)
				x.CentralLastCallsChosen = true
				if on {
					x.CentralLastCallsSince = nil
					return
				}
				now := time.Now().UTC()
				x.CentralLastCallsSince = &now
			}
		case "4":
			if u.CentralChatBarred {
				status = "|04Your SysOp has turned network chat off for you.|07"
				continue
			}
			if u.CentralChat {
				// Withdrawing needs no notice; the next line the caller
				// types in a network room ends their chat.
				mutate = func(x *user.User) {
					x.CentralChat = false
					x.CentralChatChosen = true
				}
				break
			}
			if len(chatNets) == 0 {
				status = "|04Your SysOp does not offer network chat.|07"
				continue
			}
			// Turning it on is agreeing to the notice, which saves itself.
			agreed, next, err := chatConsentNotice(c)
			if err != nil {
				return u, next, err
			}
			if !agreed {
				status = "|07Network chat stays off.|07"
				continue
			}
			status = "|10Saved.|07"
			continue
		case "5":
			if !alertsEntry {
				status = "|04Enter 1, 2, 3, 4 or Q.|07"
				continue
			}
			if next, err := runV3NetCentralAlerts(c); err != nil {
				return u, next, err
			}
			continue
		default:
			status = "|04Enter 1, 2, 3, 4 or Q.|07"
			if alertsEntry {
				status = "|04Enter 1 to 5, or Q.|07"
			}
			continue
		}
		if c.userManager != nil {
			saved, err := c.userManager.SetCentralConsent(u.ID, mutate)
			if err != nil {
				slog.Error("failed to save V3Net Central preference", "node", c.nodeNumber, "error", err)
				status = "|04Could not save that; nothing changed.|07"
				continue
			}
			centralConsentFrom(u, saved)
		} else {
			mutate(u)
		}
		if e.V3NetCentral != nil {
			e.V3NetCentral.NotifyCentralActivity()
		}
		status = "|10Saved.|07"
	}
}

// centralConsentFrom copies src's V3Net Central consent into dst.
func centralConsentFrom(dst, src *user.User) {
	dst.CentralPresence = src.CentralPresence
	dst.CentralAvailability = src.CentralAvailability
	dst.CentralLastCallsSince = src.CentralLastCallsSince
	dst.CentralPresenceChosen = src.CentralPresenceChosen
	dst.CentralLastCallsChosen = src.CentralLastCallsChosen
	dst.CentralChat = src.CentralChat
	dst.CentralChatChosen = src.CentralChatChosen
	dst.CentralChatBarred = src.CentralChatBarred
	dst.CentralAlerts = src.CentralAlerts
}

// centralPreviewHandle shows a handle as a snapshot would carry it: colour
// codes and control characters out, cut to the protocol's limit.
func centralPreviewHandle(h string) string {
	return protocol.CleanCentralText(centralPreviewPipeRe.ReplaceAllString(h, ""), protocol.MaxCentralHandleLen)
}

// centralPreviewPipeRe matches a handle's colour codes.
var centralPreviewPipeRe = regexp.MustCompile(`\|[0-9]{2}`)

// writeBuiltinCallRows draws the built-in Last Callers legend, column
// header and rows, for a menu set without the LASTNET templates.
func writeBuiltinCallRows(buf *strings.Builder, rows []centralCallRow, includeLocal bool, timeLoc *time.Location, handleW, boardW, srcW int) {
	legend := "|08  Calls shared by callers on V3Net boards, kept 24 hours"
	if includeLocal {
		legend = "|08  |14local|08 = this board,  |13network name|08 = shared from a V3Net board"
	}
	buf.Write(ansi.ReplacePipeCodes([]byte(legend + "|07\r\n")))
	buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|03  %-*s %-*s %-*s %-12s %s|07\r\n", srcW, "From", handleW, "Caller", boardW, "Location", "On", "Mins"))))
	for _, r := range rows {
		src := "|13" + v3netCell(r.network, srcW) + "|07"
		if r.local {
			src = "|14" + v3netCell("local", srcW) + "|07"
		}
		on := ""
		if !r.began.IsZero() {
			on = r.began.In(timeLoc).Format("Mon 03:04pm")
		}
		mins := "|08-|07"
		if !r.ended.IsZero() && !r.ended.Before(r.began) {
			mins = strconv.Itoa(int(r.ended.Sub(r.began).Minutes()))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("  " + src + " |15" + v3netCell(r.handle, handleW) + "|07 |11" +
			v3netCell(r.location, boardW) + "|07 " + fmt.Sprintf("%-12s ", on) + mins + "\r\n")))
	}
}
