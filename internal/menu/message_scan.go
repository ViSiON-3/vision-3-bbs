package menu

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// ScanConfig holds the scan parameters configured by GetScanType.
type ScanConfig struct {
	ScanDate       int64 // -1 = new only, 0 = all, >0 = unix timestamp
	SearchTo       string
	SearchFrom     string
	RangeStart     int
	RangeEnd       int
	UpdatePointers bool
	WhichAreas     int // 1=tagged/marked, 2=all in conference, 3=current only
	Aborted        bool
}

// Per-area lightbar options (Pascal's 6-option bar for multi-area scan)
var scanAreaOptions = []MsgLightbarOption{
	{Label: " Read ", HotKey: 'R'},
	{Label: " Post ", HotKey: 'P'},
	{Label: " Jump ", HotKey: 'J'},
	{Label: " Skip ", HotKey: 'S'},
	{Label: " Quit ", HotKey: 'Q'},
	{Label: " NonStop ", HotKey: 'N'},
}

// runGetScanType displays the Pascal-style scan configuration menu.
func runGetScanType(reader *bufio.Reader, e *MenuExecutor, terminal *term.Terminal,
	outputMode ansi.OutputMode, numMsgs int, currentOnly bool,
	hiColor int, loColor int) (*ScanConfig, error) {

	cfg := &ScanConfig{
		ScanDate:       -1,   // Default: new messages only
		UpdatePointers: true, // Default: update pointers
		WhichAreas:     1,    // Default: tagged areas
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
		if cfg.ScanDate == 0 {
			dateStr = "ALL Messages"
		} else if cfg.ScanDate > 0 {
			dateStr = fmt.Sprintf("From: %s", time.Unix(cfg.ScanDate, 0).Format("01/02/06"))
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

	for {
		showMenu()

		key, err := readSingleKey(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, err
		}

		upper := unicode.ToUpper(key)

		switch upper {
		case '\r', '\n':
			// Enter = start scanning
			return cfg, nil

		case 'D': // Date
			prompt := e.LoadedStrings.ScanDatePrompt
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			input, readErr := readLineInput(reader, terminal, outputMode, 10)
			if readErr != nil {
				continue
			}
			if input != "" {
				switch unicode.ToUpper(rune(input[0])) {
				case 'A':
					cfg.ScanDate = 0
				case 'N':
					cfg.ScanDate = -1
				default:
					// Try to parse as date
					t, tErr := time.Parse("01/02/06", input)
					if tErr == nil {
						cfg.ScanDate = t.Unix()
					}
				}
			}

		case 'T': // To
			prompt := e.LoadedStrings.ScanToPrompt
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			input, readErr := readLineInput(reader, terminal, outputMode, 30)
			if readErr != nil {
				continue
			}
			cfg.SearchTo = input

		case 'F': // From
			prompt := e.LoadedStrings.ScanFromPrompt
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			input, readErr := readLineInput(reader, terminal, outputMode, 30)
			if readErr != nil {
				continue
			}
			cfg.SearchFrom = input

		case 'R': // Range
			prompt := fmt.Sprintf(e.LoadedStrings.ScanRangeStartPrompt, numMsgs)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			startInput, readErr := readLineInput(reader, terminal, outputMode, 6)
			if readErr != nil {
				continue
			}
			startNum, _ := strconv.Atoi(startInput)
			if startNum < 1 || startNum > numMsgs {
				cfg.RangeStart = 0
				cfg.RangeEnd = 0
				continue
			}
			cfg.RangeStart = startNum

			prompt = fmt.Sprintf(e.LoadedStrings.ScanRangeEndPrompt, startNum, numMsgs)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			endInput, readErr := readLineInput(reader, terminal, outputMode, 6)
			if readErr != nil {
				continue
			}
			endNum, _ := strconv.Atoi(endInput)
			if endNum < startNum || endNum > numMsgs {
				cfg.RangeEnd = 0
				continue
			}
			cfg.RangeEnd = endNum

		case 'U': // Update pointers
			cfg.UpdatePointers = !cfg.UpdatePointers

		case 'S': // Scan which areas
			prompt := e.LoadedStrings.ScanWhichPrompt
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			aKey, aErr := readSingleKey(reader)
			if aErr != nil {
				continue
			}
			switch unicode.ToUpper(aKey) {
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
