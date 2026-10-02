package menu

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// ChatLeafProvider supplies active V3Net chat leaf connections.
type ChatLeafProvider interface {
	ActiveChatLeaves() []ChatLeafInfo
}

// ChatLeafInfo describes a single V3Net chat leaf that can create sessions.
type ChatLeafInfo struct {
	NetworkName string
	NewSession  func(handle string) chat.ChatService
}

// chatSelectService shows an interactive network picker (when multiple V3Net
// networks are configured) and a room picker, then returns the connected
// ChatService and the room to join. The pickers run in normal terminal mode;
// the caller sets up the full-screen chat UI afterwards.
func chatSelectService(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, handle string, outputMode ansi.OutputMode) (chat.ChatService, string, string, error) {
	dbPath := e.GetServerConfig().DataDir + "/chat.db"

	wt := func(text string) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
	}

	var leaves []ChatLeafInfo
	if e.ChatLeaves != nil {
		leaves = e.ChatLeaves.ActiveChatLeaves()
	}

	var svc chat.ChatService
	var netName string
	var err error

	switch {
	case len(leaves) == 0:
		svc, err = chat.NewLocalChatService(handle, dbPath)
		if err != nil {
			return nil, "", "", err
		}
		netName = "Local"
	default:
		svc, netName, err = chatNetworkPicker(e, s, terminal, handle, dbPath, leaves, outputMode, wt)
		if err != nil || svc == nil {
			return nil, "", "", err
		}
	}

	room := chatRoomPicker(e, svc, s, terminal, wt)
	return svc, room, netName, nil
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
	nets := make([]chatNetInfo, len(leaves)+1)
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
			go func() {
				defer probe.Close() //nolint:errcheck
				rooms, err := probe.Rooms()
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
	nets[len(leaves)] = chatNetInfo{name: "Local", users: -1, avail: true, isLocal: true}
	return nets
}

// chatNetworkPicker displays a numbered network list, probes each for user
// counts, and returns the ChatService for the selected network.
func chatNetworkPicker(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, handle, dbPath string, leaves []ChatLeafInfo, outputMode ansi.OutputMode, wt func(string)) (chat.ChatService, string, error) {
	nets := probeChatNetworks(leaves, handle)

	wt("\r\n" + e.Strings().ChatNetworkPickerHeader + "\r\n")
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
		wt(fmt.Sprintf(e.Strings().ChatNetworkPickerEntry+"\r\n", i+1, net.name, status))
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
// Returns "lobby" if no rooms exist yet or the user presses Enter.
func chatRoomPicker(e *MenuExecutor, svc chat.ChatService, s ssh.Session, terminal *term.Terminal, wt func(string)) string {
	rooms, err := svc.Rooms()
	if err != nil || len(rooms) == 0 {
		return "lobby"
	}

	wt("\r\n" + e.Strings().ChatRoomListHeader + "\r\n")
	for _, r := range rooms {
		topic := r.Topic
		if topic == "" {
			topic = "|08no topic|07"
		}
		wt(fmt.Sprintf(e.Strings().ChatRoomListEntry+"\r\n", r.Name, r.UserCount, topic))
	}
	wt("\r\n")
	wt("|07Select room |08[|07lobby|08]|07: ")

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return "lobby"
	}
	room, nameErr := chatRoomChoice(input, rooms)
	if nameErr != nil {
		wt("\r\n|07" + nameErr.Error() + " - joining lobby.\r\n")
	}
	return room
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

// chatNoRoomMsg is shown when the caller is in no room, after a /JOIN whose
// fallback rejoin of the previous room also failed.
const chatNoRoomMsg = "You are not in a room. Use /JOIN <room> to join one."

// chatContPrefix is the visual continuation-line prefix for word-wrapped messages (8 visible chars).
const chatContPrefix = "|08      \xC0|07 "
const chatContPrefixLen = 8

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

	// Screen layout (Retrograde MRC-style, 5-row ANSI art header):
	//
	//   Rows 1-5        : ANSI art header (room / topic / decorative status)
	//   Rows 6..sb      : chat scroll region  (sb = height-4)
	//   Row height-3    : status bar — users
	//   Row height-2    : status bar — hints
	//   Row height-1    : input prompt   ← NOT the last row
	//   Row height      : absorb buffer  (readline \r\n lands here, no screen scroll)
	//
	// Keeping input at height-1 rather than height prevents term.Terminal's
	// post-Enter \r\n from triggering a full-screen scroll that would destroy
	// the header and status bar.

	// Screen layout (Retrograde MRC-style):
	//   Rows 1-5        : ANSI art header
	//   Rows 6..sb      : chat scroll region  (sb = height-3)
	//   Row height-2    : status bar — users
	//   Row height-1    : status bar — hints
	//   Row height      : input prompt (last row; safe because chatReadLine never writes \n there)
	const chatHeaderRows = 5
	chatTop := chatHeaderRows + 1 // row 6
	scrollBottom := height - 3    // last row of chat scroll region
	if scrollBottom < chatTop {
		scrollBottom = chatTop
	}
	chatInputRow := height // input at the last visible row

	// Network and room selection — runs in normal terminal mode before full-screen UI.
	svc, selectedRoom, selectedNetwork, err := chatSelectService(e, s, terminal, handle, outputMode)
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
	currentUsers := []string{}

	var rawMu sync.Mutex

	rawWriteLocked := func(data []byte) {
		terminalio.WriteProcessedBytes(s, data, outputMode)
	}

	rawWrite := func(data []byte) {
		rawMu.Lock()
		defer rawMu.Unlock()
		rawWriteLocked(data)
	}

	// drawHeaderLocked redraws the 5-row ANSI art header. Caller must hold rawMu.
	// It loads CHATHEADER.ANS from the menu set's ansi directory, substituting
	// @MRCROOM@ and @MRCTOPIC@ placeholders with the current room and topic.
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
			artData = chatArtReplace(artData, "NET", currentNetwork)
			artData = chatArtReplace(artData, "ROOM", currentRoom)
			artData = chatArtReplace(artData, "TOPIC", currentTopic)
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
			roomLine := " |15#" + currentRoom
			if currentTopic != "" {
				roomLine += " |08/ |07" + currentTopic
			}
			rawWriteLocked([]byte(ansi.MoveCursor(2, 1)))
			rawWriteLocked(ansi.ReplacePipeCodes([]byte(roomLine)))
			rawWriteLocked([]byte(ansi.MoveCursor(3, 1)))
			rawWriteLocked(sep)
			for row := 4; row <= chatHeaderRows; row++ {
				rawWriteLocked([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K", row)))
			}
		}
		// Return cursor to input row.
		rawWriteLocked([]byte(ansi.MoveCursor(chatInputRow, 1)))
	}

	// drawStatusBarLocked redraws the 2-row status bar. Caller must hold rawMu.
	drawStatusBarLocked := func() {
		sep := ansi.ReplacePipeCodes([]byte(e.Strings().ChatSeparator))
		// Users line
		rawWriteLocked([]byte(ansi.MoveCursor(height-2, 1)))
		rawWriteLocked(sep)
		userStr := strings.Join(currentUsers, ", ")
		if userStr == "" {
			userStr = handle
		}
		rawWriteLocked([]byte(ansi.MoveCursor(height-2, 2)))
		rawWriteLocked(ansi.ReplacePipeCodes([]byte(fmt.Sprintf(" |07Users: %s", userStr))))
		// Hints line
		rawWriteLocked([]byte(ansi.MoveCursor(height-1, 1)))
		rawWriteLocked(sep)
		rawWriteLocked([]byte(ansi.MoveCursor(height-1, 2)))
		rawWriteLocked(ansi.ReplacePipeCodes([]byte(" |08/rooms /join /msg /topic /network /q")))
		// Return cursor to input row.
		rawWriteLocked([]byte(ansi.MoveCursor(chatInputRow, 1)))
	}

	// writeChatLineLocked writes a timestamped, word-wrapped message into the
	// chat scroll region. Caller must hold rawMu.
	writeChatLineLocked := func(text string) {
		ts := time.Now().Format("15:04")
		fullText := fmt.Sprintf("|08%s|07 %s", ts, text)
		lines := wrapPipeText(fullText, termWidth, chatContPrefix, chatContPrefixLen)
		n := len(lines)
		if n == 0 {
			return
		}
		// Scroll the chat region up by n lines.
		rawWriteLocked([]byte(ansi.MoveCursor(scrollBottom, 1)))
		for i := 0; i < n; i++ {
			rawWriteLocked([]byte("\r\n"))
		}
		// Write each line at its resulting position.
		for i, line := range lines {
			rawWriteLocked([]byte(ansi.MoveCursor(scrollBottom-n+1+i, 1)))
			rawWriteLocked(ansi.ReplacePipeCodes([]byte(line)))
		}
		// Return cursor to input row (chatReadLine will reposition precisely on next key).
		rawWriteLocked([]byte(ansi.MoveCursor(chatInputRow, 1)))
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
	terminalio.WriteProcessedBytes(s, []byte(fmt.Sprintf("\x1B[%d;%dr", chatTop, scrollBottom)), outputMode)

	_, history, err := svc.Join(currentRoom)
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

	// Draw header and status bar with initial state.
	currentUsers = svc.Users()
	rawMu.Lock()
	drawHeaderLocked()
	drawStatusBarLocked()
	rawMu.Unlock()

	// Show join notice and scrollback history.
	writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Joined #"+currentRoom))
	for _, msg := range history {
		writeChatLine(formatChatMessage(msg, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
	}

	// Draw initial input prompt. chatReadLine will manage it from here on.
	chatPrompt := fmt.Sprintf("<%s> ", handle)
	rawWrite([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K%s", chatInputRow, chatPrompt)))

	// Goroutine to receive and display incoming events.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range svc.Events() {
			switch ev.Type {
			case chat.TypeMessage:
				if ev.Message != nil {
					writeChatLine(formatChatMessage(*ev.Message, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
				}
			case chat.TypePrivate:
				if ev.Message != nil {
					writeChatLine(fmt.Sprintf(e.Strings().ChatPrivateMsgFormat, ev.Message.Handle, ev.Message.Text))
				}
			case chat.TypeJoin:
				if ev.Join != nil {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					writeChatLineLocked(fmt.Sprintf(e.Strings().ChatJoinMsg, ev.Join.Handle, ev.Join.Room))
					drawStatusBarLocked()
					rawMu.Unlock()
				}
			case chat.TypeLeave:
				if ev.Leave != nil {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					writeChatLineLocked(fmt.Sprintf(e.Strings().ChatLeaveMsg, ev.Leave.Handle, ev.Leave.Room))
					drawStatusBarLocked()
					rawMu.Unlock()
				}
			case chat.TypeTopic:
				if ev.Topic != nil {
					rawMu.Lock()
					currentTopic = ev.Topic.Topic
					writeChatLineLocked(fmt.Sprintf(e.Strings().ChatTopicMsg, ev.Topic.Room, ev.Topic.Topic))
					drawHeaderLocked()
					rawMu.Unlock()
				}
			case chat.TypeSystem:
				if ev.Reconnect {
					writeChatLine(e.Strings().ChatReconnected)
				} else if ev.Text != "" {
					writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, ev.Text))
				}
			}
		}
	}()

	cleanup := func() {
		if currentRoom != "" {
			svc.Leave(currentRoom) //nolint:errcheck
		}
		svc.Close() //nolint:errcheck
		<-done
	}

	for {
		input, err := chatReadLine(s, &rawMu, rawWriteLocked, chatInputRow, termWidth, chatPrompt)
		if err != nil {
			if err == io.EOF {
				cleanup()
				rawWrite([]byte("\x1B[r"))
				terminal.SetPrompt("")
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("chat input error", "node", nodeNumber, "error", err)
			break
		}

		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			continue
		}

		upper := strings.ToUpper(trimmed)
		if upper == "/Q" || upper == "/QUIT" {
			break
		}

		if strings.HasPrefix(upper, "/JOIN ") {
			// Normalise up front, as Join would: a name the service will
			// refuse is rejected without leaving the current room, and
			// currentRoom then names the room actually joined, which later
			// posts and the final Leave are addressed to.
			newRoom, nameErr := chat.NormalizeRoom(strings.TrimSpace(trimmed[6:]))
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
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not join room: "+joinErr.Error()))
				rejoined := false
				if oldRoom != "" {
					if _, _, rejoinErr := svc.Join(oldRoom); rejoinErr != nil {
						writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not rejoin #"+oldRoom+": "+rejoinErr.Error()))
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
					drawHeaderLocked()
				}
				currentUsers = svc.Users()
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
			currentUsers = svc.Users()
			drawHeaderLocked()
			drawStatusBarLocked()
			rawMu.Unlock()
			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Joined #"+currentRoom))
			for _, msg := range joinHistory {
				writeChatLine(formatChatMessage(msg, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
			}
			continue
		}

		if upper == "/NETWORK" {
			// Stop the current service and event goroutine.
			cleanup()

			// Probe available networks inline.
			var leaves []ChatLeafInfo
			if e.ChatLeaves != nil {
				leaves = e.ChatLeaves.ActiveChatLeaves()
			}
			dbPath := e.GetServerConfig().DataDir + "/chat.db"
			nets := probeChatNetworks(leaves, handle)

			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Chat Networks:"))
			for i, net := range nets {
				switch {
				case !net.avail:
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-15s |08(unavailable)|07", i+1, net.name))
				case net.isLocal:
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-15s |08(this BBS only)|07", i+1, net.name))
				default:
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-15s |08(%d users online)|07", i+1, net.name, net.users))
				}
			}

			netInput, netErr := chatReadLine(s, &rawMu, rawWriteLocked, chatInputRow, termWidth, "Select network [1]: ")
			var newSvc chat.ChatService
			var newNetName string
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
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-12s |08(%d)|07 %s", i+1, r.Name, r.UserCount, topic))
				}
				roomInput, roomErr2 := chatReadLine(s, &rawMu, rawWriteLocked, chatInputRow, termWidth, "Select room [lobby]: ")
				if roomErr2 == nil {
					var nameErr error
					if newRoom, nameErr = chatRoomChoice(roomInput, netRooms); nameErr != nil {
						writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, nameErr.Error()+" - joining lobby."))
					}
				}
			}

			// Join the new room.
			_, newHistory, joinErr := newSvc.Join(newRoom)
			if joinErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not join room: "+joinErr.Error()))
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
			done = make(chan struct{})
			rawMu.Lock()
			currentTopic = ""
			currentUsers = svc.Users()
			drawHeaderLocked()
			for row := chatTop; row <= scrollBottom; row++ {
				rawWriteLocked([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K", row)))
			}
			drawStatusBarLocked()
			rawMu.Unlock()

			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Joined #"+currentRoom))
			for _, msg := range newHistory {
				writeChatLine(formatChatMessage(msg, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
			}

			go func() {
				defer close(done)
				for ev := range svc.Events() {
					switch ev.Type {
					case chat.TypeMessage:
						if ev.Message != nil {
							writeChatLine(formatChatMessage(*ev.Message, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
						}
					case chat.TypePrivate:
						if ev.Message != nil {
							writeChatLine(fmt.Sprintf(e.Strings().ChatPrivateMsgFormat, ev.Message.Handle, ev.Message.Text))
						}
					case chat.TypeJoin:
						if ev.Join != nil {
							newUsers := svc.Users()
							rawMu.Lock()
							currentUsers = newUsers
							writeChatLineLocked(fmt.Sprintf(e.Strings().ChatJoinMsg, ev.Join.Handle, ev.Join.Room))
							drawStatusBarLocked()
							rawMu.Unlock()
						}
					case chat.TypeLeave:
						if ev.Leave != nil {
							newUsers := svc.Users()
							rawMu.Lock()
							currentUsers = newUsers
							writeChatLineLocked(fmt.Sprintf(e.Strings().ChatLeaveMsg, ev.Leave.Handle, ev.Leave.Room))
							drawStatusBarLocked()
							rawMu.Unlock()
						}
					case chat.TypeTopic:
						if ev.Topic != nil {
							rawMu.Lock()
							currentTopic = ev.Topic.Topic
							writeChatLineLocked(fmt.Sprintf(e.Strings().ChatTopicMsg, ev.Topic.Room, ev.Topic.Topic))
							drawHeaderLocked()
							rawMu.Unlock()
						}
					case chat.TypeSystem:
						if ev.Reconnect {
							writeChatLine(e.Strings().ChatReconnected)
						} else if ev.Text != "" {
							writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, ev.Text))
						}
					}
				}
			}()
			continue
		}

		if upper == "/ROOMS" {
			rooms, roomErr := svc.Rooms()
			if roomErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not list rooms: "+roomErr.Error()))
			} else {
				writeChatLine(e.Strings().ChatRoomListHeader)
				for _, r := range rooms {
					writeChatLine(fmt.Sprintf(e.Strings().ChatRoomListEntry, r.Name, r.UserCount, r.Topic))
				}
			}
			continue
		}

		if strings.HasPrefix(upper, "/TOPIC ") {
			if currentRoom == "" {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatNoRoomMsg))
				continue
			}
			topicText := strings.TrimSpace(trimmed[7:])
			if topicErr := svc.SetTopic(currentRoom, topicText); topicErr != nil {
				writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not set topic: "+topicErr.Error()))
			}
			continue
		}

		if strings.HasPrefix(upper, "/MSG ") {
			rest := strings.TrimSpace(trimmed[5:])
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 2 {
				target := strings.TrimSpace(parts[0])
				message := parts[1]
				toHandle := target
				toNode := ""
				if atIdx := strings.Index(target, "@"); atIdx > 0 && atIdx < len(target)-1 {
					toHandle = target[:atIdx]
					toNode = target[atIdx+1:]
				}
				if msgErr := svc.Private(toHandle, toNode, message); msgErr != nil {
					writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not send private message: "+msgErr.Error()))
				}
			}
			continue
		}

		if upper == "/USERS" {
			rawMu.Lock()
			currentUsers = svc.Users()
			drawStatusBarLocked()
			rawMu.Unlock()
			continue
		}

		if currentRoom == "" {
			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, chatNoRoomMsg))
			continue
		}
		if postErr := svc.Post(currentRoom, trimmed); postErr != nil {
			writeChatLine(fmt.Sprintf(e.Strings().ChatSystemPrefix, "Could not post: "+postErr.Error()))
			continue
		}

		// Echo own message locally.
		ownMsg := chat.ChatMessage{Handle: handle, Text: trimmed, Timestamp: time.Now()}
		writeChatLine(formatChatMessage(ownMsg, e.Strings().ChatSystemPrefix, e.Strings().ChatMessageFormat))
	}

	cleanup()
	rawWrite([]byte("\x1B[r"))
	terminal.SetPrompt("")
	return nil, "", nil
}

func formatChatMessage(msg chat.ChatMessage, systemFmt, userFmt string) string {
	if msg.IsSystem {
		return fmt.Sprintf(systemFmt, msg.Text)
	}
	return fmt.Sprintf(userFmt, msg.Handle, msg.Text)
}

// chatArtReplace finds the MCI placeholder @name####...####@ in ANSI art data
// and replaces it with value padded (or truncated) to the same byte length,
// preserving the visual width of the art.
func chatArtReplace(data []byte, name, value string) []byte {
	tag := []byte("@" + name)
	start := bytes.Index(data, tag)
	if start < 0 {
		return data
	}
	rest := data[start+len(tag):]
	end := bytes.IndexByte(rest, '@')
	if end < 0 {
		return data
	}
	totalLen := len(tag) + end + 1 // placeholder span; ASCII, so bytes == columns
	// Fill by columns, not bytes: the NET value is a V3Net leaf network name and
	// is not ASCII-gated, so byte padding would leave the row short and byte
	// truncation would cut a rune in half.
	runes := []rune(value)
	if len(runes) > totalLen {
		runes = runes[:totalLen]
	}
	repl := []byte(string(runes))
	for utf8.RuneCount(repl) < totalLen {
		repl = append(repl, ' ')
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:start]...)
	out = append(out, repl...)
	out = append(out, data[start+totalLen:]...)
	return out
}

// chatReadLine reads a line using the session's InputHandler (same path as all
// other BBS input), echoing at the given row. All terminal writes go through
// mu+writeFn so they are serialised with the event goroutine's writes.
func chatReadLine(s ssh.Session, mu *sync.Mutex, writeFn func([]byte), row, termWidth int, prompt string) (string, error) {
	ih := getSessionIH(s)
	promptLen := len([]rune(prompt))
	maxBuf := termWidth - promptLen - 1
	if maxBuf < 1 {
		maxBuf = 1
	}

	var buf []rune

	redraw := func() {
		input := string(buf)
		curCol := promptLen + len(buf) + 1
		seq := fmt.Sprintf("\x1B[%d;1H\x1B[2K%s%s\x1B[%d;%dH", row, prompt, input, row, curCol)
		mu.Lock()
		writeFn([]byte(seq))
		mu.Unlock()
	}

	redraw()

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", io.EOF
		}

		switch key {
		case editor.KeyEnter:
			line := strings.TrimSpace(string(buf))
			buf = buf[:0]
			mu.Lock()
			writeFn([]byte(fmt.Sprintf("\x1B[%d;1H\x1B[2K%s\x1B[%d;%dH", row, prompt, row, promptLen+1)))
			mu.Unlock()
			return line, nil

		case editor.KeyBackspace:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				redraw()
			}

		case editor.KeyEsc, editor.KeyCtrlC, editor.KeyCtrlA:
			return "", io.EOF

		default:
			if editor.IsPrintable(key) && len(buf) < maxBuf {
				buf = append(buf, rune(key))
				redraw()
			}
		}
	}
}

// pipeDisplayLen returns the number of visible characters in a pipe-code string,
// skipping |XX color codes.
func pipeDisplayLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if i+2 < len(s) && s[i] == '|' && s[i+1] >= '0' && s[i+1] <= '9' && s[i+2] >= '0' && s[i+2] <= '9' {
			i += 3
		} else {
			n++
			i++
		}
	}
	return n
}

// wrapPipeText wraps pipe-code text to fit within width visible characters.
// Continuation lines are prefixed with contPrefix (contLen visible characters).
func wrapPipeText(text string, width int, contPrefix string, contLen int) []string {
	if pipeDisplayLen(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	var cur strings.Builder
	curLen := 0
	for _, word := range words {
		wLen := pipeDisplayLen(word)
		if curLen == 0 {
			cur.WriteString(word)
			curLen = wLen
		} else if curLen+1+wLen <= width {
			cur.WriteByte(' ')
			cur.WriteString(word)
			curLen += 1 + wLen
		} else {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(contPrefix)
			cur.WriteString(word)
			curLen = contLen + wLen
		}
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
