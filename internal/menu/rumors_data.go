package menu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// RumorRecord represents a single rumor entry.
// Simplified from V2's RumorRec — title dropped in favor of text-only graffiti wall.
type RumorRecord struct {
	ID       int       `json:"id"`
	Author   string    `json:"author"`            // Displayed author (may be anonymous)
	RealUser string    `json:"real_user"`         // Actual handle (V2: Author2); legacy entries may contain old username
	UserID   int       `json:"user_id,omitempty"` // Stable owner ID for ownership checks (0 = legacy record)
	Text     string    `json:"text"`              // Rumor text
	PostedAt time.Time `json:"posted_at"`         // When posted
	MinLevel int       `json:"min_level"`         // Minimum access level to view
}

// rumorsData holds all rumors with a NextID counter.
type rumorsData struct {
	Rumors []RumorRecord `json:"rumors"`
	NextID int           `json:"next_id"`
}

// rumorMaxLength is the maximum rumor text length in runes. It matches the
// width of the @RR|C####...@ field in the stock MAIN/MSGMENU/DOORSM screens,
// so a rumor never gets clipped when shown there.
const rumorMaxLength = 56

var rumorsMu sync.Mutex

// clampRumorText trims surrounding whitespace and hard-cuts the text to
// rumorMaxLength runes. Input entry already stops at the limit; this is the
// backstop for any path (pasted input, scripts) that bypasses it.
func clampRumorText(s string) string {
	return ansi.TruncateRunes(strings.TrimSpace(s), rumorMaxLength, "")
}

// backfillRumorUserIDs populates UserID for legacy records where UserID == 0
// by looking up RealUser as a handle. Returns true if any records were updated.
func backfillRumorUserIDs(rd *rumorsData, um *user.UserMgr) bool {
	changed := false
	for i := range rd.Rumors {
		if rd.Rumors[i].UserID == 0 && rd.Rumors[i].RealUser != "" {
			if u, ok := um.GetUser(rd.Rumors[i].RealUser); ok {
				rd.Rumors[i].UserID = u.ID
				changed = true
			}
		}
	}
	return changed
}

func rumorsFilePath(dataDir string) string {
	return filepath.Join(boardDataDir(dataDir), "rumors.json")
}

func loadRumorsData(dataDir string) (*rumorsData, error) {
	data, err := os.ReadFile(rumorsFilePath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &rumorsData{NextID: 1}, nil
		}
		return nil, fmt.Errorf("read rumors.json: %w", err)
	}
	var rd rumorsData
	if err := json.Unmarshal(data, &rd); err != nil {
		return nil, fmt.Errorf("parse rumors.json: %w", err)
	}
	if rd.NextID < 1 {
		maxID := 0
		for _, r := range rd.Rumors {
			if r.ID > maxID {
				maxID = r.ID
			}
		}
		rd.NextID = maxID + 1
	}
	return &rd, nil
}

func saveRumorsData(dataDir string, rd *rumorsData) error {
	data, err := json.MarshalIndent(rd, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal rumors data: %w", err)
	}
	fp := rumorsFilePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		return fmt.Errorf("create rumors data directory: %w", err)
	}
	return os.WriteFile(fp, data, 0644)
}

// visibleRumors returns indices of rumors the user can see based on access level.
func visibleRumors(rd *rumorsData, userLevel int) []int {
	var visible []int
	for i, r := range rd.Rumors {
		if userLevel >= r.MinLevel {
			visible = append(visible, i)
		}
	}
	return visible
}

// rumorShownAuthor is the name a rumor was posted under: its stored Author,
// or the anonymous name for a record with none.
func rumorShownAuthor(r *RumorRecord, anonymousName string) string {
	if strings.TrimSpace(anonymousName) == "" {
		anonymousName = "Anonymous"
	}
	if r.Author == "" {
		return anonymousName
	}
	return r.Author
}

// rumorMaskedPoster returns the real poster behind a masked rumor, and false
// when the rumor was posted under its poster's own name. Each record keeps
// the poster's handle in RealUser, so any record whose shown author differs
// from RealUser is masked — including rumors posted under an anonymous name
// the board has since changed.
func rumorMaskedPoster(r *RumorRecord, anonymousName string) (string, bool) {
	poster := strings.TrimSpace(r.RealUser)
	if poster == "" || strings.EqualFold(poster, rumorShownAuthor(r, anonymousName)) {
		return "", false
	}
	return poster, true
}

// rumorDisplayAuthor returns the display name for a rumor author. A sysop
// also sees the real poster behind a masked rumor, as "Author (RealUser)".
func rumorDisplayAuthor(r *RumorRecord, isSysop bool, anonymousName string) string {
	shown := rumorShownAuthor(r, anonymousName)
	if isSysop {
		if poster, ok := rumorMaskedPoster(r, anonymousName); ok {
			return fmt.Sprintf("%s (%s)", shown, poster)
		}
	}
	return shown
}

// rumorSanitize replaces pipe characters in user-supplied strings to prevent
// them from being interpreted as pipe color codes when displayed via wv().
func rumorSanitize(s string) string {
	return strings.ReplaceAll(s, "|", "\xc2\xa6") // replace | with ¦ (U+00A6)
}

// rumorAnonName returns the configured anonymous display name.
func rumorAnonName(e *MenuExecutor) string {
	name := e.Strings().AnonymousName
	if strings.TrimSpace(name) == "" {
		return "Anonymous"
	}
	return name
}

// expandRandomRumorATCode replaces @RR@ AT-codes in content with a random
// visible rumor. Centralises the Contains guard + level resolution so callers
// don't duplicate the pattern.
func expandRandomRumorATCode(content []byte, dataDir string, userLevel int) []byte {
	if !bytes.Contains(content, []byte("@RR")) {
		return content
	}
	return replaceMenuATCode(content, "RR", getRandomRumorText(dataDir, userLevel))
}

// getRandomRumorText returns a random visible rumor's text for MCI substitution.
// Returns empty string if no rumors are available.
func getRandomRumorText(dataDir string, userLevel int) string {
	rumorsMu.Lock()
	rd, err := loadRumorsData(dataDir)
	rumorsMu.Unlock()
	if err != nil || len(rd.Rumors) == 0 {
		return ""
	}

	visible := visibleRumors(rd, userLevel)
	if len(visible) == 0 {
		return ""
	}

	idx := visible[rand.Intn(len(visible))]
	return rd.Rumors[idx].Text
}
