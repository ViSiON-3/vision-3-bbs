package scripting

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
	"github.com/google/uuid"
)

// registerFileMark adds the calls that work on the caller's batch queue (the
// files marked for download) to v3.file: isMarked, mark and unmark. They
// need the caller's record (Providers.CurrentUser and UserMgr); without it
// they return false.
func registerFileMark(obj *goja.Object, eng *Engine) {
	vm := eng.vm

	// isMarked(areaID, filename) — whether the file is in the batch queue.
	jsutil.Set(obj, "isMarked", func(call goja.FunctionCall) goja.Value {
		u := eng.providers.CurrentUser
		rec, ok := eng.callFile(call, false)
		return vm.ToValue(ok && u != nil && markedAt(u.TaggedFileIDs, rec.ID) >= 0)
	})

	// mark(areaID, filename) — add the file to the batch queue under the
	// same checks as DOWNLOADFILE: the area's download ACS and the queue's
	// limit. true when the file is in the queue afterwards.
	jsutil.Set(obj, "mark", func(call goja.FunctionCall) goja.Value {
		u, um := eng.providers.CurrentUser, eng.providers.UserMgr
		rec, ok := eng.callFile(call, true)
		if !ok || u == nil || um == nil {
			return vm.ToValue(false)
		}
		if markedAt(u.TaggedFileIDs, rec.ID) >= 0 {
			return vm.ToValue(true)
		}
		if len(u.TaggedFileIDs) >= file.MaxBatchFiles {
			return vm.ToValue(false)
		}
		u.TaggedFileIDs = append(u.TaggedFileIDs, rec.ID)
		if err := um.UpdateUser(u); err != nil {
			slog.Error("failed to save tagged file", "node", eng.session.NodeNumber, "error", err)
			u.TaggedFileIDs = u.TaggedFileIDs[:len(u.TaggedFileIDs)-1]
			return vm.ToValue(false)
		}
		slog.Info("tagged file", "node", eng.session.NodeNumber, "handle", u.Handle, "id", rec.ID, "file", rec.Filename)
		return vm.ToValue(true)
	})

	// unmark(areaID, filename) — take the file out of the batch queue.
	// true when it was there.
	jsutil.Set(obj, "unmark", func(call goja.FunctionCall) goja.Value {
		u, um := eng.providers.CurrentUser, eng.providers.UserMgr
		rec, ok := eng.callFile(call, false)
		if !ok || u == nil || um == nil {
			return vm.ToValue(false)
		}
		i := markedAt(u.TaggedFileIDs, rec.ID)
		if i < 0 {
			return vm.ToValue(false)
		}
		old := u.TaggedFileIDs
		u.TaggedFileIDs = append(append([]uuid.UUID(nil), old[:i]...), old[i+1:]...)
		if err := um.UpdateUser(u); err != nil {
			slog.Error("failed to save untagged file", "node", eng.session.NodeNumber, "error", err)
			u.TaggedFileIDs = old
			return vm.ToValue(false)
		}
		return vm.ToValue(true)
	})
}

// markedAt returns id's index in ids, or -1.
func markedAt(ids []uuid.UUID, id uuid.UUID) int {
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	return -1
}
