package menu

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// editorAutocorrect sets ctx's autocorrect for u writing in area (nil for
// the auto-signature, which follows the board default). It is offered
// (ctx.Autocorrect) when the area, or the board default (config.json
// editorAutocorrect) when the area does not say, has it on; it starts off
// (ctx.AutocorrectOff) where it is offered but the caller has turned it off.
// While it is offered the caller can switch it with Ctrl-O.
func editorAutocorrect(ctx editor.EditorContext, area *message.MessageArea, boardDefault bool, u *user.User) editor.EditorContext {
	ctx.Autocorrect = area.AutocorrectOn(boardDefault)
	ctx.AutocorrectOff = ctx.Autocorrect && u != nil && u.AutocorrectOff
	return ctx
}

// withAutocorrect sets ctx up with the autocorrect decision for u writing in
// area, and a way to save the caller's choice when Ctrl-O changes it in the
// editor. It returns ctx for use inline.
func withAutocorrect(e *MenuExecutor, ctx editor.EditorContext, area *message.MessageArea, u *user.User, um *user.UserMgr) editor.EditorContext {
	ctx = editorAutocorrect(ctx, area, e.GetServerConfig().EditorAutocorrect, u)
	if !ctx.Autocorrect || u == nil {
		return ctx
	}
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
