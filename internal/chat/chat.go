package chat

import (
	"fmt"
	"strings"
	"time"
)

// RoomInfo describes an active chat room.
type RoomInfo struct {
	Name      string
	Topic     string
	UserCount int
}

// ChatMessage is a single message in a room (or a system announcement).
type ChatMessage struct {
	Room      string
	Handle    string
	Node      string // empty in local mode
	BBS       string // empty in local mode
	Text      string
	Timestamp time.Time
	IsSystem  bool
}

// ChatEventType identifies the kind of event in a ChatEvent.
type ChatEventType int

const (
	TypeMessage ChatEventType = iota // incoming room message
	TypePrivate                      // private/direct message
	TypeJoin                         // user joined a room
	TypeLeave                        // user left a room
	TypeTopic                        // room topic changed
	TypeSystem                       // system message (reconnecting, errors)
)

// ChatJoin carries join event data.
type ChatJoin struct{ Room, Handle, BBS string }

// ChatLeave carries leave event data.
type ChatLeave struct{ Room, Handle, BBS string }

// ChatTopic carries topic-change event data.
type ChatTopic struct{ Room, Topic, SetBy string }

// ChatEvent is the sum type delivered on the Events() channel.
type ChatEvent struct {
	Type      ChatEventType
	Message   *ChatMessage // TypeMessage, TypePrivate
	Join      *ChatJoin    // TypeJoin
	Leave     *ChatLeave   // TypeLeave
	Topic     *ChatTopic   // TypeTopic
	Text      string       // TypeSystem human-readable text
	Reconnect bool         // TypeSystem: true when reconnected
}

// ChatService is the interface for all chat backends (local or V3Net).
type ChatService interface {
	// Join subscribes the user to a room, returning the current room list
	// and up to 50 recent messages of history.
	Join(room string) ([]RoomInfo, []ChatMessage, error)

	// Leave unsubscribes the user from a room.
	Leave(room string) error

	// Post sends a message to the current room.
	Post(room, text string) error

	// Private sends a direct message to a specific user.
	// node is the target's node ID; ignored in local mode.
	Private(handle, node, text string) error

	// SetTopic sets the topic for a room.
	SetTopic(room, topic string) error

	// Rooms returns all currently active rooms.
	Rooms() ([]RoomInfo, error)

	// History returns up to limit recent messages for a room (max 200).
	History(room string, limit int) ([]ChatMessage, error)

	// Users returns the handles currently in the active room.
	Users() []string

	// Events returns a channel delivering incoming chat events.
	// The channel is closed when the session ends.
	Events() <-chan ChatEvent

	// Close tears down the session cleanly.
	Close() error
}

// NormalizeRoom lowercases room names and replaces spaces with hyphens.
// Returns an error if the name is empty or exceeds 32 characters after normalisation.
func NormalizeRoom(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("room name cannot be empty")
	}
	name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	if len(name) > 32 {
		return "", fmt.Errorf("room name too long (max 32 characters)")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return "", fmt.Errorf("room name may only contain lowercase letters, digits, and hyphens")
		}
	}
	return name, nil
}
