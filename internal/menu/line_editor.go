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

// lineEdit is a short text edited in the full-screen editor, such as an
// auto-signature or a FILE_ID.DIZ description: what it starts from and what
// is kept of it.
type lineEdit struct {
	// text is what the editor opens on.
	text string
	// subject and handle head the editor's screen.
	subject, handle string
	// ctx is the editor's context, its node and ConfArea set.
	ctx editor.EditorContext
	// maxLines is the most lines kept; more are dropped and reported.
	maxLines int
	// clean is applied to each line before anything else, nil for none.
	clean func(string) string
	// what names the text in the log when the editor fails.
	what string
	// failed is returned when the editor couldn't run; it has been logged.
	failed error
}

// textLines splits text into lines, taking CRLF and a bare CR as a line
// break as the file list does, with any blank lines at the end dropped.
func textLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// runLineEditor opens the full-screen editor on le.text and returns the
// lines saved, each cleaned, with blank lines at the end dropped and cut to
// le.maxLines. truncated reports lines dropped there, or past the most the
// editor holds (editor.MaxLines), which it drops as it loads. saved is false
// when the caller abandoned the edit. Nothing is stored: that is the
// caller's job.
func runLineEditor(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, le lineEdit) (lines []string, saved, truncated bool, err error) {
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	body, saved, edErr := editor.RunEditorWithMetadata(
		le.text, s, sessionOutput(s), outputMode,
		le.subject, "All", le.handle, false,
		"", "", "", "", false, nil, getSessionIH(s), le.ctx,
	)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if edErr != nil {
		if errors.Is(edErr, io.EOF) || errors.Is(edErr, editor.ErrIdleTimeout) {
			return nil, false, false, edErr
		}
		slog.Error("editor failed for "+le.what, "node", le.ctx.NodeNumber, "error", edErr)
		return nil, false, false, le.failed
	}
	if !saved {
		return nil, false, false, nil
	}
	lines = textLines(body)
	if le.clean != nil {
		for i, line := range lines {
			lines[i] = le.clean(line)
		}
		lines = textLines(strings.Join(lines, "\n"))
	}
	if len(lines) > le.maxLines {
		lines, truncated = lines[:le.maxLines], true
	}
	if len(textLines(le.text)) > editor.MaxLines {
		truncated = true
	}
	return lines, true, truncated, nil
}
