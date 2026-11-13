package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// maxUploadDescription is the longest description, in characters, a caller
// can give: the same as at upload.
const maxUploadDescription = 60

// mayEditUpload reports whether u may change rec's description: the account
// that uploaded it, matched by ID so a new caller who takes a purged or
// renamed uploader's handle gets nothing (#1020), or whoever manages the
// files in its area (manages: files.manage there, which a co-sysop holds
// everywhere, #1044).
func mayEditUpload(rec *file.FileRecord, u *user.User, manages bool) bool {
	return manages || rec.UploadedByAccount(u.ID)
}

// runEditUpload is EDITUPLOAD, ViSiON/2's "Edit Upload" (FE): a caller
// changes the description of a file they uploaded, in the current file area,
// and a co-sysop any file's. The file is asked for by name, as the other file
// commands do. The new description is one line of up to 60 characters, as at
// upload; an empty line keeps the old one. The uploader is checked again as
// it is saved.
func runEditUpload(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}
	say := func(text string) {
		if err := e.writeString(terminal, outputMode, text); err != nil {
			slog.Debug("EDITUPLOAD write", "node", nodeNumber, "error", err)
		}
	}
	notice := func(text string) (*user.User, string, error) {
		say(text)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	areaID := currentUser.CurrentFileAreaID
	if areaID <= 0 {
		return notice(e.Strings().FileNoAreaSelected)
	}

	say("\r\n|07File to edit: |15")
	input, err := readLineFromSessionIHMax(s, terminal, 64)
	say("|07")
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	filename := strings.TrimSpace(input)
	if filename == "" {
		return currentUser, "", nil
	}
	rec, err := findFileInArea(e.FileMgr, areaID, filename)
	if err != nil {
		e.writeStringf(terminal, outputMode, e.Strings().FileNotFoundFormat, filename)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}
	// Who may manage the file's area, decided afresh each time it is asked.
	manages := func(areaID int) perm.Decision { return e.fileManagement(c.userManager, currentUser, areaID) }
	if !mayEditUpload(rec, currentUser, manages(rec.AreaID).Allowed) {
		return notice("\r\n|01That file wasn't uploaded by you.|07\r\n")
	}

	say(fmt.Sprintf("\r\n|15%s|07\r\n|08Now:|07 %s\r\n|11New description:|07 ",
		callerText(rec.Filename), callerText(strings.ReplaceAll(rec.Description, "\n", " "))))
	input, err = readLineFromSessionIHMax(s, terminal, maxUploadDescription)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	desc := sanitizeControlChars(strings.TrimSpace(input))
	if desc == "" {
		return notice("\r\n|07Description unchanged.\r\n")
	}

	// The uploader is checked again as it's saved, on the record as it is
	// on disk: the sysop may have changed it since it was looked up. A file
	// deleted or moved to another area meanwhile isn't found.
	saved := false
	var via perm.Decision
	err = e.FileMgr.UpdateFileRecord(rec.ID, func(r *file.FileRecord) {
		via = manages(r.AreaID)
		if mayEditUpload(r, currentUser, via.Allowed) {
			r.Description = desc
			saved = true
		}
	})
	if err != nil {
		slog.Error("EDITUPLOAD: saving description", "node", nodeNumber, "file", rec.Filename, "error", err)
		return notice("\r\n|01Error saving the description.|07\r\n")
	}
	if !saved {
		return notice("\r\n|01That file was changed elsewhere. Nothing was saved.|07\r\n")
	}
	if via.Allowed && !rec.UploadedByAccount(currentUser.ID) {
		logFileAction(via, "description changed", nodeNumber, currentUser.Handle, *rec)
	}
	slog.Info("caller edited an upload's description", "node", nodeNumber, "handle", currentUser.Handle, "file", rec.Filename)
	return notice("\r\n|07Description saved.\r\n")
}

// batchEntry is one file in a caller's batch, for REMOVEFROMBATCH: rec is nil
// for a file no longer on the board.
type batchEntry struct {
	id  uuid.UUID
	rec *file.FileRecord
}

// name is how the entry is shown and matched.
func (b batchEntry) name() string {
	if b.rec == nil {
		return "(no longer on the board)"
	}
	return b.rec.Filename
}

// runRemoveFromBatch is REMOVEFROMBATCH, ViSiON/2's "Remove from Batch" (FR):
// it lists the caller's batch, numbered, and takes one file off it, given by
// its number or filename. CLEAR_BATCH empties the whole batch.
func runRemoveFromBatch(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}
	say := func(text string) {
		if err := e.writeString(terminal, outputMode, text); err != nil {
			slog.Debug("REMOVEFROMBATCH write", "node", nodeNumber, "error", err)
		}
	}
	notice := func(text string) (*user.User, string, error) {
		say(text)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	if len(currentUser.TaggedFileIDs) == 0 {
		return notice(e.Strings().BatchQueueEmpty)
	}
	batch := make([]batchEntry, 0, len(currentUser.TaggedFileIDs))
	for _, id := range currentUser.TaggedFileIDs {
		b := batchEntry{id: id}
		if rec, err := e.FileMgr.GetFileRecordByID(id); err == nil {
			b.rec = rec
		}
		batch = append(batch, b)
	}

	say("\r\n")
	pager := e.newMorePager(s, terminal, outputMode, currentUser, c.termHeight, 2, "")
	for i, b := range batch {
		size := ""
		if b.rec != nil {
			size = fmt.Sprintf("%5dk", (b.rec.Size+1023)/1024)
		}
		say(fmt.Sprintf("|15%2d|07. |11%-12s |07%s\r\n", i+1, callerText(b.name()), size))
		if !pager.Line() {
			return pager.stopResult(currentUser)
		}
	}

	say("\r\n|07Remove which file (|15#|07 or name): |15")
	input, err := readLineFromSessionIHMax(s, terminal, 64)
	say("|07")
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return currentUser, "", nil
	}
	pick := -1
	if n, convErr := strconv.Atoi(input); convErr == nil {
		if n >= 1 && n <= len(batch) {
			pick = n - 1
		}
	} else {
		for i, b := range batch {
			if b.rec != nil && strings.EqualFold(b.rec.Filename, input) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return notice("\r\n|01That file isn't in your batch.|07\r\n")
	}

	old := currentUser.TaggedFileIDs
	kept := make([]uuid.UUID, 0, len(old)-1)
	for _, id := range old {
		if id != batch[pick].id {
			kept = append(kept, id)
		}
	}
	currentUser.TaggedFileIDs = kept
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("REMOVEFROMBATCH: saving the batch", "node", nodeNumber, "error", err)
		currentUser.TaggedFileIDs = old
		return notice(e.Strings().SaveUserError)
	}
	slog.Info("removed a file from the batch", "node", nodeNumber, "handle", currentUser.Handle, "id", batch[pick].id)
	return notice(fmt.Sprintf("\r\n|11%s|07 is off your batch: |15%d|07 left.\r\n", callerText(batch[pick].name()), len(kept)))
}
