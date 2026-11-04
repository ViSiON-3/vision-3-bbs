package menu

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// editorAutocorrect decides the message editor's autocorrect for u writing
// in area (nil for the auto-signature, which has none). offered is whether
// the area, or the board default (config.json editorAutocorrect) when the
// area does not say, has it on; on is whether it starts on, which also needs
// the caller not to have turned it off. While it is offered the caller can
// switch it with Ctrl-O.
func editorAutocorrect(area *message.MessageArea, boardDefault bool, u *user.User) (offered, on bool) {
	offered = area.AutocorrectOn(boardDefault)
	return offered, offered && (u == nil || !u.AutocorrectOff)
}

// withAutocorrect sets ctx up with the autocorrect decision for u writing in
// area, and a way to save the caller's choice when Ctrl-O changes it in the
// editor. It returns ctx for use inline.
func withAutocorrect(e *MenuExecutor, ctx editor.EditorContext, area *message.MessageArea, u *user.User, um *user.UserMgr) editor.EditorContext {
	offered, _ := editorAutocorrect(area, e.GetServerConfig().EditorAutocorrect, u)
	if !offered {
		return ctx
	}
	ctx.Autocorrect = true
	if u == nil {
		return ctx
	}
	ctx.AutocorrectOff = u.AutocorrectOff
	ctx.SaveAutocorrect = func(on bool) {
		u.AutocorrectOff = !on
		if um == nil {
			return
		}
		if err := um.UpdateUser(u); err != nil {
			slog.Warn("saving autocorrect choice", "handle", u.Handle, "error", err)
		}
	}
	return ctx
}

// autocorrectOffered reports whether the editor offers autocorrect anywhere
// on the board: by default, or in at least one message area. User Konfig
// shows the caller's switch only when it does.
func autocorrectOffered(e *MenuExecutor) bool {
	def := e.GetServerConfig().EditorAutocorrect
	if def {
		return true
	}
	if e.MessageMgr == nil {
		return false
	}
	for _, a := range e.MessageMgr.ListAreas() {
		if a.AutocorrectOn(def) {
			return true
		}
	}
	return false
}
