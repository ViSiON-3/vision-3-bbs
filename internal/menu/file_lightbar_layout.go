package menu

import (
	"github.com/google/uuid"
)

// fileLightbar geometry, paging, and selection helpers.
func (lb *fileLightbar) isFileTagged(fileID uuid.UUID) bool {
	for _, taggedID := range lb.currentUser.TaggedFileIDs {
		if taggedID == fileID {
			return true
		}
	}
	return false
}

func (lb *fileLightbar) stripAnsi(str string) string {
	return lb.ansiRe.ReplaceAllString(str, "")
}

func (lb *fileLightbar) fileEntryHeight(idx int) int {
	if idx < 0 || idx >= len(lb.allFiles) {
		return 1
	}
	dizCount := len(formatDIZLines(lb.allFiles[idx].Description, lb.descColWidth, dizMaxLines))
	if dizCount < 1 {
		return 1
	}
	return dizCount
}

func (lb *fileLightbar) filesVisibleFrom(startIdx int) int {
	usedLines := 0
	count := 0
	for idx := startIdx; idx < len(lb.allFiles) && usedLines < lb.visibleRows; idx++ {
		h := lb.fileEntryHeight(idx)
		if usedLines+1 > lb.visibleRows {
			break
		}
		if usedLines+h > lb.visibleRows {
			h = lb.visibleRows - usedLines // show as many DIZ lines as fit
		}
		usedLines += h
		count++
	}
	return count
}

func (lb *fileLightbar) topIndexForPrevPage() int {
	if lb.topIndex <= 0 {
		return 0
	}
	usedLines := 0
	newTop := lb.topIndex
	for idx := lb.topIndex - 1; idx >= 0; idx-- {
		h := lb.fileEntryHeight(idx)
		if usedLines+h > lb.visibleRows {
			break
		}
		usedLines += h
		newTop = idx
	}
	return newTop
}

func (lb *fileLightbar) calculatePageInfo() (currentPage int, totalPagesCalc int) {
	if len(lb.allFiles) == 0 {
		return 1, 1
	}
	page := 0
	idx := 0
	foundCurrent := false
	for idx < len(lb.allFiles) {
		page++
		usedLines := 0
		pageStart := idx
		for idx < len(lb.allFiles) && usedLines < lb.visibleRows {
			h := lb.fileEntryHeight(idx)
			if usedLines+1 > lb.visibleRows {
				break
			}
			if usedLines+h > lb.visibleRows {
				h = lb.visibleRows - usedLines
			}
			usedLines += h
			idx++
		}
		if !foundCurrent && lb.topIndex >= pageStart && lb.topIndex < idx {
			currentPage = page
			foundCurrent = true
		}
	}
	if !foundCurrent {
		currentPage = page
	}
	totalPagesCalc = page
	return currentPage, totalPagesCalc
}

func (lb *fileLightbar) clampSelection() {
	if len(lb.allFiles) == 0 {
		lb.selectedIndex = 0
		lb.topIndex = 0
		return
	}
	if lb.selectedIndex < 0 {
		lb.selectedIndex = 0
	}
	if lb.selectedIndex >= len(lb.allFiles) {
		lb.selectedIndex = len(lb.allFiles) - 1
	}
	if lb.selectedIndex < lb.topIndex {
		lb.topIndex = lb.selectedIndex
	}
	// Scroll down: advance topIndex until selectedIndex fits within the
	// visible screen area, accounting for multi-line file entries.
	// We keep advancing until the selected entry either fits at full
	// height or is at the very top of the viewport (so large entries
	// like 21-line ANS art are always shown from the top, not crammed
	// at the bottom with only a few lines visible).
	for lb.topIndex <= lb.selectedIndex {
		usedLines := 0
		fits := false
		for idx := lb.topIndex; idx < len(lb.allFiles) && usedLines < lb.visibleRows; idx++ {
			h := lb.fileEntryHeight(idx)
			if usedLines+1 > lb.visibleRows {
				break // can't fit even the first line
			}
			fullH := h
			if usedLines+h > lb.visibleRows {
				h = lb.visibleRows - usedLines
			}
			usedLines += h
			if idx == lb.selectedIndex {
				// Fits fully, or entry is already at top of viewport
				// (can't scroll any higher without losing the selection).
				if h == fullH || idx == lb.topIndex {
					fits = true
				}
				break
			}
		}
		if fits || lb.topIndex >= lb.selectedIndex {
			break
		}
		lb.topIndex++
	}
	if lb.topIndex < 0 {
		lb.topIndex = 0
	}
}

func (lb *fileLightbar) screenRowForFile(fileIdx int) (startRow int, height int) {
	if fileIdx < lb.topIndex {
		return -1, 0
	}
	row := lb.fileAreaStartRow
	for idx := lb.topIndex; idx < len(lb.allFiles) && (row-lb.fileAreaStartRow) < lb.visibleRows; idx++ {
		h := lb.fileEntryHeight(idx)
		remaining := lb.visibleRows - (row - lb.fileAreaStartRow)
		if h > remaining {
			h = remaining
		}
		if remaining < 1 {
			break
		}
		if idx == fileIdx {
			return row, h
		}
		row += h
	}
	return -1, 0
}
