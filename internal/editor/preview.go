package editor

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// The preview strip: rows under the text that show its first lines as they
// will be read, pipe colour codes in colour (#352). The auto-signature
// editor uses it, since a signature's colours are the point of it.

// previewDivider is the row between the text and the preview.
const previewDivider = "|08─── |07Preview|08 "

// SetPreview has the screen keep lines rows under the text for a preview
// of its first lines; 0 turns it off. The rows come out of the text area,
// at any size where at least 5 text rows are left; below that there is no
// preview. Call it before Run, which lays the screen out.
func (s *Screen) SetPreview(lines int) {
	s.previewLines = max(lines, 0)
}

// reservePreview takes the preview's rows, and the divider's, from the
// bottom of the text area. It must follow a fresh layout, which sets
// screenLines to the whole text area: Run calls it once the header and
// footer are loaded, and Resize after it lays the screen out again.
func (s *Screen) reservePreview() {
	s.previewY = 0
	s.lastPreview = nil
	if s.previewLines == 0 || s.screenLines-s.previewLines-1 < 5 {
		return
	}
	s.screenLines -= s.previewLines + 1
	s.previewY = s.editingStartY + s.screenLines
}

// previewRows are the buffer's first lines as the preview shows them: their
// colour codes expanded (only colours: a |CL or |XY in a signature must not
// move the editor's screen), each cut to the screen's width.
func (s *Screen) previewRows(buffer *MessageBuffer) []string {
	rows := make([]string, s.previewLines)
	for i := range rows {
		if i+1 > buffer.GetLineCount() {
			break
		}
		line := string(ansi.ReplaceColorPipeCodes([]byte(buffer.GetLine(i + 1))))
		rows[i] = ansi.TruncateVisible(line, s.termWidth-1)
	}
	return rows
}

// drawPreview draws the divider and the preview rows when the buffer's
// first lines changed since they were last drawn (always after a full
// redraw). It leaves the cursor where it found it to the caller, which
// repositions after.
func (s *Screen) drawPreview(buffer *MessageBuffer) {
	if s.previewY == 0 {
		return
	}
	rows := s.previewRows(buffer)
	if s.lastPreview != nil && slicesEqual(rows, s.lastPreview) {
		return
	}
	if s.lastPreview == nil {
		width := s.termWidth - 1
		divider := previewDivider + strings.Repeat("─", max(width-ansi.VisibleLength(string(ansi.ReplacePipeCodes([]byte(previewDivider)))), 0))
		s.GoXY(1, s.previewY)
		s.WriteDirectProcessed(divider + "|07")
		s.ClearEOL()
	}
	for i, row := range rows {
		if s.lastPreview != nil && s.lastPreview[i] == row {
			continue
		}
		s.GoXY(1, s.previewY+1+i)
		s.WriteDirect(textSGR + row + textSGR)
		s.ClearEOL()
	}
	s.lastPreview = rows
}

// slicesEqual reports whether a and b hold the same strings.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
