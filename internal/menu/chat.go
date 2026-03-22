package menu

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
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
	dbPath := e.ServerCfg.DataDir + "/chat.db"

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
		svc, netName, err = chatNetworkPicker(s, terminal, handle, dbPath, leaves, outputMode, wt)
		if err != nil || svc == nil {
			return nil, "", "", err
		}
	}

	room := chatRoomPicker(svc, s, terminal, wt)
	return svc, room, netName, nil
}

// chatNetworkPicker displays a numbered network list, probes each for user
// counts (2 s timeout), and returns the ChatService for the selected network.
func chatNetworkPicker(s ssh.Session, terminal *term.Terminal, handle, dbPath string, leaves []ChatLeafInfo, outputMode ansi.OutputMode, wt func(string)) (chat.ChatService, string, error) {
	type netInfo struct {
		name    string
		users   int
		avail   bool
		isLocal bool
	}

	nets := make([]netInfo, len(leaves)+1)
	var wg sync.WaitGroup
	for i, leaf := range leaves {
		i, leaf := i, leaf
		nets[i].name = leaf.NetworkName
		nets[i].avail = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			probe := leaf.NewSession(handle)
			defer probe.Close() //nolint:errcheck
			ch := make(chan []chat.RoomInfo, 1)
			go func() {
				rooms, err := probe.Rooms()
				if err == nil {
					ch <- rooms
				} else {
					ch <- nil
				}
			}()
			select {
			case rooms := <-ch:
				for _, room := range rooms {
					nets[i].users += room.UserCount
				}
			case <-time.After(2 * time.Second):
			}
		}()
	}
	wg.Wait()
	nets[len(leaves)] = netInfo{name: "Local", users: -1, avail: true, isLocal: true}

	wt("\r\n|15Chat Networks|07\r\n")
	wt("|08────────────────────────────────────────|07\r\n")
	for i, net := range nets {
		switch {
		case !net.avail:
			wt(fmt.Sprintf("|08 %d.|07 %-20s |08(unavailable)|07\r\n", i+1, net.name))
		case net.isLocal:
			wt(fmt.Sprintf("|08 %d.|07 %-20s |08(this BBS only)|07\r\n", i+1, net.name))
		default:
			wt(fmt.Sprintf("|08 %d.|07 %-20s |08(%d users online)|07\r\n", i+1, net.name, net.users))
		}
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
func chatRoomPicker(svc chat.ChatService, s ssh.Session, terminal *term.Terminal, wt func(string)) string {
	rooms, err := svc.Rooms()
	if err != nil || len(rooms) == 0 {
		return "lobby"
	}

	wt("\r\n|15Available Rooms|07\r\n")
	wt("|08────────────────────────────────────────|07\r\n")
	for i, r := range rooms {
		topic := r.Topic
		if topic == "" {
			topic = "|08no topic|07"
		}
		wt(fmt.Sprintf("|08 %d.|07 %-15s |08(%d)|07 %s\r\n", i+1, r.Name, r.UserCount, topic))
	}
	wt("\r\n")
	wt("|07Select room |08[|07lobby|08]|07: ")

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil || strings.TrimSpace(input) == "" {
		return "lobby"
	}
	trimmed := strings.TrimSpace(input)
	if n, parseErr := strconv.Atoi(trimmed); parseErr == nil && n >= 1 && n <= len(rooms) {
		return rooms[n-1].Name
	}
	return trimmed
}

// chatContPrefix is the visual continuation-line prefix for word-wrapped messages (8 visible chars).
const chatContPrefix = "|08      \xC0|07 "
const chatContPrefixLen = 8

func runChat(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	handle := currentUser.Handle

	if termWidth <= 0 {
		termWidth = 80
	}

	height := 24
	if termHeight > 0 {
		height = termHeight
	} else if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
		sess.Mutex.RLock()
		if sess.Height > 0 {
			height = sess.Height
		}
		sess.Mutex.RUnlock()
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
	scrollBottom := height - 3   // last row of chat scroll region
	if scrollBottom < chatTop {
		scrollBottom = chatTop
	}
	chatInputRow := height // input at the last visible row

	// Network and room selection — runs in normal terminal mode before full-screen UI.
	svc, selectedRoom, selectedNetwork, err := chatSelectService(e, s, terminal, handle, outputMode)
	if err != nil {
		log.Printf("ERROR: Node %d: chat setup failed: %v", nodeNumber, err)
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
		artPath := filepath.Join(e.MenuSetPath, "ansi", "CHATHEADER.ANS")
		artData, artErr := ansi.GetAnsiFileContent(artPath)
		if artErr == nil {
			// Replace MCI placeholders (ASCII — safe before CP437 conversion).
			artData = chatArtReplace(artData, "NET", currentNetwork)
			artData = chatArtReplace(artData, "ROOM", currentRoom)
			artData = chatArtReplace(artData, "TOPIC", currentTopic)
			// Convert CP437 high bytes to UTF-8 so box-drawing chars render
			// correctly on UTF-8 SSH terminals.
			artData = ansi.ConvertCP437ToUTF8(artData)
			// Split into lines (SAUCE already stripped by GetAnsiFileContent).
			lines := strings.Split(string(artData), "\n")
			for i := 0; i < chatHeaderRows && i < len(lines); i++ {
				line := strings.TrimRight(lines[i], "\r")
				rawWriteLocked([]byte(fmt.Sprintf("\x1B[%d;1H", i+1)))
				rawWriteLocked([]byte(line))
			}
		} else {
			// Fallback: simple text header using the separator string.
			sep := ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ChatSeparator))
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
		sep := ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ChatSeparator))
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
		log.Printf("INFO: Node %d: chat join failed (%v), falling back to local chat", nodeNumber, err)
		svc.Close() //nolint:errcheck
		dbPath := e.ServerCfg.DataDir + "/chat.db"
		var localErr error
		svc, localErr = chat.NewLocalChatService(handle, dbPath)
		if localErr != nil {
			log.Printf("ERROR: Node %d: local chat fallback failed: %v", nodeNumber, localErr)
			return nil, "", nil
		}
		_, history, err = svc.Join(currentRoom)
		if err != nil {
			log.Printf("ERROR: Node %d: local chat join failed: %v", nodeNumber, err)
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
	writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Joined #"+currentRoom))
	for _, msg := range history {
		writeChatLine(formatChatMessage(msg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
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
					writeChatLine(formatChatMessage(*ev.Message, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
				}
			case chat.TypePrivate:
				if ev.Message != nil {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatPrivateMsgFormat, ev.Message.Handle, ev.Message.Text))
				}
			case chat.TypeJoin:
				if ev.Join != nil {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					writeChatLineLocked(fmt.Sprintf(e.LoadedStrings.ChatJoinMsg, ev.Join.Handle, ev.Join.Room))
					drawStatusBarLocked()
					rawMu.Unlock()
				}
			case chat.TypeLeave:
				if ev.Leave != nil {
					newUsers := svc.Users()
					rawMu.Lock()
					currentUsers = newUsers
					writeChatLineLocked(fmt.Sprintf(e.LoadedStrings.ChatLeaveMsg, ev.Leave.Handle, ev.Leave.Room))
					drawStatusBarLocked()
					rawMu.Unlock()
				}
			case chat.TypeTopic:
				if ev.Topic != nil {
					rawMu.Lock()
					currentTopic = ev.Topic.Topic
					writeChatLineLocked(fmt.Sprintf(e.LoadedStrings.ChatTopicMsg, ev.Topic.Room, ev.Topic.Topic))
					drawHeaderLocked()
					rawMu.Unlock()
				}
			case chat.TypeSystem:
				if ev.Reconnect {
					writeChatLine(e.LoadedStrings.ChatReconnected)
				} else if ev.Text != "" {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, ev.Text))
				}
			}
		}
	}()

	cleanup := func() {
		svc.Leave(currentRoom) //nolint:errcheck
		svc.Close()            //nolint:errcheck
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
			log.Printf("ERROR: Node %d: Chat input error: %v", nodeNumber, err)
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
			newRoom := strings.TrimSpace(trimmed[6:])
			if newRoom != "" {
				svc.Leave(currentRoom) //nolint:errcheck
				currentRoom = newRoom
				_, joinHistory, joinErr := svc.Join(currentRoom)
				if joinErr != nil {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not join room: "+joinErr.Error()))
				} else {
					rawMu.Lock()
					currentTopic = ""
					currentUsers = svc.Users()
					drawHeaderLocked()
					drawStatusBarLocked()
					rawMu.Unlock()
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Joined #"+currentRoom))
					for _, msg := range joinHistory {
						writeChatLine(formatChatMessage(msg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
					}
				}
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
			dbPath := e.ServerCfg.DataDir + "/chat.db"

			type netInfo struct {
				name    string
				users   int
				avail   bool
				isLocal bool
			}
			nets := make([]netInfo, len(leaves)+1)
			var wg2 sync.WaitGroup
			for i, leaf := range leaves {
				i, leaf := i, leaf
				nets[i].name = leaf.NetworkName
				nets[i].avail = true
				wg2.Add(1)
				go func() {
					defer wg2.Done()
					probe := leaf.NewSession(handle)
					defer probe.Close() //nolint:errcheck
					ch := make(chan []chat.RoomInfo, 1)
					go func() {
						rooms, err := probe.Rooms()
						if err == nil {
							ch <- rooms
						} else {
							ch <- nil
						}
					}()
					select {
					case rooms := <-ch:
						for _, r := range rooms {
							nets[i].users += r.UserCount
						}
					case <-time.After(2 * time.Second):
					}
				}()
			}
			wg2.Wait()
			nets[len(leaves)] = netInfo{name: "Local", users: -1, avail: true, isLocal: true}

			writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Chat Networks:"))
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
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Network unavailable, using local."))
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
				log.Printf("ERROR: Node %d: /network reconnect failed: %v", nodeNumber, err)
				rawWrite([]byte("\x1B[r"))
				return nil, "", nil
			}

			// Room picker inline.
			newRoom := "lobby"
			if netRooms, roomErr := newSvc.Rooms(); roomErr == nil && len(netRooms) > 0 {
				writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Available Rooms:"))
				for i, r := range netRooms {
					topic := r.Topic
					if topic == "" {
						topic = "no topic"
					}
					writeChatLine(fmt.Sprintf("|08 %d.|07 %-12s |08(%d)|07 %s", i+1, r.Name, r.UserCount, topic))
				}
				roomInput, roomErr2 := chatReadLine(s, &rawMu, rawWriteLocked, chatInputRow, termWidth, "Select room [lobby]: ")
				if roomErr2 == nil {
					if t := strings.TrimSpace(roomInput); t != "" {
						if n, parseErr := strconv.Atoi(t); parseErr == nil && n >= 1 && n <= len(netRooms) {
							newRoom = netRooms[n-1].Name
						} else {
							newRoom = t
						}
					}
				}
			}

			// Join the new room.
			_, newHistory, joinErr := newSvc.Join(newRoom)
			if joinErr != nil {
				writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not join room: "+joinErr.Error()))
				newSvc.Close() //nolint:errcheck
				newSvc, err = chat.NewLocalChatService(handle, dbPath)
				if err != nil {
					rawWrite([]byte("\x1B[r"))
					return nil, "", nil
				}
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

			writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Joined #"+currentRoom))
			for _, msg := range newHistory {
				writeChatLine(formatChatMessage(msg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
			}

			go func() {
				defer close(done)
				for ev := range svc.Events() {
					switch ev.Type {
					case chat.TypeMessage:
						if ev.Message != nil {
							writeChatLine(formatChatMessage(*ev.Message, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
						}
					case chat.TypePrivate:
						if ev.Message != nil {
							writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatPrivateMsgFormat, ev.Message.Handle, ev.Message.Text))
						}
					case chat.TypeJoin:
						if ev.Join != nil {
							newUsers := svc.Users()
							rawMu.Lock()
							currentUsers = newUsers
							writeChatLineLocked(fmt.Sprintf(e.LoadedStrings.ChatJoinMsg, ev.Join.Handle, ev.Join.Room))
							drawStatusBarLocked()
							rawMu.Unlock()
						}
					case chat.TypeLeave:
						if ev.Leave != nil {
							newUsers := svc.Users()
							rawMu.Lock()
							currentUsers = newUsers
							writeChatLineLocked(fmt.Sprintf(e.LoadedStrings.ChatLeaveMsg, ev.Leave.Handle, ev.Leave.Room))
							drawStatusBarLocked()
							rawMu.Unlock()
						}
					case chat.TypeTopic:
						if ev.Topic != nil {
							rawMu.Lock()
							currentTopic = ev.Topic.Topic
							writeChatLineLocked(fmt.Sprintf(e.LoadedStrings.ChatTopicMsg, ev.Topic.Room, ev.Topic.Topic))
							drawHeaderLocked()
							rawMu.Unlock()
						}
					case chat.TypeSystem:
						if ev.Reconnect {
							writeChatLine(e.LoadedStrings.ChatReconnected)
						} else if ev.Text != "" {
							writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, ev.Text))
						}
					}
				}
			}()
			continue
		}

		if upper == "/ROOMS" {
			rooms, roomErr := svc.Rooms()
			if roomErr != nil {
				writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not list rooms: "+roomErr.Error()))
			} else {
				writeChatLine(e.LoadedStrings.ChatRoomListHeader)
				for _, r := range rooms {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatRoomListEntry, r.Name, r.UserCount, r.Topic))
				}
			}
			continue
		}

		if strings.HasPrefix(upper, "/TOPIC ") {
			topicText := strings.TrimSpace(trimmed[7:])
			if topicErr := svc.SetTopic(currentRoom, topicText); topicErr != nil {
				writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not set topic: "+topicErr.Error()))
			}
			continue
		}

		if strings.HasPrefix(upper, "/MSG ") {
			rest := strings.TrimSpace(trimmed[5:])
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 2 {
				if msgErr := svc.Private(parts[0], "", parts[1]); msgErr != nil {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not send private message: "+msgErr.Error()))
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

		if postErr := svc.Post(currentRoom, trimmed); postErr != nil {
			writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not post: "+postErr.Error()))
			continue
		}

		// Echo own message locally.
		ownMsg := chat.ChatMessage{Handle: handle, Text: trimmed, Timestamp: time.Now()}
		writeChatLine(formatChatMessage(ownMsg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
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
	totalLen := len(tag) + end + 1 // full placeholder byte span including both @s
	repl := []byte(value)
	if len(repl) > totalLen {
		repl = repl[:totalLen]
	}
	for len(repl) < totalLen {
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
