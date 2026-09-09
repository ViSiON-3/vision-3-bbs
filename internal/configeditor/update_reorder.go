package configeditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// moveRecordSlice moves the item at src to dst in the current record type's
// slice, without renumbering. Reorder mode calls this on each arrow key so the
// item travels live with the cursor; positions are renumbered once, on commit.
func (m *Model) moveRecordSlice(src, dst int) {
	if src == dst {
		return
	}
	switch m.recordType {
	case "msgarea":
		m.configs.MsgAreas = reorderSlice(m.configs.MsgAreas, src, dst)
	case "filearea":
		m.configs.FileAreas = reorderSlice(m.configs.FileAreas, src, dst)
	case "conference":
		m.configs.Conferences = reorderSlice(m.configs.Conferences, src, dst)
	case "protocol":
		m.configs.Protocols = reorderSlice(m.configs.Protocols, src, dst)
	case "archiver":
		m.configs.Archivers.Archivers = reorderSlice(m.configs.Archivers.Archivers, src, dst)
	case "login":
		m.configs.LoginSeq = reorderSlice(m.configs.LoginSeq, src, dst)
	}
}

// renumberReorderedPositions renumbers the position field of the record types
// that carry one, after a reorder is committed. Called on Enter only, so a
// cancelled (Esc) reorder leaves positions exactly as they were.
func (m *Model) renumberReorderedPositions() {
	switch m.recordType {
	case "msgarea":
		renumberMsgAreaPositions(m.configs.MsgAreas)
	case "conference":
		renumberConferencePositions(m.configs.Conferences)
	}
}

// reorderSlice removes the item at src and inserts it at dst, returning the modified slice.
func reorderSlice[T any](s []T, src, dst int) []T {
	if src < 0 || src >= len(s) || dst < 0 || dst >= len(s) {
		return s
	}
	item := s[src]
	// Remove source
	result := make([]T, 0, len(s))
	for i := range s {
		if i != src {
			result = append(result, s[i])
		}
	}
	// Insert at destination
	final := make([]T, 0, len(s))
	final = append(final, result[:dst]...)
	final = append(final, item)
	final = append(final, result[dst:]...)
	return final
}

// renumberMsgAreaPositions sets Position = index + 1 for each area in the slice.
func renumberMsgAreaPositions(areas []message.MessageArea) {
	for i := range areas {
		areas[i].Position = i + 1
	}
}

// renumberConferencePositions sets Position = index + 1 for each conference in the slice.
func renumberConferencePositions(confs []conference.Conference) {
	for i := range confs {
		confs[i].Position = i + 1
	}
}
