package message

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// ErrAreaNotFound is returned when a message area doesn't exist.
var ErrAreaNotFound = errors.New("message area not found")

// Error Handling Design:
// - Read operations (Get*Count, GetLastRead, GetNextUnread, etc.) treat missing
//   areas as empty (return 0, nil) to avoid failing when areas are referenced
//   but not yet configured. This allows graceful degradation.
// - Write operations (AddMessage, SetLastRead) and direct base access (GetBase,
//   GetMessage) return ErrAreaNotFound to ensure callers are aware the area
//   doesn't exist before attempting modifications.
// - All operations propagate I/O errors (not ErrAreaNotFound) so real failures
//   are never masked.

type threadIndex struct {
	total      int
	modCounter uint32
	counts     map[string]int
}

// msgidIndex maps MSGIDs to 1-based message numbers for fast reply lookups.
type msgidIndex struct {
	total      int
	modCounter uint32
	msgIDs     map[string]int // MSGID string -> 1-based message number
}

const messageAreaFile = "message_areas.json"

// MessageManager handles message areas backed by JAM message bases.
// Bases are opened on-demand and closed after each operation to allow
// v3mail and other external tools concurrent access.
type MessageManager struct {
	mu             sync.RWMutex
	dataPath       string // Base data directory (e.g., "data")
	areasPath      string // Full path to message_areas.json
	areasByID      map[int]*MessageArea
	areasByTag     map[string]*MessageArea
	areasByEchoTag map[string]*MessageArea // indexed by EchoTag when it differs from Tag
	boardName      string                  // BBS name for echomail origin lines
	// networkTearlines maps network key -> custom tearline text.
	networkTearlines map[string]string
	threadIndex      map[int]*threadIndex
	msgidIndex       map[int]*msgidIndex

	// OnMessagePosted is called after a message is successfully written to a JAM base.
	// The callback receives the area and the message details. May be nil.
	OnMessagePosted func(area *MessageArea, msgNum int, from, to, subject, body string)

	// BodyTransform is called before writing a message to JAM, allowing callers
	// (e.g. V3Net) to modify the body for the local copy (e.g. appending tearline/origin).
	// The original (untransformed) body is still passed to OnMessagePosted.
	// May be nil.
	BodyTransform func(areaID int, body string) string
}

// NewMessageManager creates and initializes a new MessageManager.
// dataPath is the directory where JAM base files are stored.
// configPath is the directory containing message_areas.json.
// boardName is the BBS name used in echomail origin lines.
// networkTearlines maps network name -> custom tearline text for echomail.
func NewMessageManager(dataPath, configPath, boardName string, networkTearlines map[string]string) (*MessageManager, error) {
	mm := &MessageManager{
		dataPath:         dataPath,
		areasPath:        filepath.Join(configPath, messageAreaFile),
		areasByID:        make(map[int]*MessageArea),
		areasByTag:       make(map[string]*MessageArea),
		areasByEchoTag:   make(map[string]*MessageArea),
		boardName:        boardName,
		networkTearlines: normalizeNetworkTearlines(networkTearlines),
		threadIndex:      make(map[int]*threadIndex),
		msgidIndex:       make(map[int]*msgidIndex),
	}

	if err := mm.loadMessageAreas(); err != nil {
		if os.IsNotExist(err) {
			slog.Info("message areas file not found; starting with none", "file", messageAreaFile)
		} else {
			return nil, fmt.Errorf("failed to load message areas: %w", err)
		}
	}

	slog.Info("message manager initialized", "count", len(mm.areasByID))
	return mm, nil
}

// DataPath returns the base data directory this manager was constructed with
// (e.g. "data"). Used by adjacent subsystems that persist their own state
// alongside the message bases.
func (mm *MessageManager) DataPath() string {
	return mm.dataPath
}

func normalizeNetworkTearlines(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (mm *MessageManager) tearlineForNetwork(network string) string {
	if mm.networkTearlines == nil {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(network))
	if key == "" {
		return ""
	}
	return mm.networkTearlines[key]
}

// Close is a no-op now that bases are opened on-demand.
// Kept for API compatibility.
func (mm *MessageManager) Close() error {
	return nil
}

// GetBase returns the underlying JAM base for an area. This is used by
// the tosser for direct base access. The caller MUST close the base when done.
func (mm *MessageManager) GetBase(areaID int) (*jam.Base, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return nil, err
	}
	// Note: Caller must close the base
	return b, nil
}

// normalizeLineEndings converts JAM CR line endings to LF for display.
func normalizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// stripKludgeLines removes FTN kludge lines (lines starting with \x01) from
// message text. These are metadata lines (e.g. V3NETUUID, MSGID) that should
// not be visible to users or included in quoted text.
func stripKludgeLines(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) > 0 && line[0] == '\x01' {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
