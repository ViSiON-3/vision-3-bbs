package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// ChatLeafProvider supplies the active V3Net chat leaf connections a
// caller may be offered: only subscriptions whose sysop switch offers
// network chat (see config.V3NetLeafConfig.CentralChat).
type ChatLeafProvider interface {
	ActiveChatLeaves() []ChatLeafInfo
}

// ChatLeafInfo describes a single V3Net chat leaf that can create sessions.
type ChatLeafInfo struct {
	NetworkName string
	NewSession  func(handle string) chat.ChatService
}

// chatEntry is how the CHAT command was asked to start, from its argument:
// the usual picker over the offered networks and Local, network chat only
// (NET, as V3Net Central's Chat entry runs it), or Local only (LOCAL).
type chatEntry int

const (
	chatEntryAny chatEntry = iota
	chatEntryNetwork
	chatEntryLocal
)

// parseChatEntry reads the CHAT command's argument.
func parseChatEntry(args string) chatEntry {
	switch strings.ToUpper(strings.TrimSpace(args)) {
	case "NET", "NETWORK":
		return chatEntryNetwork
	case "LOCAL":
		return chatEntryLocal
	}
	return chatEntryAny
}

// chatNetworkLeaves returns the V3Net networks the caller may chat on now:
// the ones the sysop offers (the provider lists no others), and only once
// the caller has agreed to network chat. With networks offered and no
// agreement yet, the notice is shown and their answer taken; a refusal
// leaves them with Local for this call, and declined says so. next and
// err are as v3netPromptLine returns them.
func chatNetworkLeaves(c *cmdCtx) (leaves []ChatLeafInfo, declined bool, next string, err error) {
	if c.e.ChatLeaves == nil {
		return nil, false, "", nil
	}
	leaves = c.e.ChatLeaves.ActiveChatLeaves()
	if len(leaves) == 0 || chatConsentStored(c) {
		return leaves, false, "", nil
	}
	agreed, next, err := chatConsentNotice(c)
	if err != nil || !agreed {
		return nil, err == nil, next, err
	}
	return leaves, false, "", nil
}

// chatSelectService shows an interactive network picker (when multiple V3Net
// networks are configured) and a room picker, then returns the connected
// ChatService and the room to join. The pickers run in normal terminal mode;
// the caller sets up the full-screen chat UI afterwards. A nil service with
// a nil error means there is nothing to chat on: network chat was asked for
// and the caller declined it or no network is available.
func chatSelectService(c *cmdCtx, entry chatEntry) (chat.ChatService, string, string, string, error) {
	e, s, terminal, outputMode := c.e, c.s, c.terminal, c.outputMode
	handle := c.currentUser.Handle
	dbPath := e.GetServerConfig().DataDir + "/chat.db"

	wt := func(text string) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
	}

	var leaves []ChatLeafInfo
	var declined bool
	if entry != chatEntryLocal {
		var next string
		var err error
		leaves, declined, next, err = chatNetworkLeaves(c)
		if err != nil {
			return nil, "", "", next, err
		}
	}

	var svc chat.ChatService
	var netName string
	var err error

	switch {
	case declined && entry == chatEntryNetwork:
		wt("\r\n|07Network chat stays off for you. You can turn it on under Preferences.|07\r\n")
		return nil, "", "", "", nil
	case len(leaves) == 0 && entry == chatEntryNetwork:
		wt("\r\n|07No network is available for chat right now.|07\r\n")
		return nil, "", "", "", nil
	case len(leaves) == 0:
		svc, err = chat.NewLocalChatService(handle, dbPath)
		if err != nil {
			return nil, "", "", "", err
		}
		netName = "Local"
	default:
		svc, netName, err = chatNetworkPicker(e, s, terminal, handle, dbPath, leaves, entry != chatEntryNetwork, outputMode, wt)
		if err != nil || svc == nil {
			return nil, "", "", "", err
		}
	}

	room, err := chatRoomPicker(e, svc, s, terminal, outputMode, wt)
	if err != nil {
		svc.Close() //nolint:errcheck
		return nil, "", "", "", err
	}
	return svc, room, netName, "", nil
}

// chatNetInfo is one entry in a chat network picker.
type chatNetInfo struct {
	name    string
	users   int
	avail   bool
	isLocal bool
}

// chatProbeTimeout is how long the network pickers wait for a leaf's room
// list before listing that network as unavailable.
var chatProbeTimeout = 2 * time.Second

// probeChatNetworks asks every leaf for its room list concurrently and returns
// one entry per leaf, in order, followed by the Local entry. A leaf is marked
// available only when it hands out a probe session and that session answers
// Rooms without error within chatProbeTimeout; users is then the total across
// its rooms.
func probeChatNetworks(leaves []ChatLeafInfo, handle string) []chatNetInfo {
	return probeChatNetworksOpt(leaves, handle, true)
}

// probeChatNetworksOpt is probeChatNetworks with the Local entry optional.
func probeChatNetworksOpt(leaves []ChatLeafInfo, handle string, withLocal bool) []chatNetInfo {
	n := len(leaves)
	if withLocal {
		n++
	}
	nets := make([]chatNetInfo, n)
	var wg sync.WaitGroup
	for i, leaf := range leaves {
		nets[i].name = leaf.NetworkName
		if leaf.NewSession == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A nil session here would otherwise panic in this goroutine and
			// take the whole BBS down, not just this node.
			probe := leaf.NewSession(handle)
			if probe == nil {
				return
			}
			type result struct {
				rooms []chat.RoomInfo
				err   error
			}
			ch := make(chan result, 1)
			// The goroutine asking for the room list owns the session and
			// closes it once Rooms returns, so a probe that times out stops
			// waiting without closing the session under a request in flight.
			// It closes the session before handing the list back, so a probe
			// that answered in time is closed by the time the picker returns
			// (#939); only a timed-out probe is closed later, when its Rooms
			// call finally returns.
			go func() {
				rooms, err := probe.Rooms()
				probe.Close() //nolint:errcheck
				ch <- result{rooms, err}
			}()
			select {
			case res := <-ch:
				if res.err != nil {
					return
				}
				nets[i].avail = true
				for _, room := range res.rooms {
					nets[i].users += room.UserCount
				}
			case <-time.After(chatProbeTimeout):
			}
		}()
	}
	wg.Wait()
	if withLocal {
		nets[len(leaves)] = chatNetInfo{name: "Local", users: -1, avail: true, isLocal: true}
	}
	return nets
}

// chatNetworkPicker displays a numbered network list, probes each for user
// counts, and returns the ChatService for the selected network. Without
// withLocal there is no Local entry, and an unavailable choice gives a nil
// service and no error rather than falling back to local chat.
func chatNetworkPicker(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, handle, dbPath string, leaves []ChatLeafInfo, withLocal bool, outputMode ansi.OutputMode, wt func(string)) (chat.ChatService, string, error) {
	nets := probeChatNetworksOpt(leaves, handle, withLocal)

	wt("\r\n")
	e.writeString(terminal, outputMode, e.Strings().ChatNetworkPickerHeader)
	wt("\r\n")
	for i, net := range nets {
		var status string
		switch {
		case !net.avail:
			status = "unavailable"
		case net.isLocal:
			status = "this BBS only"
		default:
			status = fmt.Sprintf("%d users online", net.users)
		}
		e.writeStringf(terminal, outputMode, e.Strings().ChatNetworkPickerEntry, i+1, chatText(net.name), status)
		wt("\r\n")
	}
	wt("\r\n")
	wt("|07Select network |08[|071|08]|07: ")

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return nil, "", err
	}
	n := 1
	if trimmed := strings.TrimSpace(input); trimmed != "" {
		if parsed, parseErr := strconv.Atoi(trimmed); parseErr == nil {
			n = parsed
		}
	}
	if n < 1 || n > len(nets) {
		n = 1
	}
	selected := nets[n-1]
	if !selected.avail && !withLocal {
		wt("|08Network unavailable.|07\r\n")
		return nil, "", nil
	}
	if !selected.avail {
		wt("|08Network unavailable, falling back to local.|07\r\n")
		svc, err := chat.NewLocalChatService(handle, dbPath)
		return svc, "Local", err
	}
	if selected.isLocal {
		svc, err := chat.NewLocalChatService(handle, dbPath)
		return svc, "Local", err
	}
	return leaves[n-1].NewSession(handle), selected.name, nil
}

// chatRoomPicker fetches the current room list and lets the user pick one.
// Returns "lobby" if no rooms exist yet or the user presses Enter. The only
// error is an idle timeout or the end of the caller's time at the prompt.
func chatRoomPicker(e *MenuExecutor, svc chat.ChatService, s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, wt func(string)) (string, error) {
	rooms, err := svc.Rooms()
	if err != nil || len(rooms) == 0 {
		return "lobby", nil
	}

	wt("\r\n")
	e.writeString(terminal, outputMode, e.Strings().ChatRoomListHeader)
	wt("\r\n")
	for _, r := range rooms {
		topic := chatText(r.Topic)
		if topic == "" {
			topic = "|08no topic|07"
		}
		e.writeStringf(terminal, outputMode, e.Strings().ChatRoomListEntry, chatText(r.Name), r.UserCount, topic)
		wt("\r\n")
	}
	wt("\r\n")
	wt("|07Select room |08[|07lobby|08]|07: ")

	input, err := readLineFromSessionIH(s, terminal)
	if errors.Is(err, editor.ErrIdleTimeout) {
		return "", err
	}
	if err != nil {
		return "lobby", nil
	}
	room, nameErr := chatRoomChoice(input, rooms)
	if nameErr != nil {
		wt("\r\n|07" + nameErr.Error() + " - joining lobby.\r\n")
	}
	return room, nil
}

// chatRoomChoice turns the answer to a room picker into the room to join: a
// blank answer is lobby, a number picks from rooms, and anything else is a
// room name, normalised as the services will join it, so that currentRoom
// matches the room actually joined. A name that cannot be a room gives lobby
// and NormalizeRoom's error, for the caller to report.
func chatRoomChoice(input string, rooms []chat.RoomInfo) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "lobby", nil
	}
	if n, parseErr := strconv.Atoi(trimmed); parseErr == nil && n >= 1 && n <= len(rooms) {
		return rooms[n-1].Name, nil
	}
	room, err := chat.NormalizeRoom(trimmed)
	if err != nil {
		return "lobby", err
	}
	return room, nil
}

// chatLagInterval is how often network chat measures the round trip to its
// server for the header's Lag field.
var chatLagInterval = 15 * time.Second

// chatNoRoomMsg is shown when the caller is in no room, after a /JOIN whose
// fallback rejoin of the previous room also failed.
const chatNoRoomMsg = "You are not in a room. Use /JOIN <room> to join one."

// chatContPrefix is the visual continuation-line prefix for word-wrapped messages (8 visible chars).
const chatContPrefix = "|08      \xC0|07 "
const chatContPrefixLen = 8

// errToastLineNotWritten says a toast's room line did not reach the caller.
var errToastLineNotWritten = errors.New("toast line not written")

func runChat(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	handle := currentUser.Handle

	// Chat has its own idle limit, and room traffic counts as activity
	// against it (see pumpEvents). The menus get theirs back on the way out.
	defer overrideSessionIdleTimeout(s, e.chatIdleTimeout(currentUser))()

	// The room is drawn with absolute positioning from another goroutine,
	// so a sysop chat break-in here would garble both.
	if t := tapOf(s); t != nil {
		t.SetMode(snoop.ModeTeleconf)
		defer t.SetMode(snoop.ModeBBS)
	}

	if termWidth <= 0 {
		termWidth = 80
	}

	height := 24
	if termHeight > 0 {
		height = termHeight
	} else if sess := e.nodeSession(nodeNumber); sess != nil {
		if _, h := sess.TermSize(); h > 0 {
			height = h
		}
	}

	// Screen layout (Retrograde MRC-style), with n input rows (1 at rest):
	//   Rows 1-5          : ANSI art header
	//   Rows 6..sb        : chat scroll region  (sb = height-n-2)
	//   Row height-n-1    : status bar — users
	//   Row height-n      : status bar — hints
	//   Rows height-n+1.. : input area, ending on the last row (safe because
	//                       chatReadLine never writes \n there)
	// chatScreen owns the rows below the header; see chat_input.go.
	const chatHeaderRows = 5
	chatTop := chatHeaderRows + 1 // row 6

	// Network and room selection — runs in normal terminal mode before full-screen UI.
	entry := parseChatEntry(args)
	svc, selectedRoom, selectedNetwork, next, err := chatSelectService(c, entry)
	if errors.Is(err, editor.ErrIdleTimeout) || next == "LOGOFF" {
		return nil, "LOGOFF", err
	}
	if err != nil {
		slog.Error("chat setup failed", "node", nodeNumber, "error", err)
		return nil, "", nil
	}
	if svc == nil {
		return nil, "", nil
	}

	// Chat state — all accessed under rawMu.
	currentRoom := selectedRoom
	currentNetwork := selectedNetwork
	currentTopic := ""
	currentUsers := []chat.ChatUser{}
	// homeNode is this board's node ID on the current network, which tells
	// a member or sender from this board apart from one on another board,
	// who is named handle@Board; "" on local chat. See chatUserName.
	homeNode := chatNodeOf(svc)
	mentions := 0 // room messages naming the caller since joining the room
	scrollback := &chatScrollback{capacity: chatScrollbackLines}
	// Network round trip for the Lag field. pinging is false on a service
	// that cannot be measured (Local), which marks the field local; lagOK
	// is false until a measurement succeeds, which shows dashes; lagKnown
	// is false until any measurement completes, which keeps the link field
	// from calling the hub unreachable before it has been asked.
	pinging, lagOK, lagKnown := false, false, false
	var lag time.Duration

	var rawMu sync.Mutex

	// writeFailed notes a failed write for those that need to know, as a toast
	// that must stay queued if it did not reach the caller.
	var writeFailed atomic.Bool
	rawWriteLocked := func(data []byte) {
		if err := terminalio.WriteProcessedBytes(s, data, outputMode); err != nil {
			writeFailed.Store(true)
		}
	}
	scr := &chatScreen{height: height, width: termWidth, chatTop: chatTop, inputRows: 1, write: rawWriteLocked}

	rawWrite := func(data []byte) {
		rawMu.Lock()
		defer rawMu.Unlock()
		rawWriteLocked(data)
	}

	// drawHeaderLocked redraws the 5-row ANSI art header. Caller must hold rawMu.
	// It loads CHATHEADER.ANS from the menu set's ansi directory and fills its
	// placeholders (see chatArtFill): @NET@, @ROOM@ and @TOPIC@ with the
	// network, room and topic, and the counters @U@ (users in the room), @M@
	// (mentions since joining), @L@ (network lag) and @B@ (scrollback use).
	// Falls back to a simple text header if the art file is not found.
	drawHeaderLocked := func() {
		artPath := e.menuFile("ansi", "CHATHEADER.ANS")
		artData, artErr := ansi.GetAnsiFileContent(artPath)
		if artErr == nil {
			// Convert CP437 high bytes to UTF-8 so box-drawing chars render
			// correctly on UTF-8 SSH terminals.
			artData = ansi.ConvertCP437ToUTF8(artData)
			// Substitute AFTER the conversion. The network name comes from a
			// V3Net leaf and can be non-ASCII; substituting first would feed its
			// UTF-8 bytes through the CP437 converter, which reinterprets each
			// byte separately and garbles the name rather than just cutting it.
			// The @NAME@ placeholders are ASCII, so conversion leaves them intact.
			text := func(v string) func(int) string { return func(int) string { return v } }
			artData = chatArtFill(artData, map[string]func(int) string{
				// The art is not expanded, so a pipe in these shows as it is
				// without escaping; only control characters need to go.
				"NET":   text(toastLine(currentNetwork)),
				"ROOM":  text(toastLine(currentRoom)),
				"TOPIC": text(toastLine(currentTopic)),
				"U":     func(w int) string { return chatCounter(w, len(currentUsers), "") },
				"M":     func(w int) string { return chatCounter(w, mentions, "") },
				"L": func(w int) string {
					if !pinging {
						return chatLocalField(w)
					}
					return chatLagField(w, lag, lagOK)
				},
				"K": func(w int) string { return chatLinkField(pinging, lagKnown, lagOK) },
				"B": func(w int) string { return chatBufferField(w, scrollback.len(), chatScrollbackLines) },
			})
			// The art is UTF-8 by now; make wraps explicit so each header
			// row splits out on its own on terminals wider than the art.
			artData = fitArt(terminal, artData, termWidth, true)
			// Split into lines (SAUCE already stripped by GetAnsiFileContent).
			lines := strings.Split(string(artData), "\n")
			for i := 0; i < chatHeaderRows && i < len(lines); i++ {
				line := strings.TrimRight(lines[i], "\r")
				rawWriteLocked([]byte(fmt.Sprintf("\x1B[%d;1H", i+1)))
				rawWriteLocked([]byte(line))
			}
		} else {
			// Fallback: simple text header using the separator string.
			sep := ansi.ReplacePipeCodes([]byte(e.Strings().ChatSeparator))
			rawWriteLocked([]byte(ansi.MoveCursor(1, 1)))
			rawWriteLocked(sep)
			rawWriteLocked([]byte(ansi.MoveCursor(2, 1)))
			rawWriteLocked(sep)
			roomLine := " |15#" + chatText(currentRoom)
			if currentTopic != "" {
				roomLine += " |08/ |07" + chatText(currentTopic)
			}
			rawWriteLocked([]byte(ansi.MoveCursor(2, 1)))
			rawWriteLocked(ansi.ReplacePipeCodes([]byte(roomLine)))
			rawWriteLocked([]byte(ansi.MoveCursor(3, 1)))
			rawWriteLocked(sep)
			for row := 4; row <= chatHeaderRows; row++ {
				rawWriteLocked([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K", row)))
			}
		}
		scr.restoreCursor()
	}

	// drawStatusBarLocked redraws the 2-row status bar above the input area,
	// wherever its current height puts it. Caller must hold rawMu.
	drawStatusBarLocked := func() {
		sep := ansi.ReplacePipeCodes([]byte(e.Strings().ChatSeparator))
		usersRow, hintsRow := scr.statusRow(), scr.statusRow()+1
		// Users line
		rawWriteLocked([]byte(ansi.MoveCursor(usersRow, 1) + "\x1B[2K"))
		rawWriteLocked(sep)
		userStr := chatUserList(currentUsers, homeNode)
		if userStr == "" {
			userStr = handle
		}
		rawWriteLocked([]byte(ansi.MoveCursor(usersRow, 2)))
		rawWriteLocked(ansi.ReplacePipeCodes([]byte(fmt.Sprintf(" |07Users: %s", chatText(userStr)))))
		// Hints line
		rawWriteLocked([]byte(ansi.MoveCursor(hintsRow, 1) + "\x1B[2K"))
		rawWriteLocked(sep)
		rawWriteLocked([]byte(ansi.MoveCursor(hintsRow, 2)))
		hints := " |08/rooms /join /msg /topic /network /q"
		if !scrollback.live() {
			hints = e.Strings().ChatScrollbackHint
		}
		rawWriteLocked(ansi.ReplacePipeCodes([]byte(hints)))
		scr.restoreCursor()
	}
	scr.drawStatus = drawStatusBarLocked

	// drawRegionLocked redraws the chat scroll region from the scrollback:
	// the newest lines on the live view, older ones while paged back. The
	// lines sit at the bottom of the region, as scrolling leaves them.
	// Caller must hold rawMu.
	drawRegionLocked := func() {
		bottom := scr.scrollBottom()
		lines := scrollback.view(bottom - chatTop + 1)
		var b strings.Builder
		for row := chatTop; row <= bottom; row++ {
			fmt.Fprintf(&b, "\x1B[%d;1H\x1B[2K", row)
		}
		for i, line := range lines {
			b.WriteString(ansi.MoveCursor(bottom-len(lines)+1+i, 1))
			b.Write(ansi.ReplacePipeCodes([]byte(line)))
		}
		rawWriteLocked([]byte(b.String()))
		scr.restoreCursor()
	}
	scr.redrawRegion = drawRegionLocked

	// wakeToastsLocked calls for toasts held while the view was paged back.
	wakeToastsLocked := func() {
		if sess := e.nodeSession(nodeNumber); sess != nil {
			sess.WakeToasts()
		}
	}

	// scr.page moves the view a page up or down the scrollback, with the
	// hint bar saying when it is off the live view. Called with rawMu held.
	scr.page = func(dir int) {
		if scrollback.page(dir, scr.scrollBottom()-chatTop+1) {
			drawRegionLocked()
			drawStatusBarLocked()
			if scrollback.live() {
				wakeToastsLocked()
			}
		}
	}

	// keepChatLineLocked wraps text into the scrollback as a line stamped
	// with the time at, or now when at is zero, without drawing it. Caller
	// must hold rawMu.
	keepChatLineLocked := func(text string, at time.Time) {
		fullText := fmt.Sprintf("|08%s|07 %s", chatStamp(at, time.Now()), text)
		// The last column stays free, as it does on the input rows: a terminal
		// without a deferred wrap moves to the next row as soon as a character
		// lands there, and on the bottom row of the region that scrolls the
		// room by a row that nothing here accounts for (#911).
		scrollback.add(wrapPipeText(fullText, max(termWidth-1, 1), chatContPrefix, chatContPrefixLen, outputMode)...)
	}

	// showChatLinesLocked redraws what keepChatLineLocked added since the
	// scrollback held `held` lines: the Buffer counter if it changed, and the
	// chat region unless the caller is paged back, when the lines show once
	// they return. Caller must hold rawMu.
	//
	// The region is redrawn from the scrollback rather than scrolled. SyncTERM
	// scrolls a DECSTBM region in its cell model but leaves the pixels where
	// they were, and repaints only the cells whose contents change, so a line
	// scrolled into place stayed on screen and showed through the spaces of
	// the next line written over it (#911). Redrawing changes every cell of
	// every row, so the terminal shows what the model holds.
	showChatLinesLocked := func(held int) {
		if scrollback.len() != held {
			drawHeaderLocked()
		}
		if scrollback.live() {
			drawRegionLocked()
		}
	}

	// writeChatLineLocked writes a timestamped, word-wrapped message into the
	// chat region and the scrollback. While the caller is paged back the line
	// is only kept, and shows when they return. Caller must hold rawMu.
	//
	// Strings shown inside the split screen are printed as text even when they
	// start with !door or %file (see string_hooks.go): they are wrapped into
	// the scroll region, many arrive on the receive goroutine with rawMu held,
	// and a script or a full-screen file would tear the layout apart.
	writeChatLineLocked := func(text string) {
		held := scrollback.len()
		keepChatLineLocked(text, time.Time{})
		showChatLinesLocked(held)
	}

	// writeChatMessageLocked writes a room or private message as text,
	// stamped with the time it was sent. Caller must hold rawMu.
	writeChatMessageLocked := func(msg chat.ChatMessage, text string) {
		held := scrollback.len()
		keepChatLineLocked(text, msg.Timestamp)
		showChatLinesLocked(held)
	}

	// writeChatHistory shows a room's history, each line at the time it was
	// said, then the join notice, where the live room starts, as one redraw
	// of the region rather than one per line.
	writeChatHistory := func(room string, history []chat.ChatMessage) {
		rawMu.Lock()
		defer rawMu.Unlock()
		held := scrollback.len()
		for _, msg := range history {
			keepChatLineLocked(formatChatMessage(msg, homeNode, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat), msg.Timestamp)
		}
		keepChatLineLocked(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Joined #"+room), time.Time{})
		showChatLinesLocked(held)
	}

	writeChatLine := func(text string) {
		rawMu.Lock()
		defer rawMu.Unlock()
		writeChatLineLocked(text)
	}

	// Clear screen and set scroll region to the chat area only.
	// Write directly to s (not via terminal) so term.Terminal's cursor tracking
	// never interferes with our direct-write approach.
	terminalio.WriteProcessedBytes(s, []byte(ansi.ClearScreen()), outputMode)
	terminalio.WriteProcessedBytes(s, []byte(fmt.Sprintf("\x1B[%d;%dr", chatTop, scr.scrollBottom())), outputMode)

	// The pickers took time, and the caller's agreement to network chat
	// may have been withdrawn on another node meanwhile: check it again
	// right before the join publishes their handle.
	if !chatIsLocal(svc) && !chatConsentStored(c) {
		svc.Close() //nolint:errcheck
		rawWrite([]byte("\x1B[r" + ansi.ClearScreen()))
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Network chat is off for you. You can turn it on under V3Net Central -> Preferences.|07\r\n")), outputMode)
		return nil, "", nil
	}

	_, history, err := svc.Join(currentRoom)
	if err != nil && entry == chatEntryNetwork {
		// Network chat only: no room is no chat, rather than local chat
		// the caller did not ask for.
		slog.Info("chat join failed", "node", nodeNumber, "error", err)
		svc.Close() //nolint:errcheck
		rawWrite([]byte("\x1B[r" + ansi.ClearScreen()))
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Could not join the room: "+chatText(err.Error())+"|07\r\n")), outputMode)
		return nil, "", nil
	}
	if err != nil {
		slog.Info("chat join failed, falling back to local chat", "node", nodeNumber, "error", err)
		svc.Close() //nolint:errcheck
		dbPath := e.GetServerConfig().DataDir + "/chat.db"
		var localErr error
		svc, localErr = chat.NewLocalChatService(handle, dbPath)
		if localErr != nil {
			slog.Error("local chat fallback failed", "node", nodeNumber, "error", localErr)
			return nil, "", nil
		}
		currentNetwork = "Local"
		_, history, err = svc.Join(currentRoom)
		if err != nil {
			slog.Error("local chat join failed", "node", nodeNumber, "error", err)
			svc.Close() //nolint:errcheck
			return nil, "", nil
		}
	}

	// Draw header and status bar with initial state. The Lag and link fields
	// show whether there is a link to measure from the first draw; startLag
	// measures it once the room is on screen.
	currentUsers = svc.Users()
	rawMu.Lock()
	_, pinging = svc.(chat.Pinger)
	drawHeaderLocked()
	drawStatusBarLocked()
	rawMu.Unlock()

	// Show join notice and scrollback history.
	writeChatHistory(currentRoom, history)

	// Draw initial input prompt. chatReadLine will manage it from here on.
	chatPrompt := fmt.Sprintf("<%s> ", handle)
	rawWrite([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K%s", scr.height, chatPrompt)))

	// Toasts become lines in the room. A line written while paged back would
	// only be kept, unseen, so they hold until the view is live again.
	defer setToastLineScreen(s, e.nodeSession(nodeNumber), toastLineScreen{
		ready: func() bool {
			rawMu.Lock()
			defer rawMu.Unlock()
			return scrollback.live()
		},
		write: func(text string) error {
			rawMu.Lock()
			defer rawMu.Unlock()
			writeFailed.Store(false)
			writeChatLineLocked(text)
			if writeFailed.Load() {
				return errToastLineNotWritten
			}
			return nil
		},
	})()

	// pumpEvents shows the events svc delivers until its Events channel is
	// closed, then closes done. It runs on its own goroutine, one per
	// service: /network starts a new one after closing the old service.
	ih := getSessionIH(s)
	pumpEvents := func(svc chat.ChatService, done chan struct{}) {
		defer close(done)
		for ev := range svc.Events() {
			switch ev.Type {
			case chat.TypeMessage, chat.TypePrivate, chat.TypeJoin, chat.TypeLeave, chat.TypeTopic:
				// Someone is talking: a caller reading along isn't idle.
				ih.NoteActivity()
			}
			switch ev.Type {
			case chat.TypeMessage:
				if ev.Message != nil {
					rawMu.Lock()
					writeChatMessageLocked(*ev.Message, formatChatMessage(*ev.Message, homeNode, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
					if !ev.Message.IsSystem && chatMentions(ev.Message.Text, handle) {
						mentions++
						drawHeaderLocked()
					}
					rawMu.Unlock()
				}
			case chat.TypePrivate:
				if ev.Message != nil {
					rawMu.Lock()
					from := chatUserName(ev.Message.Handle, ev.Message.Node, ev.Message.BBS, homeNode)
					writeChatMessageLocked(*ev.Message, fmt.Sprintf(e.Strings().ChatPrivateMsgFormat, chatText(from), chatText(ev.Message.Text)))
					rawMu.Unlock()
				}
			case chat.TypeJoin:
				if ev.Join != nil {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					who := chatUserName(ev.Join.Handle, ev.Join.Node, ev.Join.BBS, homeNode)
					writeChatLineLocked(fmt.Sprintf(e.Strings().ChatJoinMsg, chatText(who), chatText(ev.Join.Room)))
					drawHeaderLocked()
					drawStatusBarLocked()
					rawMu.Unlock()
				}
			case chat.TypeLeave:
				if ev.Leave != nil {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					who := chatUserName(ev.Leave.Handle, ev.Leave.Node, ev.Leave.BBS, homeNode)
					writeChatLineLocked(fmt.Sprintf(e.Strings().ChatLeaveMsg, chatText(who), chatText(ev.Leave.Room)))
					drawHeaderLocked()
					drawStatusBarLocked()
					rawMu.Unlock()
				}
			case chat.TypeTopic:
				if ev.Topic != nil {
					rawMu.Lock()
					currentTopic = ev.Topic.Topic
					writeChatLineLocked(fmt.Sprintf(e.Strings().ChatTopicMsg, chatText(ev.Topic.Room), chatText(ev.Topic.Topic)))
					drawHeaderLocked()
					rawMu.Unlock()
				}
			case chat.TypeSystem:
				if ev.Reconnect {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					if ev.Topic != nil && ev.Topic.Room == currentRoom {
						currentTopic = ev.Topic.Topic
					}
					writeChatLineLocked(e.Strings().ChatReconnected)
					drawHeaderLocked()
					drawStatusBarLocked()
					rawMu.Unlock()
				} else if ev.Text != "" {
					writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatText(ev.Text)))
				}
			}
		}
	}
	done := make(chan struct{})
	go pumpEvents(svc, done)

	// startLag measures svc's round trip now and every chatLagInterval, if
	// it is a chat.Pinger, and redraws the header when the shown value
	// changes. The returned function stops it and waits for it to finish;
	// call it without rawMu held.
	startLag := func(svc chat.ChatService) func() {
		p, ok := svc.(chat.Pinger)
		rawMu.Lock()
		pinging, lagOK, lagKnown = ok, false, false
		rawMu.Unlock()
		if !ok {
			return func() {}
		}
		ctx, cancel := context.WithCancel(context.Background())
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			tick := time.NewTicker(chatLagInterval)
			defer tick.Stop()
			// The header may have been drawn before pinging was set, with a
			// blank field, so the first result redraws it even as a failure.
			first := true
			for {
				d, err := p.Ping(ctx)
				rawMu.Lock()
				if ctx.Err() == nil {
					wasShown, wasOK := lag.Milliseconds(), lagOK
					lag, lagOK, lagKnown = d, err == nil, true
					if first || lagOK != wasOK || (lagOK && lag.Milliseconds() != wasShown) {
						drawHeaderLocked()
					}
					first = false
				}
				rawMu.Unlock()
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}()
		return func() {
			cancel()
			<-stopped
			rawMu.Lock()
			pinging = false
			rawMu.Unlock()
		}
	}
	stopLag := startLag(svc)

	cleanup := func() {
		stopLag()
		if currentRoom != "" {
			svc.Leave(currentRoom) //nolint:errcheck
		}
		svc.Close() //nolint:errcheck
		<-done
	}

	// onNetwork is whether svc is a V3Net session rather than local chat:
	// network chat is the caller's agreement to re-check (see
	// chatConsentStored), and the leaf's to end (see done).
	onNetwork := !chatIsLocal(svc)

	for {
		input, err := chatReadLine(s, &rawMu, scr, chatPrompt)
		if err != nil {
			// An idle timeout or the time limit goes up as it is: the menu
			// loop shows the caller why the call is ending and logs it.
			if err == io.EOF || errors.Is(err, editor.ErrIdleTimeout) {
				cleanup()
				rawWrite([]byte("\x1B[r"))
				terminal.SetPrompt("")
				return nil, "LOGOFF", err
			}
			slog.Error("chat input error", "node", nodeNumber, "error", err)
			break
		}

		// The leaf ends its sessions when a reload stops it (the reason
		// was shown as a system line as it did), and the caller's
		// agreement can be withdrawn under Preferences on another node.
		// Either way network chat is over: say so and leave.
		if onNetwork {
			ended := false
			select {
			case <-done:
				ended = true
			default:
			}
			if !ended && !chatConsentStored(c) {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "You turned network chat off under Preferences; leaving."))
				ended = true
			}
			if ended {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Network chat has ended."))
				break
			}
		}

		// Sending a line, even a blank one, returns a paged-back view to
		// the newest messages.
		rawMu.Lock()
		if scrollback.toLive() {
			drawRegionLocked()
			drawStatusBarLocked()
			wakeToastsLocked()
		}
		rawMu.Unlock()

		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			continue
		}

		// A line starting with / is a command, and is never posted: an
		// unknown or incomplete one is answered on this screen alone (#659).
		// "//" escapes the slash, so a message can still start with one.
		isCommand := strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "//")
		var upper, arg string
		if isCommand {
			var cmd string
			cmd, arg, _ = strings.Cut(trimmed, " ")
			upper = strings.ToUpper(cmd)
			arg = strings.TrimSpace(arg)
		}
		if upper == "/Q" || upper == "/QUIT" {
			break
		}

		if upper == "/?" || upper == "/HELP" {
			for _, line := range strings.Split(e.Strings().ChatHelp, "\n") {
				writeChatLine(strings.TrimRight(line, "\r"))
			}
			continue
		}

		if upper == "/JOIN" {
			if arg == "" {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, e.Strings().ChatJoinUsage))
				continue
			}
			// Normalise up front, as Join would: a name the service will
			// refuse is rejected without leaving the current room, and
			// currentRoom then names the room actually joined, which later
			// posts and the final Leave are addressed to.
			newRoom, nameErr := chat.NormalizeRoom(arg)
			if nameErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not join room: "+nameErr.Error()))
				continue
			}
			// Leave before joining rather than after: both services track a
			// single current room, and Leave clears it whichever room is
			// named, so leaving the old room second would drop the new one.
			// If the join fails, go back to the old room instead.
			oldRoom := currentRoom
			if oldRoom != "" {
				svc.Leave(oldRoom) //nolint:errcheck
			}
			_, joinHistory, joinErr := svc.Join(newRoom)
			if joinErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not join room: "+chatText(joinErr.Error())))
				rejoined := false
				if oldRoom != "" {
					if _, _, rejoinErr := svc.Join(oldRoom); rejoinErr != nil {
						writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not rejoin #"+chatText(oldRoom)+": "+chatText(rejoinErr.Error())))
					} else {
						rejoined = true
					}
				}
				rawMu.Lock()
				if !rejoined {
					// In no room now: clear it so nothing is posted to a room
					// the caller has left, until a /JOIN succeeds.
					currentRoom = ""
					currentTopic = ""
				}
				mentions = 0
				currentUsers = svc.Users()
				drawHeaderLocked()
				drawStatusBarLocked()
				rawMu.Unlock()
				if !rejoined {
					writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatNoRoomMsg))
				}
				continue
			}
			rawMu.Lock()
			currentRoom = newRoom
			currentTopic = ""
			mentions = 0
			currentUsers = svc.Users()
			drawHeaderLocked()
			drawStatusBarLocked()
			rawMu.Unlock()
			writeChatHistory(currentRoom, joinHistory)
			continue
		}

		if upper == "/NETWORK" {
			// Stop the current service and event goroutine.
			cleanup()

			// Probe available networks inline: the ones the sysop offers,
			// and only while the caller's agreement to network chat stands.
			// Network-only chat lists no Local, and ends rather than fall
			// back to it; cleanup has run, so these paths return directly.
			var leaves []ChatLeafInfo
			if e.ChatLeaves != nil && chatConsentStored(c) {
				leaves = e.ChatLeaves.ActiveChatLeaves()
			}
			dbPath := e.GetServerConfig().DataDir + "/chat.db"
			netOnly := entry == chatEntryNetwork
			endNetOnly := func(why string) (*user.User, string, error) {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, why))
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Network chat has ended."))
				rawWrite([]byte("\x1B[r"))
				terminal.SetPrompt("")
				return nil, "", nil
			}
			nets := probeChatNetworksOpt(leaves, handle, !netOnly)
			if len(nets) == 0 {
				return endNetOnly("No network is available for chat right now.")
			}

			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Chat Networks:"))
			for i, net := range nets {
				switch {
				case !net.avail:
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-15s |08(unavailable)|07", i+1, chatText(net.name)))
				case net.isLocal:
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-15s |08(this BBS only)|07", i+1, chatText(net.name)))
				default:
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-15s |08(%d users online)|07", i+1, chatText(net.name), net.users))
				}
			}

			netInput, netErr := chatReadLine(s, &rawMu, scr, "Select network [1]: ")
			if errors.Is(netErr, editor.ErrIdleTimeout) {
				// cleanup has already run.
				rawWrite([]byte("\x1B[r"))
				terminal.SetPrompt("")
				return nil, "LOGOFF", netErr
			}
			var newSvc chat.ChatService
			var newNetName string
			if netErr != nil && netOnly {
				return endNetOnly("No network chosen.")
			}
			if netErr != nil {
				newSvc, err = chat.NewLocalChatService(handle, dbPath)
				newNetName = "Local"
			} else {
				n := 1
				if t := strings.TrimSpace(netInput); t != "" {
					if parsed, parseErr := strconv.Atoi(t); parseErr == nil {
						n = parsed
					}
				}
				if n < 1 || n > len(nets) {
					n = 1
				}
				sel := nets[n-1]
				switch {
				case !sel.avail && netOnly:
					return endNetOnly("Network unavailable.")
				case !sel.avail:
					writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Network unavailable, using local."))
					newSvc, err = chat.NewLocalChatService(handle, dbPath)
					newNetName = "Local"
				case sel.isLocal:
					newSvc, err = chat.NewLocalChatService(handle, dbPath)
					newNetName = "Local"
				default:
					newSvc = leaves[n-1].NewSession(handle)
					newNetName = sel.name
				}
			}
			if err != nil || newSvc == nil {
				slog.Error("/network reconnect failed", "node", nodeNumber, "error", err)
				rawWrite([]byte("\x1B[r"))
				return nil, "", nil
			}

			// Room picker inline.
			newRoom := "lobby"
			if netRooms, roomErr := newSvc.Rooms(); roomErr == nil && len(netRooms) > 0 {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Available Rooms:"))
				for i, r := range netRooms {
					topic := r.Topic
					if topic == "" {
						topic = "no topic"
					}
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-12s |08(%d)|07 %s", i+1, chatText(r.Name), r.UserCount, chatText(topic)))
				}
				roomInput, roomErr2 := chatReadLine(s, &rawMu, scr, "Select room [lobby]: ")
				if errors.Is(roomErr2, editor.ErrIdleTimeout) {
					newSvc.Close() //nolint:errcheck
					rawWrite([]byte("\x1B[r"))
					terminal.SetPrompt("")
					return nil, "LOGOFF", roomErr2
				}
				if roomErr2 == nil {
					var nameErr error
					if newRoom, nameErr = chatRoomChoice(roomInput, netRooms); nameErr != nil {
						writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, nameErr.Error()+" - joining lobby."))
					}
				}
			}

			// The pickers took time: a withdrawal of the caller's agreement
			// meanwhile means no network join now.
			if !chatIsLocal(newSvc) && !chatConsentStored(c) {
				newSvc.Close() //nolint:errcheck
				return endNetOnly("You turned network chat off under Preferences; leaving.")
			}

			// Join the new room.
			_, newHistory, joinErr := newSvc.Join(newRoom)
			if joinErr != nil && netOnly {
				newSvc.Close() //nolint:errcheck
				return endNetOnly("Could not join room: " + chatText(joinErr.Error()))
			}
			if joinErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not join room: "+chatText(joinErr.Error())))
				newSvc.Close() //nolint:errcheck
				newSvc, err = chat.NewLocalChatService(handle, dbPath)
				if err != nil {
					rawWrite([]byte("\x1B[r"))
					return nil, "", nil
				}
				newNetName = "Local"
				_, newHistory, joinErr = newSvc.Join(newRoom)
				if joinErr != nil {
					newSvc.Close() //nolint:errcheck
					rawWrite([]byte("\x1B[r"))
					return nil, "", nil
				}
			}

			// Swap in the new service and restart the event goroutine.
			svc = newSvc
			currentRoom = newRoom
			currentNetwork = newNetName
			onNetwork = !chatIsLocal(svc)
			done = make(chan struct{})
			stopLag = startLag(svc)
			rawMu.Lock()
			homeNode = chatNodeOf(svc)
			currentTopic = ""
			mentions = 0
			currentUsers = svc.Users()
			scrollback.reset()
			drawHeaderLocked()
			for row := chatTop; row <= scr.scrollBottom(); row++ {
				rawWriteLocked([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K", row)))
			}
			drawStatusBarLocked()
			rawMu.Unlock()

			writeChatHistory(currentRoom, newHistory)

			go pumpEvents(svc, done)
			continue
		}

		if upper == "/ROOMS" {
			rooms, roomErr := svc.Rooms()
			if roomErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not list rooms: "+chatText(roomErr.Error())))
			} else {
				writeChatLine(e.Strings().ChatRoomListHeader)
				for _, r := range rooms {
					writeChatLine(fmt.Sprintf(e.Strings().ChatRoomListEntry, chatText(r.Name), r.UserCount, chatText(r.Topic)))
				}
			}
			continue
		}

		if upper == "/TOPIC" {
			if arg == "" {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, e.Strings().ChatTopicUsage))
				continue
			}
			if currentRoom == "" {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatNoRoomMsg))
				continue
			}
			if topicErr := svc.SetTopic(currentRoom, arg); topicErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not set topic: "+chatText(topicErr.Error())))
			}
			continue
		}

		if upper == "/MSG" {
			rawMu.Lock()
			users, home := currentUsers, homeNode
			rawMu.Unlock()
			toHandle, toNode, message, targetErr := chatMsgTarget(arg, users, home)
			if errors.Is(targetErr, errChatMsgUsage) {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, e.Strings().ChatMsgUsage))
				continue
			}
			if targetErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatText(targetErr.Error())))
				continue
			}
			if msgErr := svc.Private(toHandle, toNode, message); msgErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not send private message: "+chatText(msgErr.Error())))
			}
			continue
		}

		if upper == "/USERS" {
			rawMu.Lock()
			currentUsers = svc.Users()
			drawHeaderLocked()
			drawStatusBarLocked()
			rawMu.Unlock()
			continue
		}

		if isCommand {
			cmd, _, _ := strings.Cut(trimmed, " ")
			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, fmt.Sprintf(e.Strings().ChatUnknownCommand, chatText(cmd))))
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			trimmed = trimmed[1:]
		}

		if currentRoom == "" {
			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatNoRoomMsg))
			continue
		}
		if postErr := svc.Post(currentRoom, trimmed); postErr != nil {
			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not post: "+chatText(postErr.Error())))
			continue
		}

		// Echo own message locally.
		ownMsg := chat.ChatMessage{Handle: handle, Text: trimmed, Timestamp: time.Now()}
		writeChatLine(formatChatMessage(ownMsg, homeNode, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
	}

	cleanup()
	rawWrite([]byte("\x1B[r"))
	terminal.SetPrompt("")
	return nil, "", nil
}

// chatIsLocal reports whether svc is this board's local chat rather than a
// network session.
func chatIsLocal(svc chat.ChatService) bool {
	_, ok := svc.(*chat.LocalChatService)
	return ok
}

// chatText prepares text from a caller, a hub or another board to go into a
// pipe-coded chat line: flattened to one line with no control characters, and
// with only its colour codes left to act. A line is expanded once, whether it is
// drawn now or replayed from the scrollback, so "|CL" shows as just that. Formats
// from strings.json keep every pipe code, so escape the arguments, not the line.
func chatText(s string) string {
	return ansi.EscapeScreenPipeCodes(toastLine(s))
}

// formatChatMessage formats a room message as a chat line: a system message
// with systemFmt, anyone else's with userFmt, named as chatUserName names
// them against home, this board's node ID.
func formatChatMessage(msg chat.ChatMessage, home, systemFmt, userFmt string) string {
	if msg.IsSystem {
		return fmt.Sprintf(systemFmt, chatText(msg.Text))
	}
	return fmt.Sprintf(userFmt, chatText(chatUserName(msg.Handle, msg.Node, msg.BBS, home)), chatText(msg.Text))
}

// chatNodeOf returns this board's node ID on svc's network, or "" for a
// service with no network, such as local chat.
func chatNodeOf(svc chat.ChatService) string {
	if n, ok := svc.(chat.NodeIdentifier); ok {
		return n.NodeID()
	}
	return ""
}

// chatUserName is how a member or sender is named on the chat screen: the
// bare handle for someone on this board, whose node is home, or who has no
// node (local chat, or a hub that predates members), and handle@Board for
// someone on another board, with the board's node ID in place of a name
// the hub does not have.
func chatUserName(handle, node, bbs, home string) string {
	if node == "" || node == home {
		return handle
	}
	if bbs == "" {
		bbs = node
	}
	return handle + "@" + bbs
}

// chatUserList is the status bar's user list: the members as chatUserName
// names them, comma-separated, in the order the service lists them.
func chatUserList(users []chat.ChatUser, home string) string {
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = chatUserName(u.Handle, u.Node, u.BBS, home)
	}
	return strings.Join(names, ", ")
}

// errChatMsgUsage says a /msg had no recipient or no text.
var errChatMsgUsage = errors.New("usage: /msg <who> <text>")

// chatMsgTarget reads the argument of /msg: who the message is for, and the
// text. The recipient is looked for in the room's user list first, as the
// status bar names them: the bare handle of someone on this board, or
// handle@Board for someone on another, either of which may hold spaces, so
// the longest name the argument starts with wins and the text is what
// follows it. Someone on another board may be named by bare handle too, when
// nobody on this board has it; a handle that several boards in the room
// share is refused with an error naming them. A recipient not in the room is
// read as before: the first word, with an @node suffix naming the board's
// node ID, for the hub to resolve among the network's rooms. errChatMsgUsage
// is returned for a missing recipient or text.
func chatMsgTarget(arg string, users []chat.ChatUser, home string) (handle, node, text string, err error) {
	type match struct {
		user  chat.ChatUser
		exact bool // matched by its name on the list, not by bare handle
		rest  string
	}
	var found []match
	longest := 0
	for _, u := range users {
		name := chatUserName(u.Handle, u.Node, u.BBS, home)
		for _, try := range []struct {
			name  string
			exact bool
		}{{name, true}, {u.Handle, false}} {
			n := len(try.name)
			if n == 0 || n < longest || len(arg) < n || !strings.EqualFold(arg[:n], try.name) {
				continue
			}
			if len(arg) > n && arg[n] != ' ' {
				continue
			}
			if n > longest {
				found, longest = nil, n
			}
			found = append(found, match{u, try.exact, strings.TrimSpace(arg[n:])})
			break
		}
	}
	// A name as listed beats a bare handle shared with someone elsewhere:
	// "Bob" is this board's Bob when there is one, not also Bob@Other BBS.
	if slices.ContainsFunc(found, func(m match) bool { return m.exact }) {
		found = slices.DeleteFunc(found, func(m match) bool { return !m.exact })
	}
	found = slices.CompactFunc(found, func(a, b match) bool {
		return a.user.Handle == b.user.Handle && a.user.Node == b.user.Node
	})
	switch len(found) {
	case 0:
		target, message, ok := strings.Cut(arg, " ")
		message = strings.TrimSpace(message)
		if !ok || message == "" {
			return "", "", "", errChatMsgUsage
		}
		handle, node = target, ""
		if at := strings.Index(target, "@"); at > 0 && at < len(target)-1 {
			handle, node = target[:at], target[at+1:]
		}
		return handle, node, message, nil
	case 1:
		if found[0].rest == "" {
			return "", "", "", errChatMsgUsage
		}
		return found[0].user.Handle, found[0].user.Node, found[0].rest, nil
	}
	names := make([]string, len(found))
	for i, m := range found {
		names[i] = chatUserName(m.user.Handle, m.user.Node, m.user.BBS, home)
	}
	// Two boards with one name make the names the same, as recipients are
	// matched: ignoring case. The node IDs, which the hub accepts after the
	// @, tell them apart.
	folded := make([]string, len(names))
	for i, n := range names {
		folded[i] = strings.ToLower(n)
	}
	if len(slices.Compact(slices.Sorted(slices.Values(folded)))) < len(names) {
		for i, m := range found {
			names[i] = m.user.Handle + "@" + m.user.Node
		}
	}
	return "", "", "", fmt.Errorf("%s is here from more than one board; send to one of: %s", found[0].user.Handle, strings.Join(names, ", "))
}

// chatArtReplace fills the @name####@ placeholder in ANSI art data with
// value; see chatArtFill.
func chatArtReplace(data []byte, name, value string) []byte {
	return chatArtFill(data, map[string]func(int) string{name: func(int) string { return value }})
}

// chatArtFill replaces the placeholders in the chat header art in one pass
// over data. A placeholder is "@", an upper-case name, any fill characters
// (the stock art uses "#") and a closing "@"; its length in columns, both
// "@"s included, is the width of the field on screen. For each placeholder
// whose name has an entry in fields, the function is called with that width
// and its result is padded with spaces or cut to exactly that many columns,
// so the art keeps its shape. Unknown placeholders are left as they are.
// Values are never scanned for placeholders themselves, so a topic or room
// name containing "@U@" is shown as typed.
func chatArtFill(data []byte, fields map[string]func(int) string) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if data[i] == '@' {
			if name, span, ok := chatArtPlaceholder(data[i:]); ok {
				if fill, known := fields[name]; known {
					width := utf8.RuneCount(data[i : i+span])
					out = append(out, chatArtPad(fill(width), width)...)
					i += span
					continue
				}
			}
		}
		out = append(out, data[i])
		i++
	}
	return out
}

// chatArtPlaceholder reports whether data starts with a placeholder, and if
// so returns its name and its length in bytes.
func chatArtPlaceholder(data []byte) (string, int, bool) {
	j := 1
	for j < len(data) && data[j] >= 'A' && data[j] <= 'Z' {
		j++
	}
	if j == 1 {
		return "", 0, false
	}
	k := j
	for k < len(data) && data[k] == '#' {
		k++
	}
	if k >= len(data) || data[k] != '@' {
		return "", 0, false
	}
	return string(data[1:j]), k + 1, true
}

// chatArtPad fits value to width columns, cutting whole runes or padding with
// spaces. Values such as a V3Net network name are not ASCII-gated, so this
// counts terminal cells: a wide character takes two.
func chatArtPad(value string, width int) []byte {
	value = runewidth.Truncate(value, width, "")
	repl := []byte(value)
	for n := runewidth.StringWidth(value); n < width; n++ {
		repl = append(repl, ' ')
	}
	return repl
}

// chatCounter formats n for a header counter field width columns wide, the
// way the stock art draws it: a "·" separator, n zero-padded to fill the
// field, then suffix. A value with more digits than fit shows as all nines.
// A field too narrow for the separator gets the bare number. For example,
// chatCounter(4, 3, "") is "·003" and chatCounter(6, 42, "ms") is "·042ms".
func chatCounter(width, n int, suffix string) string {
	digits := width - 1 - len(suffix)
	if digits < 1 {
		return strconv.Itoa(n)
	}
	return "\u00b7" + chatPadNumber(n, digits) + suffix
}

// chatLagField formats a round trip for the header's Lag field, width
// columns wide: as chatCounter in milliseconds with an "ms" suffix when ok,
// and with dashes in place of the digits when there is no measurement, as in
// "·---ms".
func chatLagField(width int, lag time.Duration, ok bool) string {
	if ok {
		return chatCounter(width, int(lag/time.Millisecond), "ms")
	}
	if digits := width - 3; digits >= 1 {
		return "\u00b7" + strings.Repeat("-", digits) + "ms"
	}
	return "-"
}

// chatLocalField fills the Lag field on local chat, where there is no link
// to measure: "·local" when it fits, else as much of "local" as does.
func chatLocalField(width int) string {
	if width >= 6 {
		return "\u00b7local"
	}
	return runewidth.Truncate("local", width, "")
}

// chatLinkField fills the header's link field: blank on local chat, which
// has no link, "?" until the first lag ping has an answer either way, "■"
// while the hub answers and "!" while it does not, so a caller can see
// whether the room is live. The mark sits in the middle of the three
// columns a placeholder takes at the least.
func chatLinkField(pinging, known, ok bool) string {
	switch {
	case !pinging:
		return ""
	case !known:
		return " ? "
	case ok:
		return " \u25a0 "
	}
	return " ! "
}

// chatStamp is the timestamp shown in front of a chat line said at the time
// at, as seen at now: the time of day for a line from today, and the
// month and day, in the same five columns, for one from an earlier day, so
// a room's history is not mistaken for a burst of live chat. A zero at
// means now.
func chatStamp(at, now time.Time) string {
	if at.IsZero() {
		at = now
	}
	at = at.In(now.Location())
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("01/02")
}

// chatBufferField formats the scrollback use for a header field width columns
// wide, as "·used/capacity" with each number zero-padded to half the room
// left; chatBufferField(8, 12, 500) is "·012/500".
func chatBufferField(width, used, capacity int) string {
	digits := (width - 2) / 2
	if digits < 1 {
		return strconv.Itoa(used)
	}
	return "\u00b7" + chatPadNumber(used, digits) + "/" + chatPadNumber(capacity, digits)
}

// chatPadNumber zero-pads n to digits, or shows all nines when it is longer.
func chatPadNumber(n, digits int) string {
	s := fmt.Sprintf("%0*d", digits, max(n, 0))
	if len(s) > digits {
		return strings.Repeat("9", digits)
	}
	return s
}

// chatMentions reports whether text mentions handle: contains it, ignoring
// case, as a whole word, with no letter or digit directly before or after.
// "hey caller!" and "caller's" mention Caller; "callers" does not.
func chatMentions(text, handle string) bool {
	if handle == "" {
		return false
	}
	lower, h := strings.ToLower(text), strings.ToLower(handle)
	for from := 0; ; {
		i := strings.Index(lower[from:], h)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(h)
		before, _ := utf8.DecodeLastRuneInString(lower[:start])
		after, _ := utf8.DecodeRuneInString(lower[end:])
		if !chatWordRune(before) && !chatWordRune(after) {
			return true
		}
		from = start + 1
	}
}

// chatWordRune reports whether r is part of a word for chatMentions.
func chatWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// pipeUnitAt returns the first piece of pipe-code text s as ReplacePipeCodes
// will show it in output mode mode: a colour code, which takes no cells and
// is reported as one; "||", which shows as one "|"; or one character, with
// the cells it takes on the terminal. On a UTF-8 terminal a character is a
// grapheme cluster, so a joined emoji is one unit and a wide character two
// cells. A CP437 terminal is sent one byte per rune, with a "?" for a rune
// the code page lacks, so there every rune is one cell. A byte that is not
// UTF-8 is a CP437 character, one cell wide either way. s must not be empty.
func pipeUnitAt(s string, mode ansi.OutputMode) (unit string, cells int, code bool) {
	if s[0] == '|' {
		if len(s) > 1 && s[1] == '|' {
			return "||", 1, false
		}
		if n := ansi.ColorPipeCodeLen(s); n > 0 {
			return s[:n], 0, true
		}
	}
	if mode == ansi.OutputModeCP437 {
		_, size := utf8.DecodeRuneInString(s)
		return s[:size], 1, false
	}
	unit, _, cells, _ = uniseg.FirstGraphemeClusterInString(s, -1)
	return unit, cells, false
}

// pipeDisplayLen returns the number of terminal cells a pipe-code string
// takes in output mode mode, as ReplacePipeCodes will show it: colour codes
// take no room, "||" takes one, and a character what pipeUnitAt says.
func pipeDisplayLen(s string, mode ansi.OutputMode) int {
	n := 0
	for s != "" {
		unit, cells, _ := pipeUnitAt(s, mode)
		n += cells
		s = s[len(unit):]
	}
	return n
}

// pipePen is the colour a run of pipe-code text leaves in effect: the last
// foreground and background codes seen, each empty once reset. |23 resets
// both; so does a normal foreground code, |00 to |07, which expands to a
// reset before its colour, while a bright one, |08 to |15, leaves the
// background alone.
type pipePen struct {
	fg, bg string
}

// scan applies the colour codes in s to the pen.
func (p *pipePen) scan(s string) {
	for s != "" {
		unit, _, code := pipeUnitAt(s, ansi.OutputModeUTF8)
		s = s[len(unit):]
		switch {
		case !code:
		case unit == "|23":
			p.fg, p.bg = "", ""
		case unit[1] == 'B':
			p.bg = unit
		case unit <= "|07":
			p.fg, p.bg = unit, ""
		default:
			p.fg = unit
		}
	}
}

// restore returns what brings the colour from the pen state from back to p,
// as the escape sequences the codes expand to rather than the codes: a "|B1"
// put before text that starts with a digit would read as "|B10". A changed
// foreground goes first, since one may reset the background.
func (p pipePen) restore(from pipePen) string {
	codes := ""
	if p.fg != from.fg && p.fg != "" {
		codes = p.fg
	}
	if p.bg != "" && (codes != "" || p.bg != from.bg) {
		codes += p.bg
	}
	return string(ansi.ReplacePipeCodes([]byte(codes)))
}

// wrapPipeText wraps pipe-code text to rows of at most width terminal cells
// in output mode mode, measured as pipeDisplayLen does. Continuation rows
// start with contPrefix (contLen cells) and go on in the colour the previous
// row ended in. A word that does not fit on its row moves whole to the next
// one; a word wider than a row fills what is left of its row and is cut at
// the edge, never inside a colour code, a "||" or a character. Spaces
// between words on a row stay as they are; those at a wrap and at the end of
// the text are dropped, so no row ends in them, though the colour codes among
// them still act.
func wrapPipeText(text string, width int, contPrefix string, contLen int, mode ansi.OutputMode) []string {
	width = max(width, 1)
	if pipeDisplayLen(text, mode) <= width {
		return []string{text}
	}
	var pen, prefixPen pipePen
	prefixPen.scan(contPrefix)

	var lines []string
	var cur strings.Builder
	curLen := 0 // cells on the current row
	base := 0   // cells its prefix took
	newRow := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		cur.WriteString(contPrefix)
		cur.WriteString(pen.restore(prefixPen))
		curLen, base = contLen, contLen
	}
	put := func(s string, cells int) {
		cur.WriteString(s)
		pen.scan(s)
		curLen += cells
	}
	// Spaces wait until the word after them is placed: on the same row they
	// go in as they are, otherwise only their colour codes do.
	gap, gapLen := "", 0
	flushGap := func(keep bool) {
		if keep {
			put(gap, gapLen)
		} else {
			for gap != "" {
				unit, _, code := pipeUnitAt(gap, mode)
				if code {
					put(unit, 0)
				}
				gap = gap[len(unit):]
			}
		}
		gap, gapLen = "", 0
	}

	for text != "" {
		// The next token: a run of spaces or a word, with the colour codes
		// among them. A space with a combining mark on it is a space.
		space := text[0] == ' '
		end, cells := 0, 0
		for end < len(text) {
			unit, n, code := pipeUnitAt(text[end:], mode)
			if !code && (unit[0] == ' ') != space && end > 0 {
				break
			}
			end += len(unit)
			cells += n
		}
		tok := text[:end]
		text = text[end:]

		switch {
		case space:
			gap, gapLen = gap+tok, gapLen+cells
		case curLen+gapLen+cells <= width:
			flushGap(true)
			put(tok, cells)
		case contLen+cells <= width:
			flushGap(false)
			newRow()
			put(tok, cells)
		default:
			// Wider than a row: fill this one and cut at the edge. The gap
			// goes in only if the word's first cells follow it on the row.
			_, n, _ := pipeUnitAt(tok, mode)
			flushGap(curLen+gapLen+n <= width)
			for tok != "" {
				unit, n, _ := pipeUnitAt(tok, mode)
				if curLen+n > width && curLen > base {
					newRow()
				}
				put(unit, n)
				tok = tok[len(unit):]
			}
		}
	}
	flushGap(false)
	return append(lines, cur.String())
}
