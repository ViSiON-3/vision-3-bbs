package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central is the caller-facing side of V3Net: a shared place to find
// the boards on a network. Phase 1 is the board directory. Every screen
// here shows what other boards published and what this board's sysop
// chose; none of them sends anything about the caller anywhere.

// CentralNetworkInfo is one subscription's V3Net Central state as the
// screens need it. The service fills it from the leaf (see
// v3net.Service.CentralNetworks).
type CentralNetworkInfo struct {
	Network string
	HubURL  string
	// Enabled is the sysop's participation switch; Publishing the listing
	// switch.
	Enabled    bool
	Publishing bool
	// SupportKnown is whether the hub's feature list has been read, and
	// Supported whether it names the directory.
	SupportKnown bool
	Supported    bool
	// Connected is whether the event stream to the hub is open.
	Connected bool
	// LastPublished is when the listing last reached the hub (zero if not
	// this run); Withdrawn whether this run withdrew it; LastError the
	// latest publish or withdrawal failure.
	LastPublished time.Time
	Withdrawn     bool
	LastError     string
	// DirectoryFetched is when the directory was last fetched (zero if
	// never) and DirectoryCount how many boards it held.
	DirectoryFetched time.Time
	DirectoryCount   int
	// Presence and LastCallers are network Who's Online and Last Callers
	// on this network.
	Presence    CentralActivityInfo
	LastCallers CentralActivityInfo
	// Chat is network chat on this network.
	Chat CentralChatInfo
}

// CentralChatInfo is network chat's state on a network.
type CentralChatInfo struct {
	// Enabled is the sysop's switch for chat (which needs Central on);
	// Supported whether the hub serves it.
	Enabled   bool
	Supported bool
}

// ChatOffered reports whether the sysop offers network chat on this
// network.
func (n CentralNetworkInfo) ChatOffered() bool { return n.Enabled && n.Chat.Enabled }

// ChatAvailable reports whether callers can use network chat on this
// network now: offered, and served by the hub.
func (n CentralNetworkInfo) ChatAvailable() bool { return n.ChatOffered() && n.Chat.Supported }

// CentralActivityInfo is one activity feature's state on a network.
type CentralActivityInfo struct {
	// Enabled is the sysop's switch for the feature (which needs Central
	// on); Supported whether the hub serves it.
	Enabled   bool
	Supported bool
	// CallersDefault is whether callers who have not chosen count as
	// opted in on this network.
	CallersDefault bool
	// LastPublished is when this board's snapshot last reached the hub
	// (zero if not this run) and LastCount how many entries it held;
	// LastError the latest publish or withdrawal failure.
	LastPublished time.Time
	LastCount     int
	LastError     string
}

// Activity returns the network's state of an activity kind.
func (n CentralNetworkInfo) Activity(kind string) CentralActivityInfo {
	if kind == protocol.CentralKindLastCallers {
		return n.LastCallers
	}
	return n.Presence
}

// Available reports whether callers can use the directory on this
// network: the sysop enabled Central and the hub serves it.
func (n CentralNetworkInfo) Available() bool { return n.Enabled && n.Supported }

// V3NetCentralProvider is what the Central screens need from the V3Net
// service. The concrete implementation is an adapter over *v3net.Service,
// injected from cmd/vision3.
type V3NetCentralProvider interface {
	// CentralNetworks returns every running subscription's Central state.
	CentralNetworks() []CentralNetworkInfo
	// CentralDirectory returns network's verified board directory; force
	// skips the leaf's short cache. It returns an error wrapping
	// leaf.ErrCentralDisabled or leaf.ErrCentralUnsupported when the
	// directory cannot be offered on the network.
	CentralDirectory(ctx context.Context, network string, force bool) ([]protocol.CentralDirectoryEntry, error)
	// CentralActivity returns network's verified feed of an activity kind
	// (protocol.CentralKindPresence or CentralKindLastCallers).
	CentralActivity(ctx context.Context, network, kind string, force bool) ([]protocol.CentralActivityBoard, error)
	// NotifyCentralActivity says a caller's sharing choices changed, so
	// the board republishes what it shares.
	NotifyCentralActivity()
	// NodeID is this board's node ID, to tell its own snapshots from
	// other boards'.
	NodeID() string
}

// centralRecentWindow separates a recently reachable board from a stale
// listing: a board the hub has seen inside it is "recent", one it has not
// is "stale". A listing past its own expiry is not served at all.
const centralRecentWindow = 7 * 24 * time.Hour

// centralFetchTimeout bounds one directory fetch from the Boards screen.
const centralFetchTimeout = 15 * time.Second

// centralBoard is one directory row on the Boards screen.
type centralBoard struct {
	network string
	entry   protocol.CentralDirectoryEntry
}

// freshness classifies the board as online, recent or stale at now.
func (b centralBoard) freshness(now time.Time) string {
	switch {
	case b.entry.Online:
		return "online"
	case !b.entry.LastSeen().IsZero() && now.Sub(b.entry.LastSeen()) <= centralRecentWindow:
		return "recent"
	default:
		return "stale"
	}
}

// matches reports whether the board's name, description or an address
// contains q, ignoring case. An empty q matches every board.
func (b centralBoard) matches(q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	l := &b.entry.Listing
	if strings.Contains(strings.ToLower(l.Name), q) || strings.Contains(strings.ToLower(l.Description), q) {
		return true
	}
	for _, a := range l.Addresses {
		if strings.Contains(strings.ToLower(a), q) {
			return true
		}
	}
	return false
}

// centralNetworks returns the provider's networks split into those the
// sysop enabled and, of those, the ones the hub serves the directory on.
func centralNetworks(p V3NetCentralProvider) (enabled, available []CentralNetworkInfo) {
	for _, n := range p.CentralNetworks() {
		if !n.Enabled {
			continue
		}
		enabled = append(enabled, n)
		if n.Supported {
			available = append(available, n)
		}
	}
	return enabled, available
}

// centralHeader draws a screen title with the rule under it.
func centralHeader(title string, width int) string {
	return ansi.ClearScreen() + string(ansi.ReplacePipeCodes([]byte("|12V3Net Central: "+title+"|07\r\n"+v3netRule(width))))
}

// runV3NetCentral is the V3Net Central menu: Boards, Who's Online, Last
// Callers, Chat, My Preferences and Connection Status. A feature the sysop
// has not enabled, or the hub does
// not serve, is left off the menu rather than shown refused. With V3Net
// off, or Central enabled on no network, it says so and returns.
func runV3NetCentral(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if c.currentUser == nil {
		return nil, "", nil
	}
	if e.V3NetCentral == nil {
		terminalio.WriteProcessedBytes(terminal, []byte(centralHeader("Menu", termWidth)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|08  V3Net is not enabled on this system.|07\r\n")), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}

	status := ""
	for {
		enabled, available := centralNetworks(e.V3NetCentral)
		if len(enabled) == 0 {
			terminalio.WriteProcessedBytes(terminal, []byte(centralHeader("Menu", termWidth)), outputMode)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|08  V3Net Central is not enabled on this system.|07\r\n")), outputMode)
			return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
		}

		var buf strings.Builder
		buf.WriteString(centralHeader("Menu", termWidth))
		names := make([]string, 0, len(enabled))
		for _, n := range enabled {
			names = append(names, n.Network)
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|08  Networks: |07%s|07\r\n\r\n", v3netClip(strings.Join(names, ", "), termWidth-14)))))
		if len(available) > 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15B|03]oards            |08find participating boards and how to call them|07\r\n")))
		}
		whoOK := len(centralActivityNetworks(e.V3NetCentral, protocol.CentralKindPresence)) > 0
		lastOK := len(centralActivityNetworks(e.V3NetCentral, protocol.CentralKindLastCallers)) > 0
		if whoOK {
			buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15W|03]ho's Online      |08callers on other boards who chose to be shown|07\r\n")))
		}
		if lastOK {
			buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15L|03]ast Callers      |08recent calls to other boards, shared by choice|07\r\n")))
		}
		chatOK := centralChatAvailable(e.V3NetCentral)
		if chatOK {
			buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15C|03]hat              |08talk with callers on other boards, if you choose|07\r\n")))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15P|03]references       |08what, if anything, is shared about you|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15S|03]tatus            |08connection to each network's hub|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15Q|03]uit|07\r\n\r\n")))
		for _, n := range enabled {
			if n.Supported {
				continue
			}
			why := "the hub does not serve the board directory"
			if !n.SupportKnown {
				why = "waiting for the hub"
			}
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|08  %s: boards not available (%s)|07\r\n", v3netClip(n.Network, 24), why))))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		line, next, err := v3netPromptLine(s, terminal, outputMode, "|07  Choice: ")
		if err != nil {
			return nil, next, err
		}
		switch strings.ToUpper(line) {
		case "", "Q":
			return nil, "", nil
		case "B":
			if len(available) == 0 {
				status = "|04Boards are not available on any network right now.|07"
				continue
			}
			if _, next, err := runV3NetCentralBoards(c, ""); err != nil {
				return nil, next, err
			}
		case "W":
			if !whoOK {
				status = "|04Who's Online is not available on any network.|07"
				continue
			}
			if _, next, err := runV3NetCentralWho(c, ""); err != nil {
				return nil, next, err
			}
		case "L":
			if !lastOK {
				status = "|04Last Callers is not available on any network.|07"
				continue
			}
			if _, next, err := runCentralLastCallers(c, defaultLastCallerRows, false); err != nil {
				return nil, next, err
			}
		case "C":
			if !chatOK {
				status = "|04Chat is not available on any network.|07"
				continue
			}
			if _, next, err := runChat(c, "NET"); err != nil || next == "LOGOFF" {
				return nil, next, err
			}
		case "P":
			if _, next, err := runV3NetCentralPrefs(c, ""); err != nil {
				return nil, next, err
			}
		case "S":
			if _, next, err := runV3NetCentralStatus(c, ""); err != nil {
				return nil, next, err
			}
		default:
			status = "|04Enter a letter from the menu, or Q.|07"
		}
	}
}

// runV3NetCentralStatus shows, per subscribed network, the sysop's Central
// switches, whether the hub serves the directory, whether the event stream
// is up, the listing's state and when the directory was last fetched.
func runV3NetCentralStatus(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if c.currentUser == nil {
		return nil, "", nil
	}
	var buf strings.Builder
	buf.WriteString(centralHeader("Connection Status", termWidth))
	if e.V3NetCentral == nil {
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  V3Net is not enabled on this system.|07\r\n")))
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}
	nets := e.V3NetCentral.CentralNetworks()
	if len(nets) == 0 {
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  No V3Net networks are subscribed.|07\r\n")))
	}
	now := time.Now()
	buf.Write(ansi.ReplacePipeCodes([]byte("|03  NETWORK       CENTRAL  DIRECTORY     STREAM     LISTING      BOARDS|07\r\n")))
	var errs []string
	for _, n := range nets {
		central := "|08off|07"
		if n.Enabled {
			central = "|10on|07 "
		}
		directory := "|08no directory|07"
		switch {
		case !n.SupportKnown:
			directory = "|14checking...|07 "
		case n.Supported:
			directory = "|10served|07      "
		}
		stream := "|04down|07     "
		if n.Connected {
			stream = "|10connected|07"
		}
		listing := "|08off|07         "
		switch {
		case n.Publishing && !n.LastPublished.IsZero():
			listing = "|10published|07   "
		case n.Publishing && n.LastError != "":
			listing = "|04failed|07      "
		case n.Publishing:
			listing = "|14pending|07     "
		case n.Withdrawn:
			listing = "|08withdrawn|07   "
		}
		boards := "|08-|07"
		if !n.DirectoryFetched.IsZero() {
			boards = fmt.Sprintf("|15%d|07 |08(%s)|07", n.DirectoryCount, v3netAgo(n.DirectoryFetched.UTC().Format(time.RFC3339), now))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("  |11%s|07 %s      %s %s  %s %s\r\n",
			v3netCell(n.Network, 13), central, directory, stream, listing, boards))))
		if n.LastError != "" {
			errs = append(errs, n.Network+": "+n.LastError)
		}
	}
	if len(nets) > 0 {
		buf.Write(ansi.ReplacePipeCodes([]byte("\r\n|03  NETWORK       WHO'S ONLINE              LAST CALLERS|07\r\n")))
		for _, n := range nets {
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("  |11%s|07 %s %s\r\n",
				v3netCell(n.Network, 13), centralActivityState(n, n.Presence, "online", now), centralActivityState(n, n.LastCallers, "calls", now)))))
			for _, a := range []CentralActivityInfo{n.Presence, n.LastCallers} {
				if a.LastError != "" {
					errs = append(errs, n.Network+": "+a.LastError)
				}
			}
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("\r\n|03  NETWORK       CHAT|07\r\n")))
		for _, n := range nets {
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("  |11%s|07 %s\r\n", v3netCell(n.Network, 13), centralChatState(n)))))
		}
	}
	for _, msg := range errs {
		buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + v3netClip(msg, termWidth-4) + "|07\r\n")))
	}
	terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
	return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
}

// centralActivityState is one Status cell for an activity feature: off,
// not served by the hub, or how many entries this board last shared and
// when. noun names the entries. The cell is 25 columns wide.
func centralActivityState(n CentralNetworkInfo, a CentralActivityInfo, noun string, now time.Time) string {
	var cell string
	switch {
	case !n.Enabled || !a.Enabled:
		cell = "|08off|07"
	case !n.SupportKnown:
		cell = "|14checking...|07"
	case !a.Supported:
		cell = "|08hub does not serve it|07"
	case a.LastError != "" && a.LastPublished.IsZero():
		cell = "|04failed|07"
	case a.LastPublished.IsZero():
		cell = "|14pending|07"
	default:
		cell = fmt.Sprintf("|10on|07, |15%d|07 %s |08(%s)|07", a.LastCount, noun, v3netAgo(a.LastPublished.UTC().Format(time.RFC3339), now))
	}
	plain := len([]rune(string(ansi.StripAnsi(string(ansi.ReplacePipeCodes([]byte(cell)))))))
	if plain < 25 {
		cell += strings.Repeat(" ", 25-plain)
	}
	return cell
}

// centralChatState is one Status cell for network chat: off, not served by
// the hub, or on.
func centralChatState(n CentralNetworkInfo) string {
	switch {
	case !n.ChatOffered():
		return "|08off|07"
	case !n.SupportKnown:
		return "|14checking...|07"
	case !n.Chat.Supported:
		return "|08hub does not serve it|07"
	default:
		return "|10on|07"
	}
}

// runV3NetCentralBoards is the board directory: a lightbar over every
// verified listing on the networks where the directory is available. Enter
// opens a board's details, / filters the list by a search text, R fetches
// the directory again and Q leaves. args names one network; blank covers
// every available one.
func runV3NetCentralBoards(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if c.currentUser == nil {
		return nil, "", nil
	}
	showMessage := func(msg string) (*user.User, string, error) {
		terminalio.WriteProcessedBytes(terminal, []byte(centralHeader("Boards", termWidth)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("  "+msg+"\r\n")), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}
	if e.V3NetCentral == nil {
		return showMessage("|08V3Net is not enabled on this system.|07")
	}
	_, available := centralNetworks(e.V3NetCentral)
	var networks []string
	for _, n := range available {
		if args == "" || strings.EqualFold(args, n.Network) {
			networks = append(networks, n.Network)
		}
	}
	if len(networks) == 0 {
		return showMessage("|08The board directory is not available on any network.|07")
	}

	// fetch loads the directory of every network, keeping the rows of a
	// network whose fetch failed out and its error for the status line.
	var all []centralBoard
	var fetchErrs []string
	fetch := func(force bool) {
		all, fetchErrs = nil, nil
		ctx, cancel := context.WithTimeout(context.Background(), centralFetchTimeout)
		defer cancel()
		for _, net := range networks {
			entries, err := e.V3NetCentral.CentralDirectory(ctx, net, force)
			if err != nil {
				fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %s", net, err))
				continue
			}
			for _, en := range entries {
				all = append(all, centralBoard{network: net, entry: en})
			}
		}
		// Online boards first, then by name, so the reachable ones are at
		// the top whatever order the hubs served.
		now := time.Now()
		rank := map[string]int{"online": 0, "recent": 1, "stale": 2}
		sort.SliceStable(all, func(i, j int) bool {
			fi, fj := rank[all[i].freshness(now)], rank[all[j].freshness(now)]
			if fi != fj {
				return fi < fj
			}
			return strings.ToLower(all[i].entry.Listing.Name) < strings.ToLower(all[j].entry.Listing.Name)
		})
	}
	terminalio.WriteProcessedBytes(terminal, []byte(centralHeader("Boards", termWidth)+string(ansi.ReplacePipeCodes([]byte("|08  Fetching the board directory...|07\r\n")))), outputMode)
	fetch(false)

	if len(all) == 0 {
		msg := "|08No boards are listed yet.|07"
		if len(fetchErrs) > 0 {
			msg = "|04Could not fetch the directory: " + v3netClip(fetchErrs[0], termWidth-40) + "|07"
		}
		return showMessage(msg)
	}

	ih := getSessionIH(s)
	filter := ""
	var rows []centralBoard
	applyFilter := func() {
		rows = rows[:0]
		for _, b := range all {
			if b.matches(filter) {
				rows = append(rows, b)
			}
		}
	}
	applyFilter()

	selected, top := 0, 0
	statusMsg := ""
	needFullRedraw := true
	multi := len(networks) > 1

	headerRows, footerRows := 3, 2
	visibleRows := max(termHeight-headerRows-footerRows, 1)

	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	clamp := func() {
		if len(rows) == 0 {
			selected, top = 0, 0
			return
		}
		selected = min(max(selected, 0), len(rows)-1)
		if selected < top {
			top = selected
		}
		if selected >= top+visibleRows {
			top = selected - visibleRows + 1
		}
		top = max(top, 0)
	}

	// Column widths: marker 1, name 22, description fills, address 26,
	// network 10 when more than one.
	nameW, addrW, netW := 22, 26, 0
	if multi {
		netW = 10
	}
	descW := termWidth - 1 - (2 + nameW + 1 + addrW + 1) - 1
	if multi {
		descW -= netW + 1
	}
	descW = max(descW, 8)

	renderRow := func(idx int, highlight bool) string {
		if idx < 0 || idx >= len(rows) {
			return strings.Repeat(" ", termWidth-1)
		}
		b := rows[idx]
		l := &b.entry.Listing
		marker := "·"
		switch b.freshness(time.Now()) {
		case "online":
			marker = "■"
		case "recent":
			marker = "√"
		}
		addr := ""
		if len(l.Addresses) > 0 {
			addr = l.Addresses[0]
		}
		// Written without expanding pipe codes, so only control
		// characters need removing; the text is the other board's.
		line := " " + marker + " " + padRight(truncateStr(ansi.OneLine(l.Name), nameW), nameW) +
			" " + padRight(truncateStr(ansi.OneLine(l.Description), descW), descW) +
			" " + padRight(truncateStr(ansi.OneLine(addr), addrW), addrW)
		if multi {
			line += " " + padRight(truncateStr(ansi.OneLine(b.network), netW), netW)
		}
		maxW := termWidth - 1
		if n := utf8.RuneCountInString(line); n < maxW {
			line += strings.Repeat(" ", maxW-n)
		} else if n > maxW {
			line = string([]rune(line)[:maxW])
		}
		if highlight {
			return "\x1b[1;37;44m" + line + "\x1b[0m"
		}
		return line
	}

	// helpLine returns the footer, pipe codes expanded and cut to the
	// screen width: it sits on the last row, where a line that runs over
	// would scroll the screen and put every later update on the wrong row.
	helpLine := func() string {
		help := "|08 [|15Enter|08] Details  [|15/|08] Search  [|15R|08] Refresh  [|15Q|08] Quit|07"
		if len(rows) > 0 {
			help += fmt.Sprintf("  |08%d/%d|07", selected+1, len(rows))
		}
		if filter != "" {
			help += fmt.Sprintf("  |03/%s|07", v3netClip(filter, 16))
		}
		if statusMsg != "" {
			help += "  " + statusMsg
		}
		return clipColumns(string(ansi.ReplacePipeCodes([]byte(help))), termWidth-1, outputMode)
	}

	renderFull := func() {
		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.WriteString(ansi.MoveCursor(1, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net Central: Boards|07  |10■|08 online  |14√|08 seen this week  |08· stale|07")))
		buf.WriteString(ansi.MoveCursor(2, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("─", termWidth-1) + "|07")))
		buf.WriteString(ansi.MoveCursor(3, 1))
		hdr := fmt.Sprintf("|03   %-*s %-*s %-*s", nameW, "Board", descW, "Description", addrW, "Address")
		if multi {
			hdr += " Network"
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(hdr + "|07")))
		for i := 0; i < visibleRows; i++ {
			buf.WriteString(ansi.MoveCursor(headerRows+1+i, 1))
			buf.WriteString(renderRow(top+i, top+i == selected))
		}
		if len(rows) == 0 {
			buf.WriteString(ansi.MoveCursor(headerRows+1, 1))
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  No boards match.|07")))
		}
		sep := headerRows + visibleRows + 1
		buf.WriteString(ansi.MoveCursor(sep, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("─", termWidth-1) + "|07")))
		buf.WriteString(ansi.MoveCursor(sep+1, 1))
		buf.WriteString(helpLine())
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
	}
	renderItemRow := func(idx int) {
		row := headerRows + 1 + (idx - top)
		if row < headerRows+1 || row > headerRows+visibleRows {
			return
		}
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(row, 1)+renderRow(idx, idx == selected)), outputMode)
	}
	renderHelpRow := func() {
		row := headerRows + visibleRows + 2
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(row, 1)+"\x1b[2K"+helpLine()), outputMode)
	}

	if len(fetchErrs) > 0 {
		statusMsg = "|04" + v3netClip(fetchErrs[0], 30) + "|07"
	}
	prevSelected, prevTop := -1, -1
	for {
		clamp()
		if needFullRedraw || top != prevTop {
			renderFull()
			needFullRedraw = false
		} else if selected != prevSelected {
			if prevSelected >= 0 {
				renderItemRow(prevSelected)
			}
			renderItemRow(selected)
			renderHelpRow()
		}
		prevSelected, prevTop = selected, top

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
		statusMsg = ""
		switch key {
		case editor.KeyArrowUp, editor.KeyCtrlE:
			selected--
		case editor.KeyArrowDown, editor.KeyCtrlX:
			selected++
		case editor.KeyPageUp, editor.KeyCtrlR:
			selected -= visibleRows
			top -= visibleRows
		case editor.KeyPageDown, editor.KeyCtrlC:
			selected += visibleRows
			top += visibleRows
		case editor.KeyHome:
			selected, top = 0, 0
		case editor.KeyEnd:
			selected = len(rows) - 1
		case editor.KeyEnter, '\n':
			if len(rows) == 0 {
				continue
			}
			if next, err := centralBoardDetails(c, rows[selected], ih); err != nil {
				return nil, next, err
			}
			needFullRedraw = true
		case '/':
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"+ansi.MoveCursor(headerRows+visibleRows+2, 1)+"\x1b[2K"+string(ansi.ReplacePipeCodes([]byte("|07 Search (blank clears): ")))), outputMode)
			line, err := readLineFromSessionIH(s, terminal)
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				return nil, "", err
			}
			filter = strings.TrimSpace(line)
			applyFilter()
			selected, top = 0, 0
			needFullRedraw = true
		case 'r', 'R':
			fetch(true)
			applyFilter()
			if len(fetchErrs) > 0 {
				statusMsg = "|04" + v3netClip(fetchErrs[0], 30) + "|07"
			} else {
				statusMsg = "|10Refreshed.|07"
			}
			needFullRedraw = true
		case 'q', 'Q', editor.KeyEsc:
			return nil, "", nil
		}
	}
}

// centralBoardDetails shows everything a board published and what the hub
// knows about it, then waits for a key. It returns the next action to take
// if the caller went away.
func centralBoardDetails(c *cmdCtx, b centralBoard, ih *editor.InputHandler) (string, error) {
	terminal := c.terminal
	outputMode := c.outputMode
	termWidth := c.termWidth
	l := &b.entry.Listing
	now := time.Now()

	var buf strings.Builder
	buf.WriteString(centralHeader("Board Details", termWidth))
	row := func(label, value string) {
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|03  %-13s|08: |07%s|07\r\n", label, v3netClip(value, termWidth-19)))))
	}
	row("Board", l.Name)
	row("Network", b.network)
	if l.Description != "" {
		row("Description", l.Description)
	}
	if len(l.Addresses) == 0 {
		row("Addresses", "(none published)")
	}
	for i, a := range l.Addresses {
		label := "Addresses"
		if i > 0 {
			label = ""
		}
		row(label, a)
	}
	if l.Software != "" {
		row("Software", l.Software)
	}
	if len(l.Capabilities) > 0 {
		row("Capabilities", strings.Join(l.Capabilities, ", "))
	}
	row("Node ID", l.NodeID)
	buf.WriteString("\r\n")
	status := "stale: not seen in over a week"
	switch b.freshness(now) {
	case "online":
		status = "online now"
	case "recent":
		status = "seen " + v3netAgo(b.entry.LastSeenAt, now)
	}
	row("Status", status)
	if !b.entry.LastSeen().IsZero() {
		row("Last seen", v3netAgo(b.entry.LastSeenAt, now))
	}
	row("Published", v3netAgo(l.PublishedAt, now))
	row("Expires", "in "+v3netIn(l.ExpiresAt, now))
	buf.Write(ansi.ReplacePipeCodes([]byte("\r\n|08  Signed by the board itself; the hub cannot alter it.|07\r\n")))
	buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth) + "|08  Press any key...|07")))
	terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

	if _, err := ih.ReadKey(); err != nil {
		if errors.Is(err, editor.ErrIdleTimeout) {
			return "LOGOFF", editor.ErrIdleTimeout
		}
		if errors.Is(err, io.EOF) {
			return "LOGOFF", io.EOF
		}
		return "", err
	}
	return "", nil
}

// v3netIn renders an RFC 3339 stamp in the future as a short duration from
// now: "3d", "5h", "20m", or "0m" once passed. Anything that does not parse
// is shown as-is, truncated.
func v3netIn(stamp string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return truncateStr(stamp, 10)
	}
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "0m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
