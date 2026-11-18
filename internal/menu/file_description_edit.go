package menu

import (
	"errors"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// minDescriptionLines is the most lines an edited multi-line description
// keeps: FILE_ID.DIZ's convention. A description imported with more keeps
// its own count, up to the most the editor holds, so an edit never cuts one
// shorter than that.
const minDescriptionLines = 10

// errDescriptionEditorFailed is returned when the editor couldn't run; the
// caller says so and leaves the description as it was.
var errDescriptionEditorFailed = errors.New("description editor failed")

// descriptionLines is desc as the editor shows it and an edit saves it:
// split at CRLF, LF or a bare CR, as the file list breaks it, with control
// characters stripped as at upload, trailing spaces trimmed and blank lines
// at the end dropped.
func descriptionLines(desc string) []string {
	lines := textLines(desc)
	for i, line := range lines {
		lines[i] = cleanDescriptionLine(line)
	}
	return textLines(strings.Join(lines, "\n"))
}

// cleanDescriptionLine is one line of a description as an edit saves it.
func cleanDescriptionLine(line string) string {
	return strings.TrimRight(sanitizeControlChars(line), " \t")
}

// multiLineDescription reports whether desc has more than one line, as a
// FILE_ID.DIZ description does. Those are edited in the full-screen editor,
// so a one-line prompt doesn't replace every line with one.
func multiLineDescription(desc string) bool {
	return len(textLines(desc)) > 1
}

// descriptionLineCap is the most lines an edit of desc keeps.
func descriptionLineCap(desc string) int {
	return min(max(minDescriptionLines, len(textLines(desc))), editor.MaxLines)
}

// oneLineDescription is a one-line description as the one-line prompt shows
// it, with any line break at its end gone.
func oneLineDescription(desc string) string {
	return strings.Join(textLines(desc), " ")
}

// editDescription opens the full-screen editor on desc, a file's multi-line
// description, titled with the file's name. It returns the edited text and
// whether it differs from desc as the editor showed it; an abort, or a save
// with nothing changed, is not a change, so nothing is rewritten.
// truncated reports lines dropped past descriptionLineCap.
func editDescription(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int,
	filename, handle, desc string) (edited string, changed, truncated bool, err error) {
	shown := descriptionLines(desc)
	lines, saved, truncated, err := runLineEditor(s, terminal, outputMode, lineEdit{
		text: strings.Join(shown, "\n"), subject: filename, handle: handle,
		ctx:      editor.EditorContext{NodeNumber: nodeNumber, ConfArea: "File Description"},
		maxLines: descriptionLineCap(desc), clean: cleanDescriptionLine,
		what: "a file description", failed: errDescriptionEditorFailed,
	})
	if err != nil || !saved || len(lines) == 0 {
		return "", false, false, err
	}
	edited = strings.Join(lines, "\n")
	if edited == strings.Join(shown[:min(len(shown), editor.MaxLines)], "\n") {
		return "", false, false, nil
	}
	return edited, true, truncated, nil
}
