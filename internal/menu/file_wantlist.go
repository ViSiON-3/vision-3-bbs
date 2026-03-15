package menu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// WantListEntry represents a single file request from a user.
type WantListEntry struct {
	Handle   string `json:"handle"`
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
	Date     string `json:"date"`
}

var wantListMu sync.Mutex

func wantListFilePath(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "wantlist.json")
}

func loadWantList(rootConfigPath string) ([]WantListEntry, error) {
	data, err := os.ReadFile(wantListFilePath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			return []WantListEntry{}, nil
		}
		return nil, fmt.Errorf("read wantlist.json: %w", err)
	}
	var entries []WantListEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse wantlist.json: %w", err)
	}
	return entries, nil
}

func saveWantList(rootConfigPath string, entries []WantListEntry) error {
	dir := filepath.Dir(wantListFilePath(rootConfigPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal wantlist: %w", err)
	}
	return os.WriteFile(wantListFilePath(rootConfigPath), data, 0644)
}

func runWantList(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if e.isCoSysOpOrAbove(currentUser) {
		return runWantListSysop(e, s, terminal, userManager, currentUser, nodeNumber, outputMode, termWidth, termHeight)
	}
	return runWantListUser(e, s, terminal, currentUser, nodeNumber, outputMode)
}

func runWantListSysop(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	wantListMu.Lock()
	entries, err := loadWantList(e.RootConfigPath)
	wantListMu.Unlock()
	if err != nil {
		return currentUser, "", err
	}

	if len(entries) == 0 {
		msg := e.LoadedStrings.WantListEmpty
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)
		writeCenteredPausePrompt(s, terminal, e.LoadedStrings.PauseString, outputMode, termWidth, termHeight)
		return currentUser, "", nil
	}

	// Display header
	hdr := e.LoadedStrings.WantListHeader
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
		wantListMu.Lock()
		err = saveWantList(e.RootConfigPath, []WantListEntry{})
		wantListMu.Unlock()
		if err != nil {
			return currentUser, "", err
		}
		msg := e.LoadedStrings.WantListCleared
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)

	case "D":
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Entry # to delete: ")), outputMode)
		numInput, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return currentUser, "", err
		}
		idx, err := strconv.Atoi(strings.TrimSpace(numInput))
		wantListMu.Lock()
		entries, loadErr := loadWantList(e.RootConfigPath)
		if err != nil || loadErr != nil || idx < 1 || idx > len(entries) {
			wantListMu.Unlock()
			return currentUser, "", nil
		}
		entries = append(entries[:idx-1], entries[idx:]...)
		err = saveWantList(e.RootConfigPath, entries)
		wantListMu.Unlock()
		if err != nil {
			return currentUser, "", err
		}
	}

	return currentUser, "", nil
}

func runWantListUser(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, nodeNumber int, outputMode ansi.OutputMode) (*user.User, string, error) {
	// Prompt for filename
	prompt := e.LoadedStrings.WantListPrompt
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
	reasonPrompt := e.LoadedStrings.WantListReasonPrompt
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
	entries, err := loadWantList(e.RootConfigPath)
	if err != nil {
		wantListMu.Unlock()
		return currentUser, "", err
	}
	entries = append(entries, entry)
	err = saveWantList(e.RootConfigPath, entries)
	wantListMu.Unlock()
	if err != nil {
		return currentUser, "", err
	}

	msg := e.LoadedStrings.WantListSubmitted
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+msg+"\r\n")), outputMode)

	return currentUser, "", nil
}
