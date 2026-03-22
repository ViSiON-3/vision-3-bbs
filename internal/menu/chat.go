package menu

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
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

// pickChatService selects a chat backend based on available leaves.
// If no leaves are configured, falls back to the local SQLite backend.
// If exactly one leaf is configured, uses it directly.
// If multiple leaves exist, falls back to local (network picker is future work).
func pickChatService(e *MenuExecutor, handle string) (chat.ChatService, error) {
	dbPath := e.ServerCfg.DataDir + "/chat.db"
	if e.ChatLeaves == nil {
		return chat.NewLocalChatService(handle, dbPath)
	}
	leaves := e.ChatLeaves.ActiveChatLeaves()
	if len(leaves) == 0 {
		return chat.NewLocalChatService(handle, dbPath)
	}
	if len(leaves) == 1 {
		return leaves[0].NewSession(handle), nil
	}
	// Multiple leaves: fall back to local for now (network picker is future work).
	return chat.NewLocalChatService(handle, dbPath)
}

func runChat(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	handle := currentUser.Handle

	// Get terminal height: prefer passed parameter, then session registry, then default
	height := 24 // default
	if termHeight > 0 {
		height = termHeight
	} else if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
		sess.Mutex.RLock()
		if sess.Height > 0 {
			height = sess.Height
		}
		sess.Mutex.RUnlock()
	}

	// Layout: line 1 = header, line 2 = top separator, lines 3..(height-2) = scroll region,
	// line (height-1) = bottom separator, line height = input
	scrollBottom := height - 2

	// Clear screen and show header
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	header := e.LoadedStrings.ChatHeader
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode)

	// Separator on line 2
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(2, 1)), outputMode)
	sep := e.LoadedStrings.ChatSeparator
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(sep)), outputMode)

	// Set scroll region to lines 3..(height-2) for messages
	terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1B[3;%dr", scrollBottom)), outputMode)

	// rawWriter writes directly to the SSH session, bypassing term.Terminal's line editor.
	// This is needed because term.Terminal.Write() does escape processing that conflicts
	// with ReadLine() when called from another goroutine.
	var rawMu sync.Mutex
	rawWrite := func(data []byte) {
		rawMu.Lock()
		defer rawMu.Unlock()
		terminalio.WriteProcessedBytes(s, data, outputMode)
	}

	// writeChatLine writes a message into the scroll region via the raw SSH session.
	writeChatLine := func(text string) {
		seq := ansi.SaveCursor() + ansi.MoveCursor(scrollBottom, 1) + "\r\n"
		processed := ansi.ReplacePipeCodes([]byte(text))
		rawMu.Lock()
		defer rawMu.Unlock()
		terminalio.WriteProcessedBytes(s, []byte(seq), outputMode)
		terminalio.WriteProcessedBytes(s, processed, outputMode)
		terminalio.WriteProcessedBytes(s, []byte(ansi.RestoreCursor()), outputMode)
	}

	// Select chat backend, falling back to local if the network backend fails.
	svc, err := pickChatService(e, handle)
	if err != nil {
		log.Printf("ERROR: Node %d: failed to create chat service: %v", nodeNumber, err)
		return nil, "", nil
	}

	// Join default room "lobby"
	currentRoom := "lobby"
	_, history, err := svc.Join(currentRoom)
	if err != nil {
		// Network backend unavailable (e.g. hub doesn't support chat yet) — fall back to local.
		log.Printf("INFO: Node %d: chat join failed (%v), falling back to local chat", nodeNumber, err)
		svc.Close() //nolint:errcheck
		dbPath := e.ServerCfg.DataDir + "/chat.db"
		svc, err = chat.NewLocalChatService(handle, dbPath)
		if err != nil {
			log.Printf("ERROR: Node %d: local chat fallback failed: %v", nodeNumber, err)
			return nil, "", nil
		}
		_, history, err = svc.Join(currentRoom)
		if err != nil {
			log.Printf("ERROR: Node %d: local chat join failed: %v", nodeNumber, err)
			svc.Close() //nolint:errcheck
			return nil, "", nil
		}
	}

	// Show recent history
	for _, msg := range history {
		writeChatLine(formatChatMessage(msg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
	}

	// Draw input line separator and position cursor
	inputSep := e.LoadedStrings.ChatSeparator
	rawWrite([]byte(ansi.MoveCursor(height-1, 1)))
	rawWrite(ansi.ReplacePipeCodes([]byte(inputSep)))
	rawWrite([]byte(ansi.MoveCursor(height, 1)))

	// Set terminal prompt to show handle
	prompt := fmt.Sprintf("\x1B[%d;1H\x1B[2K<%s> ", height, handle)
	terminal.SetPrompt(prompt)

	// Goroutine to receive and display events from the chat service
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
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatJoinMsg, ev.Join.Handle, ev.Join.Room))
				}
			case chat.TypeLeave:
				if ev.Leave != nil {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatLeaveMsg, ev.Leave.Handle, ev.Leave.Room))
				}
			case chat.TypeTopic:
				if ev.Topic != nil {
					writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatTopicMsg, ev.Topic.Room, ev.Topic.Topic))
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

	// cleanup closes the service and waits for the event goroutine.
	cleanup := func() {
		svc.Leave(currentRoom) //nolint:errcheck
		svc.Close()            //nolint:errcheck
		<-done
	}

	// Main input loop
	for {
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if err == io.EOF {
				cleanup()
				rawWrite([]byte("\x1B[r")) // reset scroll region
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
					for _, msg := range joinHistory {
						writeChatLine(formatChatMessage(msg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
					}
				}
			}
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
			users := svc.Users()
			writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Users in "+currentRoom+": "+strings.Join(users, ", ")))
			continue
		}

		// Post to current room
		if postErr := svc.Post(currentRoom, trimmed); postErr != nil {
			writeChatLine(fmt.Sprintf(e.LoadedStrings.ChatSystemPrefix, "Could not post: "+postErr.Error()))
			continue
		}

		// Display own message locally
		ownMsg := chat.ChatMessage{
			Handle:    handle,
			Text:      trimmed,
			Timestamp: time.Now(),
		}
		writeChatLine(formatChatMessage(ownMsg, e.LoadedStrings.ChatSystemPrefix, e.LoadedStrings.ChatMessageFormat))
	}

	// Normal exit
	cleanup()

	// Reset scroll region and prompt before leaving
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
