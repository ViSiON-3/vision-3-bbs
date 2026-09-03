package menu

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// Scan date sentinels for ScanConfig.ScanDate.
const (
	scanDateNewOnly int64 = -1 // only messages past the user's last-read pointer
	scanDateAll     int64 = 0  // every message in the area
)

// ScanConfig holds the scan parameters configured by runGetScanType.
//
// Every field here is honoured by the scan: SearchTo/SearchFrom, a date and a
// range become a per-message filter (see ScanConfig.filter) that both the
// start-message calculation and the message reader apply, and UpdatePointers
// controls whether the last-read pointers the reader advances are kept.
type ScanConfig struct {
	ScanDate       int64  // scanDateNewOnly, scanDateAll, or unix time of local midnight on the chosen day
	SearchTo       string // case-insensitive substring the To field must contain ("" = any)
	SearchFrom     string // case-insensitive substring the From field must contain ("" = any)
	RangeStart     int    // first message number to scan (0 = from the computed start)
	RangeEnd       int    // last message number to scan (0 = to the end of the area)
	UpdatePointers bool   // keep last-read pointer changes made while reading
	WhichAreas     int    // 1=tagged/marked, 2=all in conference, 3=current only
	Aborted        bool
}

// scanNoticePause is how long a validation notice (bad date, bad range, no
// matches) stays on screen before the scan menu redraws over it. Tests set
// it to zero.
var scanNoticePause = time.Second

// Per-area lightbar options (Pascal's 6-option bar for multi-area scan)
var scanAreaOptions = []MsgLightbarOption{
	{Label: " Read ", HotKey: 'R'},
	{Label: " Post ", HotKey: 'P'},
	{Label: " Jump ", HotKey: 'J'},
	{Label: " Skip ", HotKey: 'S'},
	{Label: " Quit ", HotKey: 'Q'},
	{Label: " NonStop ", HotKey: 'N'},
}

// scanDateLayouts lists the date formats accepted at the scan date prompts:
// month/day/year with a slash or dash and a 2- or 4-digit year, ISO
// year-month-day, and the all-digit MMDDYY / MMDDYYYY forms. This is the
// exact list the invalid-date notice and the sysop docs quote, so keep the
// three in step. Go's "1" and "2" verbs accept both zero-padded and bare
// month/day digits, so "9/1/26" and "09/01/26" both match the first layout;
// the all-digit layouts are fixed width. A two-digit year follows Go's pivot
// (69–99 → 19xx, 00–68 → 20xx).
var scanDateLayouts = []string{
	"1/2/06", "1/2/2006",
	"1-2-06", "1-2-2006",
	"2006-1-2",
	"010206", "01022006",
}

// parseScanDate parses a user-typed date in any of scanDateLayouts and
// returns local midnight at the start of that day. The scan treats the date
// as inclusive: a message written at any time on that day is "on or after"
// it, which is why the result is the start of the day rather than the
// parsed instant.
func parseScanDate(input string) (time.Time, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return time.Time{}, false
	}
	for _, layout := range scanDateLayouts {
		t, err := time.ParseInLocation(layout, input, time.Local)
		if err != nil {
			continue
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local), true
	}
	return time.Time{}, false
}

// since returns the inclusive lower date bound of the scan, or the zero time
// when the scan is not date-limited (new-only or all messages).
func (cfg *ScanConfig) since() time.Time {
	if cfg.ScanDate <= 0 {
		return time.Time{}
	}
	return time.Unix(cfg.ScanDate, 0)
}

// filter returns the per-message predicate implied by the scan's To, From,
// date and range settings, or nil when none of them is set so the reader
// runs unfiltered exactly as a plain newscan does.
//
// To and From are case-insensitive substring matches, so "joe" finds
// "Joe Smith" and "joe@example". The range and date bounds are inclusive.
func (cfg *ScanConfig) filter() msgOwnershipFilter {
	to := strings.ToUpper(strings.TrimSpace(cfg.SearchTo))
	from := strings.ToUpper(strings.TrimSpace(cfg.SearchFrom))
	since := cfg.since()
	rangeStart, rangeEnd := cfg.RangeStart, cfg.RangeEnd

	if to == "" && from == "" && since.IsZero() && rangeStart <= 0 && rangeEnd <= 0 {
		return nil
	}
	return func(m *message.DisplayMessage) bool {
		if rangeStart > 0 && m.MsgNum < rangeStart {
			return false
		}
		if rangeEnd > 0 && m.MsgNum > rangeEnd {
			return false
		}
		if !since.IsZero() && m.DateTime.Before(since) {
			return false
		}
		if to != "" && !strings.Contains(strings.ToUpper(m.To), to) {
			return false
		}
		if from != "" && !strings.Contains(strings.ToUpper(m.From), from) {
			return false
		}
		return true
	}
}

// readScanLine reads a line of printable input from the session's decoded key
// stream, echoing as it goes. Arrow keys and other escape sequences are
// decoded by the InputHandler and ignored here, so they can never leak into
// the scan menu as spurious commands (a raw byte reader would see the "A" of
// an up-arrow's ESC [ A as the Abort hotkey). Returns the trimmed line;
// errInputAborted on ESC.
func readScanLine(ih *editor.InputHandler, terminal *term.Terminal, outputMode ansi.OutputMode, maxLen int) (string, error) {
	var buf []byte
	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", err
		}
		switch {
		case key == editor.KeyEnter:
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return strings.TrimSpace(string(buf)), nil
		case key == editor.KeyEsc:
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return "", errInputAborted
		case key == editor.KeyBackspace || key == editor.KeyDelete:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				terminalio.WriteProcessedBytes(terminal, []byte("\b \b"), outputMode)
			}
		case key >= 32 && key < 127:
			if maxLen <= 0 || len(buf) < maxLen {
				buf = append(buf, byte(key))
				terminalio.WriteProcessedBytes(terminal, []byte{byte(key)}, outputMode)
			}
		}
	}
}

// showScanNotice writes a validation notice and holds it on screen briefly
// so it is readable before the caller redraws the menu.
func showScanNotice(terminal *term.Terminal, outputMode ansi.OutputMode, text string) {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
	time.Sleep(scanNoticePause)
}

// runGetScanType displays the Pascal-style scan configuration menu and
// returns the chosen settings. Enter starts the scan; A, Q or ESC abort it.
//
// Keys are read through the session InputHandler so escape sequences are
// decoded (and ignored) rather than mis-read as hotkeys.
func runGetScanType(ih *editor.InputHandler, e *MenuExecutor, terminal *term.Terminal,
	outputMode ansi.OutputMode, numMsgs int, currentOnly bool) (*ScanConfig, error) {

	cfg := &ScanConfig{
		ScanDate:       scanDateNewOnly, // Default: new messages only
		UpdatePointers: true,            // Default: update pointers
		WhichAreas:     1,               // Default: tagged areas
	}
	if currentOnly {
		cfg.WhichAreas = 3
	}

	showMenu := func() {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

		// Display ANSI header (Vision/2 style - 4 rows tall)
		ansPath := "menus/v3/ansi/NSCANHDR.ANS"
		headerContent, ansErr := ansi.GetAnsiFileContent(ansPath)
		if ansErr == nil {
			// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
			if outputMode == ansi.OutputModeCP437 {
				_, _ = terminal.Write(headerContent) // best-effort display
			} else {
				terminalio.WriteProcessedBytes(terminal, headerContent, outputMode)
			}
			// Position cursor on line 5 (after 4-row header)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		}

		// Date - Brackets: Dark grey (|08), Hotkeys: Bright cyan (|11), Labels: Dark cyan (|03), Values: Bright blue (|09)
		dateStr := "All New Messages"
		if cfg.ScanDate == scanDateAll {
			dateStr = "ALL Messages"
		} else if cfg.ScanDate > 0 {
			dateStr = fmt.Sprintf("Since %s", cfg.since().Format("01/02/06"))
		}
		line := fmt.Sprintf(e.LoadedStrings.ScanDateLine, dateStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		// To
		toStr := "N/A"
		if cfg.SearchTo != "" {
			toStr = fmt.Sprintf("Search For %s", cfg.SearchTo)
		}
		line = fmt.Sprintf(e.LoadedStrings.ScanToLine, toStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		// From
		fromStr := "N/A"
		if cfg.SearchFrom != "" {
			fromStr = fmt.Sprintf("Search For %s", cfg.SearchFrom)
		}
		line = fmt.Sprintf(e.LoadedStrings.ScanFromLine, fromStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		// Range
		rangeStr := "All"
		if cfg.RangeStart > 0 && cfg.RangeEnd > 0 {
			rangeStr = fmt.Sprintf("%d-%d", cfg.RangeStart, cfg.RangeEnd)
		}
		line = fmt.Sprintf(e.LoadedStrings.ScanRangeLine, rangeStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		// Update Pointers
		upStr := "Yes"
		if !cfg.UpdatePointers {
			upStr = "No"
		}
		line = fmt.Sprintf(e.LoadedStrings.ScanUpdateLine, upStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		// Which Areas
		var whichStr string
		switch cfg.WhichAreas {
		case 1:
			whichStr = "All Tagged Areas"
		case 2:
			whichStr = "ALL Areas in Conference"
		case 3:
			whichStr = "Current Area Only"
		}
		line = fmt.Sprintf(e.LoadedStrings.ScanWhichLine, whichStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		line = e.LoadedStrings.ScanAbortLine
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

		// Prompt - "Selection;" Dark Cyan (|03), "(Cr" Bright Cyan (|11), "/" Bright Magenta (|13), "Scan) :" Bright Cyan (|11)
		prompt := e.LoadedStrings.ScanSelectionPrompt
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
	}

	// promptLine shows a prompt and reads one line. A nil error with ok=false
	// means the user pressed ESC and the caller should leave the setting
	// alone and redraw the menu. Any other read error (EOF, idle timeout) is
	// returned so the session can disconnect as it would from any input loop.
	promptLine := func(prompt string, maxLen int) (string, bool, error) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
		input, err := readScanLine(ih, terminal, outputMode, maxLen)
		if err != nil {
			if errors.Is(err, errInputAborted) {
				return "", false, nil
			}
			return "", false, err
		}
		return input, true, nil
	}

	for {
		showMenu()

		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, err
		}

		if key == editor.KeyEsc {
			cfg.Aborted = true
			return cfg, nil
		}
		if key < 0 || key > 0x7F {
			continue // arrows, page keys and other decoded sequences: not hotkeys
		}

		switch unicode.ToUpper(rune(key)) {
		case '\r', '\n':
			// Enter = start scanning
			return cfg, nil

		case 'D': // Date
			input, ok, err := promptLine(e.LoadedStrings.ScanDatePrompt, 10)
			if err != nil {
				return nil, err
			}
			if !ok || input == "" {
				continue
			}
			switch unicode.ToUpper(rune(input[0])) {
			case 'A':
				cfg.ScanDate = scanDateAll
			case 'N':
				cfg.ScanDate = scanDateNewOnly
			default:
				t, parsed := parseScanDate(input)
				if !parsed {
					showScanNotice(terminal, outputMode, e.LoadedStrings.ScanInvalidDate)
					continue
				}
				cfg.ScanDate = t.Unix()
			}

		case 'T': // To
			input, ok, err := promptLine(e.LoadedStrings.ScanToPrompt, 30)
			if err != nil {
				return nil, err
			}
			if ok {
				cfg.SearchTo = input
			}

		case 'F': // From
			input, ok, err := promptLine(e.LoadedStrings.ScanFromPrompt, 30)
			if err != nil {
				return nil, err
			}
			if ok {
				cfg.SearchFrom = input
			}

		case 'R': // Range
			// ESC at either prompt leaves the range as it was, like the other
			// prompts. Enter alone at either prompt clears it (Cr/none): an
			// edit abandoned half-way must not leave the previous range in
			// effect. An out-of-bounds entry clears it with a notice, so the
			// menu never shows "All" while a stale bound is still in effect.
			// The range is only ever stored as a validated pair.
			if numMsgs <= 0 {
				showScanNotice(terminal, outputMode, e.LoadedStrings.ScanNoMessages)
				continue
			}
			startInput, ok, err := promptLine(fmt.Sprintf(e.LoadedStrings.ScanRangeStartPrompt, numMsgs), 6)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if startInput == "" {
				cfg.RangeStart, cfg.RangeEnd = 0, 0
				continue
			}
			startNum, convErr := strconv.Atoi(startInput)
			if convErr != nil || startNum < 1 || startNum > numMsgs {
				cfg.RangeStart, cfg.RangeEnd = 0, 0
				showScanNotice(terminal, outputMode, e.LoadedStrings.ScanInvalidRange)
				continue
			}

			endInput, ok, err := promptLine(fmt.Sprintf(e.LoadedStrings.ScanRangeEndPrompt, startNum, numMsgs), 6)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if endInput == "" {
				cfg.RangeStart, cfg.RangeEnd = 0, 0
				continue
			}
			endNum, convErr := strconv.Atoi(endInput)
			if convErr != nil || endNum < startNum || endNum > numMsgs {
				cfg.RangeStart, cfg.RangeEnd = 0, 0
				showScanNotice(terminal, outputMode, e.LoadedStrings.ScanInvalidRange)
				continue
			}
			cfg.RangeStart, cfg.RangeEnd = startNum, endNum

		case 'U': // Update pointers
			cfg.UpdatePointers = !cfg.UpdatePointers

		case 'S': // Scan which areas
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanWhichPrompt)), outputMode)
			aKey, aErr := ih.ReadKey()
			if aErr != nil {
				return nil, aErr
			}
			if aKey < 0 || aKey > 0x7F {
				continue
			}
			switch unicode.ToUpper(rune(aKey)) {
			case 'M':
				cfg.WhichAreas = 1
			case 'A':
				cfg.WhichAreas = 2
			case 'C':
				cfg.WhichAreas = 3
			}

		case 'A', 'Q': // Abort
			cfg.Aborted = true
			return cfg, nil
		}
	}
}

// areaListItem represents an item in the newscan config list (area or conference header)
type areaListItem struct {
	area     *message.MessageArea
	confName string
	isHeader bool
}
