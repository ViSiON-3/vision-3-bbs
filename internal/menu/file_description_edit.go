package menu

import (
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// minDescriptionLines is the most lines an edited multi-line description
// keeps: FILE_ID.DIZ's convention. A description imported with more keeps
// its own count, so an edit never cuts one short.
const minDescriptionLines = 10

// errDescriptionEditorFailed is returned when the editor couldn't run; the
// caller says so and leaves the description as it was.
var errDescriptionEditorFailed = errors.New("description editor failed")

// multiLineDescription reports whether desc has more than one line, as a
// FILE_ID.DIZ description does. Those are edited in the full-screen editor,
// so a one-line prompt doesn't replace every line with one.
func multiLineDescription(desc string) bool {
	return strings.Contains(strings.TrimRight(desc, "\r\n"), "\n")
}

// descriptionLineCap is the most lines an edit of desc keeps.
func descriptionLineCap(desc string) int {
	return max(minDescriptionLines, strings.Count(strings.TrimRight(desc, "\r\n"), "\n")+1)
}

// editDescription opens the full-screen editor on desc, a file's multi-line
// description, titled with the file's name. It returns the edited text,
// whether the caller saved it, and whether lines past descriptionLineCap
// were dropped. An abort, or saving it empty, leaves it unchanged
// (saved is false). Control characters are stripped as at upload.
func editDescription(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int,
	filename, handle, desc string) (edited string, saved, truncated bool, err error) {
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	editorCtx := editor.EditorContext{NodeNumber: nodeNumber, ConfArea: "File Description"}
	body, ok, edErr := editor.RunEditorWithMetadata(
		desc, s, sessionOutput(s), outputMode,
		filename, "All", handle, false,
		"", "", "", "", false, nil, getSessionIH(s), editorCtx,
	)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if edErr != nil {
		if errors.Is(edErr, io.EOF) || errors.Is(edErr, editor.ErrIdleTimeout) {
			return "", false, false, edErr
		}
		slog.Error("editor failed for a file description", "node", nodeNumber, "file", filename, "error", edErr)
		return "", false, false, errDescriptionEditorFailed
	}
	if !ok {
		return "", false, false, nil
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r", ""), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(sanitizeControlChars(line), " \t")
	}
	// Blank lines at the end are the editor's, not the description's.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "", false, false, nil
	}
	if limit := descriptionLineCap(desc); len(lines) > limit {
		lines, truncated = lines[:limit], true
	}
	return strings.Join(lines, "\n"), true, truncated, nil
}
