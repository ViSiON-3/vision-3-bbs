package menu

import (
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
)

// areaLightbarPicker is a generic full-screen lightbar list picker shared by
// the conference (CHANGEMSGCONF) and message-area (SELECTMSGAREA) selection
// screens. It owns terminal layout, template-based rendering, the diff-based
// redraw loop, and key handling; variant-specific behavior is injected via
// the callback fields. Rendering methods live in area_lightbar_render.go.
type areaLightbarPicker[T any] struct {
	e           *MenuExecutor
	terminal    io.Writer
	outputMode  ansi.OutputMode
	currentUser *user.User
	nodeNumber  int

	items []T
	// buildItemLine renders one item row from the MID template. displayIdx
	// is 1-based.
	buildItemLine func(item T, displayIdx int) string
	hint          string // pipe-coded hint line rendered at hintRow
	headerName    string // value substituted for ^CN in the TOP template
	topBytes      []byte // raw TOP template bytes
	hiColorSeq    string // ANSI sequence for the highlighted (selected) row

	termWidth        int
	termHeight       int
	itemAreaStartRow int
	separatorRow     int
	hintRow          int
	visibleRows      int

	selectedIndex  int
	topIndex       int
	needFullRedraw bool

	// onSelect handles Enter on items[idx]. When done is false the loop
	// continues (e.g. an ACS check failed); otherwise run returns its values.
	onSelect func(idx int) (done bool, u *user.User, next string, err error)
	// onSideNav handles left/right arrows; nil means the keys are ignored.
	onSideNav func(left bool)
}

// resolveTermDims resolves terminal dimensions: prefer passed values, then
// user preferences, then 80x24 defaults.
func resolveTermDims(currentUser *user.User, termWidth, termHeight int) (int, int) {
	if termWidth <= 0 && currentUser != nil {
		termWidth = currentUser.ScreenWidth
	}
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 && currentUser != nil {
		termHeight = currentUser.ScreenHeight
	}
	if termHeight <= 0 {
		termHeight = 24
	}
	return termWidth, termHeight
}

// measureAreaHeaderRows counts the rows the TOP template occupies, using the
// same pipeline as renderTop so the count is accurate even when
// applyCommonTemplateTokens expands multi-line tokens.
func measureAreaHeaderRows(e *MenuExecutor, topBytes []byte, currentUser *user.User, nodeNumber int) int {
	sampleTop := strings.ReplaceAll(string(topBytes), "^CN", "")
	sampleWithTokens := e.applyCommonTemplateTokens([]byte(sampleTop), currentUser, nodeNumber)
	processedSample := string(ansi.ReplacePipeCodes(sampleWithTokens))
	headerLines := strings.Count(processedSample, "\n")
	// If the template's last row has no trailing \n, the cursor stays on that
	// row and headerLines is undercounted by 1 — causing itemAreaStartRow to
	// land on the separator line, which renderItemArea then overwrites.
	// Detect visible content after the last \n and bump headerLines if found.
	lastNL := strings.LastIndex(processedSample, "\n")
	tail := processedSample
	if lastNL >= 0 {
		tail = processedSample[lastNL+1:]
	}
	tail = areaLightbarAnsiRe.ReplaceAllString(tail, "")
	tail = strings.Trim(tail, "\r")
	if len(tail) > 0 {
		headerLines++
	}
	if headerLines < 1 {
		headerLines = 1
	}
	return headerLines
}

// resolveAreaHiColor returns the highlight ANSI sequence, preferring the
// optional BAR file (same pattern as FILELISTHI.BAR) over the theme default.
func resolveAreaHiColor(e *MenuExecutor, barName string, nodeNumber int) string {
	hiBarOptions, err := loadBarFile(barName, e)
	if err != nil {
		slog.Warn("failed to load "+barName+".BAR", "node", nodeNumber, "error", err)
	}
	if len(hiBarOptions) > 0 {
		return colorCodeToAnsi(hiBarOptions[0].HighlightColor)
	}
	return colorCodeToAnsi(e.Theme.YesNoHighlightColor)
}

// computeLayout derives the item-area, separator, and hint rows from the
// header height and terminal height.
func (p *areaLightbarPicker[T]) computeLayout(headerLines int) {
	p.itemAreaStartRow = headerLines + 1
	p.separatorRow = p.termHeight - 1
	p.hintRow = p.termHeight
	if p.separatorRow <= p.itemAreaStartRow {
		p.separatorRow = p.itemAreaStartRow + 1
	}
	p.visibleRows = p.separatorRow - p.itemAreaStartRow
	if p.visibleRows < 3 {
		p.visibleRows = 3
	}
}

// clampSelection keeps selectedIndex within the item list and scrolls
// topIndex so the selection stays visible.
func (p *areaLightbarPicker[T]) clampSelection() {
	if len(p.items) == 0 {
		p.selectedIndex, p.topIndex = 0, 0
		return
	}
	if p.selectedIndex < 0 {
		p.selectedIndex = 0
	}
	if p.selectedIndex >= len(p.items) {
		p.selectedIndex = len(p.items) - 1
	}
	if p.selectedIndex < p.topIndex {
		p.topIndex = p.selectedIndex
	}
	if p.selectedIndex >= p.topIndex+p.visibleRows {
		p.topIndex = p.selectedIndex - p.visibleRows + 1
	}
	if p.topIndex < 0 {
		p.topIndex = 0
	}
}

// moveSelection applies navigation keys that only change the selection or
// scroll position. Other keys are ignored.
func (p *areaLightbarPicker[T]) moveSelection(keyInt int) {
	switch keyInt {
	case editor.KeyArrowUp:
		p.selectedIndex--
	case editor.KeyArrowDown:
		p.selectedIndex++
	case editor.KeyPageUp, editor.KeyCtrlR:
		p.selectedIndex -= p.visibleRows
		p.topIndex -= p.visibleRows
		if p.topIndex < 0 {
			p.topIndex = 0
		}
	case editor.KeyPageDown, editor.KeyCtrlC:
		p.selectedIndex += p.visibleRows
		p.topIndex += p.visibleRows
	case editor.KeyHome:
		p.selectedIndex = 0
	case editor.KeyEnd:
		if len(p.items) > 0 {
			p.selectedIndex = len(p.items) - 1
		}
	default:
		if keyInt >= 32 && keyInt < 127 {
			ch := rune(keyInt)
			switch {
			case ch >= '1' && ch <= '9':
				idx := int(ch - '1')
				if idx < len(p.items) {
					p.selectedIndex = idx
				}
			case ch == '0':
				if 9 < len(p.items) {
					p.selectedIndex = 9
				}
			}
		}
	}
}

// run hides the cursor, drives the diff-based redraw loop, and dispatches
// keys until a selection is made or the user quits or disconnects.
func (p *areaLightbarPicker[T]) run(s ssh.Session) (*user.User, string, error) {
	ih := getSessionIH(s)
	_ = terminalio.WriteProcessedBytes(p.terminal, []byte("\x1b[?25l"), p.outputMode)
	defer terminalio.WriteProcessedBytes(p.terminal, []byte("\x1b[?25h"), p.outputMode)

	prevSelectedIndex := -1
	prevTopIndex := -1
	p.needFullRedraw = true

	for {
		p.clampSelection()

		if p.needFullRedraw {
			if err := p.renderFull(); err != nil {
				return nil, "", err
			}
			p.needFullRedraw = false
		} else if p.topIndex != prevTopIndex {
			if err := p.renderItemArea(); err != nil {
				return nil, "", err
			}
		} else if p.selectedIndex != prevSelectedIndex {
			if err := p.redrawChangedRows(prevSelectedIndex); err != nil {
				return nil, "", err
			}
		}

		prevSelectedIndex = p.selectedIndex
		prevTopIndex = p.topIndex

		keyInt, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, editor.ErrIdleTimeout) {
				return nil, "LOGOFF", editor.ErrIdleTimeout
			}
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		switch keyInt {
		case editor.KeyArrowLeft:
			if p.onSideNav != nil {
				p.onSideNav(true)
			}
		case editor.KeyArrowRight:
			if p.onSideNav != nil {
				p.onSideNav(false)
			}
		case editor.KeyEnter:
			if len(p.items) == 0 {
				continue
			}
			done, u, next, err := p.onSelect(p.selectedIndex)
			if done {
				return u, next, err
			}
		case editor.KeyEsc:
			return p.currentUser, "", nil
		default:
			if keyInt == 'q' || keyInt == 'Q' {
				return p.currentUser, "", nil
			}
			p.moveSelection(keyInt)
		}
	}
}
