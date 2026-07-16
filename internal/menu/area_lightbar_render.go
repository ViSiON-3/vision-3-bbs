package menu

import (
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// renderTop draws the TOP template at row 1, substituting ^CN with the
// current header name.
func (p *areaLightbarPicker[T]) renderTop() error {
	topStr := strings.ReplaceAll(string(p.topBytes), "^CN", p.headerName)
	withTokens := p.e.applyCommonTemplateTokens([]byte(topStr), p.currentUser, p.nodeNumber)
	processed := ansi.ReplacePipeCodes(withTokens)
	if err := terminalio.WriteProcessedBytes(p.terminal, []byte(ansi.MoveCursor(1, 1)), p.outputMode); err != nil {
		return err
	}
	return terminalio.WriteProcessedBytes(p.terminal, processed, p.outputMode)
}

// renderSeparator draws the horizontal rule above the hint line.
func (p *areaLightbarPicker[T]) renderSeparator() error {
	count := p.termWidth - 2
	if count < 0 {
		count = 0
	}
	sep := strings.Repeat("\xc4", count)
	line := ansi.MoveCursor(p.separatorRow, 1) + "\x1b[2K" + string(ansi.ReplacePipeCodes([]byte("|08\xfa"+sep+"\xfa|07")))
	return terminalio.WriteProcessedBytes(p.terminal, []byte(line), p.outputMode)
}

// renderHint draws the key-hint line at the bottom row.
func (p *areaLightbarPicker[T]) renderHint() error {
	line := ansi.MoveCursor(p.hintRow, 1) + "\x1b[2K" + string(ansi.ReplacePipeCodes([]byte(p.hint)))
	return terminalio.WriteProcessedBytes(p.terminal, []byte(line), p.outputMode)
}

// renderItemArea redraws every visible item row, highlighting the selection.
func (p *areaLightbarPicker[T]) renderItemArea() error {
	for row := 0; row < p.visibleRows; row++ {
		absRow := p.itemAreaStartRow + row
		if err := terminalio.WriteProcessedBytes(p.terminal, []byte(ansi.MoveCursor(absRow, 1)+"\x1b[2K"), p.outputMode); err != nil {
			return err
		}
		idx := p.topIndex + row
		if idx >= len(p.items) {
			continue
		}
		line := p.buildItemLine(p.items[idx], idx+1)
		if idx == p.selectedIndex {
			stripped := stripAreaAnsi(line)
			if len(stripped) > p.termWidth {
				stripped = stripped[:p.termWidth]
			}
			rendered := p.hiColorSeq + padRight(stripped, p.termWidth) + "\x1b[0m"
			if err := terminalio.WriteProcessedBytes(p.terminal, []byte(rendered), p.outputMode); err != nil {
				return err
			}
		} else {
			if err := terminalio.WriteProcessedBytes(p.terminal, []byte(line), p.outputMode); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderFull clears the screen and redraws header, items, separator, and hint.
func (p *areaLightbarPicker[T]) renderFull() error {
	if err := terminalio.WriteProcessedBytes(p.terminal, []byte(ansi.ClearScreen()), p.outputMode); err != nil {
		return err
	}
	if err := p.renderTop(); err != nil {
		return err
	}
	if err := p.renderItemArea(); err != nil {
		return err
	}
	if err := p.renderSeparator(); err != nil {
		return err
	}
	return p.renderHint()
}

// redrawChangedRows redraws only the previously selected and newly selected
// rows after a selection move that did not scroll the window.
func (p *areaLightbarPicker[T]) redrawChangedRows(prevSelectedIndex int) error {
	if prevSelectedIndex >= p.topIndex && prevSelectedIndex < p.topIndex+p.visibleRows {
		oldRow := p.itemAreaStartRow + (prevSelectedIndex - p.topIndex)
		oldLine := p.buildItemLine(p.items[prevSelectedIndex], prevSelectedIndex+1)
		if err := terminalio.WriteProcessedBytes(p.terminal, []byte(ansi.MoveCursor(oldRow, 1)+"\x1b[2K"), p.outputMode); err != nil {
			return err
		}
		if err := terminalio.WriteProcessedBytes(p.terminal, []byte(oldLine), p.outputMode); err != nil {
			return err
		}
	}
	if p.selectedIndex >= p.topIndex && p.selectedIndex < p.topIndex+p.visibleRows {
		newRow := p.itemAreaStartRow + (p.selectedIndex - p.topIndex)
		newLine := p.buildItemLine(p.items[p.selectedIndex], p.selectedIndex+1)
		rendered := p.hiColorSeq + padRight(stripAreaAnsi(newLine), p.termWidth) + "\x1b[0m"
		if err := terminalio.WriteProcessedBytes(p.terminal, []byte(ansi.MoveCursor(newRow, 1)+"\x1b[2K"), p.outputMode); err != nil {
			return err
		}
		if err := terminalio.WriteProcessedBytes(p.terminal, []byte(rendered), p.outputMode); err != nil {
			return err
		}
	}
	return nil
}

// showConfirm replaces the hint line with a pipe-coded confirmation message
// and pauses briefly so the user can read it.
func (p *areaLightbarPicker[T]) showConfirm(msg string) {
	line := ansi.MoveCursor(p.hintRow, 1) + "\x1b[2K" + string(ansi.ReplacePipeCodes([]byte(msg)))
	_ = terminalio.WriteProcessedBytes(p.terminal, []byte(line), p.outputMode)
	time.Sleep(1 * time.Second)
}
