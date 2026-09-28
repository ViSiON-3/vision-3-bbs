package menu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// WantListEntry represents a single file request from a user.
type WantListEntry struct {
	ID       int    `json:"id"`
	Handle   string `json:"handle"`
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
	Date     string `json:"date"`
}

// wantListData is the stored want list. NextID is a monotonic allocator: IDs
// are never reused, so a delete chosen from an earlier listing can only ever
// hit the entry that was shown, even when two entries have identical text.
type wantListData struct {
	Entries []WantListEntry `json:"entries"`
	NextID  int             `json:"next_id"`
}

var wantListMu sync.Mutex

func wantListFilePath(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "wantlist.json")
}

// loadWantList reads the want list. It also accepts the legacy format, a bare
// JSON array of entries without IDs, and gives every entry lacking a unique
// ID one (see normalizeWantListIDs); the next save persists them.
func loadWantList(rootConfigPath string) (*wantListData, error) {
	data, err := os.ReadFile(wantListFilePath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			return &wantListData{NextID: 1}, nil
		}
		return nil, fmt.Errorf("read wantlist.json: %w", err)
	}
	var wl wantListData
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &wl.Entries); err != nil {
			return nil, fmt.Errorf("parse wantlist.json: %w", err)
		}
	} else if err := json.Unmarshal(data, &wl); err != nil {
		return nil, fmt.Errorf("parse wantlist.json: %w", err)
	}
	normalizeWantListIDs(&wl)
	return &wl, nil
}

// normalizeWantListIDs raises NextID above every live ID, then gives a new ID
// to each entry that has none or shares one with an earlier entry, in list
// order. It is deterministic for a given file, so sessions that load a legacy
// file agree on the IDs before any of them has saved it.
func normalizeWantListIDs(wl *wantListData) {
	if wl.NextID < 1 {
		wl.NextID = 1
	}
	for _, en := range wl.Entries {
		if en.ID >= wl.NextID {
			wl.NextID = en.ID + 1
		}
	}
	used := make(map[int]bool, len(wl.Entries))
	for i := range wl.Entries {
		if id := wl.Entries[i].ID; id > 0 && !used[id] {
			used[id] = true
			continue
		}
		wl.Entries[i].ID = wl.NextID
		wl.NextID++
	}
}

// findWantListEntryByID returns the index of the entry with the given ID, or -1.
func findWantListEntryByID(wl *wantListData, id int) int {
	for i := range wl.Entries {
		if wl.Entries[i].ID == id {
			return i
		}
	}
	return -1
}

func saveWantList(rootConfigPath string, wl *wantListData) error {
	dir := filepath.Dir(wantListFilePath(rootConfigPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if wl.Entries == nil {
		wl.Entries = []WantListEntry{}
	}
	data, err := json.MarshalIndent(wl, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal wantlist: %w", err)
	}
	return os.WriteFile(wantListFilePath(rootConfigPath), data, 0644)
}

func runWantList(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	if e.isCoSysOpOrAbove(currentUser) {
		return runWantListSysop(e, s, terminal, userManager, currentUser, nodeNumber, outputMode, termWidth, termHeight)
	}
	return runWantListUser(e, s, terminal, currentUser, nodeNumber, outputMode)
}

func runWantListSysop(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	wantListMu.Lock()
	wl, err := loadWantList(e.RootConfigPath)
	wantListMu.Unlock()
	if err != nil {
		return currentUser, "", err
	}
	entries := wl.Entries

	if len(entries) == 0 {
		msg := e.Strings().WantListEmpty
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)
		_ = writeCenteredPausePrompt(s, terminal, e.Strings().PauseString, outputMode, termWidth, termHeight) // best-effort pause prompt
		return currentUser, "", nil
	}

	// Display header
	hdr := e.Strings().WantListHeader
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+hdr+"\r\n")), outputMode)

	// Display each entry
	for i, entry := range entries {
		line := fmt.Sprintf("|15%3d. |07%-14s |11%-20s |03%-20s |08%s\r\n",
			i+1, entry.Handle, entry.Filename, entry.Reason, entry.Date)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07[|15C|07]lear All  [|15D|07]elete Individual  [|15Q|07]uit: ")), outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}

	choice := strings.ToUpper(strings.TrimSpace(input))
	switch choice {
	case "C":
		// Keep the allocator so cleared IDs are never handed out again.
		wantListMu.Lock()
		fresh, loadErr := loadWantList(e.RootConfigPath)
		if loadErr != nil {
			wantListMu.Unlock()
			return currentUser, "", loadErr
		}
		fresh.Entries = nil
		err = saveWantList(e.RootConfigPath, fresh)
		wantListMu.Unlock()
		if err != nil {
			return currentUser, "", err
		}
		msg := e.Strings().WantListCleared
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)

	case "D":
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Entry # to delete: ")), outputMode)
		numInput, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return currentUser, "", err
		}
		idx, err := strconv.Atoi(strings.TrimSpace(numInput))
		if err != nil || idx < 1 || idx > len(entries) {
			return currentUser, "", nil
		}
		// The number refers to the list shown above. Resolve it to that
		// entry's ID and find the ID in the reloaded list, rather than
		// trusting its position, which shifts if another session deleted one
		// meanwhile, or its text, which two entries can share.
		targetID := entries[idx-1].ID
		wantListMu.Lock()
		fresh, loadErr := loadWantList(e.RootConfigPath)
		if loadErr != nil {
			wantListMu.Unlock()
			return currentUser, "", nil
		}
		fi := findWantListEntryByID(fresh, targetID)
		if fi < 0 {
			wantListMu.Unlock()
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07That entry no longer exists.\r\n")), outputMode)
			return currentUser, "", nil
		}
		fresh.Entries = slices.Delete(fresh.Entries, fi, fi+1)
		err = saveWantList(e.RootConfigPath, fresh)
		wantListMu.Unlock()
		if err != nil {
			return currentUser, "", err
		}
	}

	return currentUser, "", nil
}

func runWantListUser(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode) (*user.User, string, error) {
	// Prompt for filename
	prompt := e.Strings().WantListPrompt
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+prompt+": ")), outputMode)
	filename, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return currentUser, "", nil
	}

	// Prompt for reason
	reasonPrompt := e.Strings().WantListReasonPrompt
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(reasonPrompt+": ")), outputMode)
	reason, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", err
	}
	reason = strings.TrimSpace(reason)

	entry := WantListEntry{
		Handle:   currentUser.Handle,
		Filename: filename,
		Reason:   reason,
		Date:     time.Now().Format("01/02/2006"),
	}

	wantListMu.Lock()
	wl, err := loadWantList(e.RootConfigPath)
	if err != nil {
		wantListMu.Unlock()
		return currentUser, "", err
	}
	entry.ID = wl.NextID
	wl.NextID++
	wl.Entries = append(wl.Entries, entry)
	err = saveWantList(e.RootConfigPath, wl)
	wantListMu.Unlock()
	if err != nil {
		return currentUser, "", err
	}

	msg := e.Strings().WantListSubmitted
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)

	return currentUser, "", nil
}
