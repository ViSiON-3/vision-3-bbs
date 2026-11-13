package menu

import (
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Admin permissions below sysop level (#610). A sysop keeps every admin
// function exactly as before; a user below sysOpLevel gets an admin action
// only through a role in configs/roles.json that grants its permission.
// perm.Check decides both here and for the ACS term PERM:, so a menu command
// gated on PERM:users.validate and the handler behind it agree.

// storedActor is the acting user's record as stored now, with any edit made
// in ./ue picked up, or nil when the account no longer exists or is deleted.
// Without a user manager (tests, headless paths) it is the session's copy.
func storedActor(um *user.UserMgr, actor *user.User) *user.User {
	if actor == nil {
		return nil
	}
	if um == nil {
		return actor
	}
	stored, ok := um.CurrentUserByID(actor.ID)
	if !ok || stored.DeletedUser {
		return nil
	}
	return stored
}

// levels are config.json's sysOpLevel and coSysOpLevel, for perm.Check.
func (e *MenuExecutor) levels() perm.Levels {
	cfg := e.GetServerConfig()
	return perm.Levels{SysOp: cfg.SysOpLevel, CoSysOp: cfg.CoSysOpLevel}
}

// permission decides whether actor holds p, which isn't scoped, from the
// stored record.
func (e *MenuExecutor) permission(um *user.UserMgr, actor *user.User, p perm.Permission) perm.Decision {
	return perm.Check(storedActor(um, actor), p, perm.Scope{}, e.levels(), nil)
}

// areaScope is message area areaID as perm.Check asks about it: its tag,
// its conference's tag and its sponsor, read afresh so a sponsor the sysop
// has just changed counts at once. An unknown area is the zero Scope.
func (e *MenuExecutor) areaScope(areaID int) perm.Scope {
	if e.MessageMgr == nil || areaID <= 0 {
		return perm.Scope{}
	}
	area, ok := e.MessageMgr.GetAreaByID(areaID)
	if !ok || area == nil {
		return perm.Scope{}
	}
	scope := perm.Scope{Area: area.Tag}
	if area.Sponsor != "" && area.SponsorID > 0 {
		scope.SponsorID = area.SponsorID
	}
	if e.ConferenceMgr != nil && area.ConferenceID != 0 {
		if conf, ok := e.ConferenceMgr.GetByID(area.ConferenceID); ok && conf != nil {
			scope.Conference = conf.Tag
		}
	}
	return scope
}

// currentAreaScope is u's current message area, for the ACS term PERM.
func (e *MenuExecutor) currentAreaScope(u *user.User) perm.Scope {
	return e.areaScope(u.CurrentMessageAreaID)
}

// moderation decides whether actor may moderate message area areaID
// (messages.moderate): co-sysop level and up anywhere, the area's sponsor
// there, or a role that grants it there. It reads the actor's stored record
// and the area afresh, so it is called again just before a delete.
func (e *MenuExecutor) moderation(um *user.UserMgr, actor *user.User, areaID int) perm.Decision {
	return perm.Check(storedActor(um, actor), perm.MessagesModerate, e.areaScope(areaID), e.levels(), nil)
}

// fileAreaScope is file area areaID for a permission scoped to file areas
// (files.manage): its tag and its conference's tag, read afresh. File areas
// have no sponsor. An unknown area is the zero Scope.
func (e *MenuExecutor) fileAreaScope(areaID int) perm.Scope {
	if e.FileMgr == nil || areaID <= 0 {
		return perm.Scope{}
	}
	area, ok := e.FileMgr.GetAreaByID(areaID)
	if !ok || area == nil {
		return perm.Scope{}
	}
	scope := perm.Scope{Area: area.Tag}
	if e.ConferenceMgr != nil && area.ConferenceID != 0 {
		if conf, ok := e.ConferenceMgr.GetByID(area.ConferenceID); ok && conf != nil {
			scope.Conference = conf.Tag
		}
	}
	return scope
}

// currentFileAreaScope is u's current file area, for the ACS term PERM.
func (e *MenuExecutor) currentFileAreaScope(u *user.User) perm.Scope {
	return e.fileAreaScope(u.CurrentFileAreaID)
}

// fileManagement decides whether actor may manage the files in file area
// areaID (files.manage): co-sysop level and up anywhere, or a role that
// grants it there. It reads the actor's stored record and the area afresh,
// so each action calls it again just before it changes anything.
func (e *MenuExecutor) fileManagement(um *user.UserMgr, actor *user.User, areaID int) perm.Decision {
	return perm.Check(storedActor(um, actor), perm.FilesManage, e.fileAreaScope(areaID), e.levels(), nil)
}

// fileRecordGone reports whether file id is no longer in area areaID: moved
// to another area or deleted. A change authorised for areaID is refused then.
func (e *MenuExecutor) fileRecordGone(id uuid.UUID, areaID int) bool {
	rec, err := e.FileMgr.GetFileRecordByID(id)
	return err != nil || rec == nil || rec.AreaID != areaID
}

// managedFileAreas lists the file areas actor manages, in listing order.
func (e *MenuExecutor) managedFileAreas(um *user.UserMgr, actor *user.User) []file.FileArea {
	if e.FileMgr == nil {
		return nil
	}
	var out []file.FileArea
	for _, a := range e.FileMgr.ListAreas() {
		if e.fileManagement(um, actor, a.ID).Allowed {
			out = append(out, a)
		}
	}
	return out
}

// logFileAction logs a change made to a file through files.manage below
// co-sysop level, with the grant that allowed it, as moderator deletes are.
// A co-sysop's actions keep their existing log lines.
func logFileAction(d perm.Decision, action string, nodeNumber int, handle string, rec file.FileRecord, attrs ...any) {
	if d.Level {
		return
	}
	args := append([]any{"node", nodeNumber, "handle", handle, "file", rec.Filename, "area", rec.AreaID, "via", d.Source()}, attrs...)
	slog.Info("file "+action+" by file manager", args...)
}

// mayValidateUsers reports whether actor may open the pending-validation
// queue: a sysop, or a holder of users.validate.
func (e *MenuExecutor) mayValidateUsers(um *user.UserMgr, actor *user.User) bool {
	return e.permission(um, actor, perm.UsersValidate).Allowed
}

// Messages for a save the actor isn't allowed to make.
const (
	msgAdminNoLongerAllowed = "|01Access denied: your account no longer has that permission.|07"
	msgValidatorApproveOnly = "|01Access denied: you may only approve an account awaiting validation.|07"
	msgValidatorNotPending  = "|01That account is no longer awaiting validation.|07"
	msgAdminAboveCeiling    = "|01Access denied: your role reaches only accounts below co-sysop level.|07"
	msgAdminFieldNotAllowed = "|01Access denied: your role doesn't allow that change.|07"
	msgAdminNoSelfBan       = "|01You can't ban yourself.|07"
)

// authorizeUserChanges decides whether actor may save changes to stored, the
// account as it is on disk. A sysop may make any change. Below sysop level
// every change needs a permission, read from actor's stored record (#610,
// #1045):
//
//   - users.validate approves an account awaiting validation:
//     {"validated": true} and nothing else.
//   - users.edit changes realname, grouploc and note.
//   - users.ban bans ({"validated": false, "level": 0}), un-validates
//     ({"validated": false}) and un-bans a banned account back to
//     regularUserLevel ({"validated": true, "level": regular}).
//
// users.edit and users.ban reach only accounts below coSysOpLevel, never
// User #1, and nobody bans themselves. Any other field (handle, level, flags,
// roles, groups, password, deletion, the chat bar) stays the sysop's. via
// names the grants used, for the audit log; denied is the message when the
// save is refused.
func (e *MenuExecutor) authorizeUserChanges(um *user.UserMgr, actor, stored *user.User, changes map[string]interface{}) (via, denied string) {
	fresh := storedActor(um, actor)
	if fresh == nil {
		return "", msgAdminNoLongerAllowed
	}
	cfg := e.GetServerConfig()
	if fresh.AccessLevel >= cfg.SysOpLevel {
		return "", ""
	}
	hold := func(p perm.Permission) perm.Decision { return perm.Check(fresh, p, perm.Scope{}, e.levels(), nil) }
	validate, edit, ban := hold(perm.UsersValidate), hold(perm.UsersEdit), hold(perm.UsersBan)

	// Approving a pending account is users.validate's, as before.
	if approved, ok := changes["validated"].(bool); ok && approved && len(changes) == 1 && validate.Allowed {
		if !isPendingValidationUser(stored) {
			return "", msgValidatorNotPending
		}
		return string(perm.UsersValidate) + " via " + validate.Source(), ""
	}
	if !edit.Allowed && !ban.Allowed {
		if !validate.Allowed {
			return "", msgAdminNoLongerAllowed
		}
		if approved, ok := changes["validated"].(bool); !ok || !approved || len(changes) != 1 {
			return "", msgValidatorApproveOnly
		}
		return "", msgValidatorNotPending
	}

	if stored == nil || stored.ID == 1 || (cfg.CoSysOpLevel > 0 && stored.AccessLevel >= cfg.CoSysOpLevel) {
		return "", msgAdminAboveCeiling
	}
	need := map[perm.Permission]bool{}
	for field := range changes {
		switch field {
		case "realname", "grouploc", "note":
			need[perm.UsersEdit] = true
		case "validated", "level":
		default:
			return "", msgAdminFieldNotAllowed
		}
	}
	if _, v := changes["validated"]; v || changes["level"] != nil {
		validated, hasValidated := changes["validated"].(bool)
		level, hasLevel := changes["level"].(int)
		regular := cfg.RegularUserLevel
		if regular <= 0 {
			regular = 10
		}
		banned := stored.AccessLevel == 0 && !stored.Validated
		switch {
		case hasValidated && !validated && hasLevel && level == 0: // ban
			if stored.ID == fresh.ID {
				return "", msgAdminNoSelfBan
			}
		case hasValidated && !validated && !hasLevel: // un-validate
		case hasValidated && validated && hasLevel && level == regular && banned: // un-ban
		default:
			return "", msgAdminFieldNotAllowed
		}
		need[perm.UsersBan] = true
	}
	var grants []string
	for _, p := range []perm.Permission{perm.UsersEdit, perm.UsersBan} {
		if !need[p] {
			continue
		}
		d := map[perm.Permission]perm.Decision{perm.UsersEdit: edit, perm.UsersBan: ban}[p]
		if !d.Allowed {
			return "", msgAdminFieldNotAllowed
		}
		grants = append(grants, string(p)+" via "+d.Source())
	}
	return strings.Join(grants, "; "), ""
}

// mayManageUsers reports whether actor, below sysop level, holds users.edit
// or users.ban, which open the user editor's limited view: edit and ban
// tell which.
func (e *MenuExecutor) mayManageUsers(um *user.UserMgr, actor *user.User) (edit, ban bool) {
	return e.permission(um, actor, perm.UsersEdit).Allowed, e.permission(um, actor, perm.UsersBan).Allowed
}

// mayBanUsers reports whether actor may use BANUSER and UNVALIDATEUSER: a
// sysop, or a holder of users.ban.
func (e *MenuExecutor) mayBanUsers(um *user.UserMgr, actor *user.User) bool {
	return e.permission(um, actor, perm.UsersBan).Allowed
}

// belowUserCeiling reports whether u is an account users.edit and users.ban
// reach: below coSysOpLevel, not User #1, and not deleted.
func (e *MenuExecutor) belowUserCeiling(u *user.User) bool {
	cfg := e.GetServerConfig()
	return u != nil && u.ID != 1 && !u.DeletedUser && (cfg.CoSysOpLevel <= 0 || u.AccessLevel < cfg.CoSysOpLevel)
}

// validatorEditorKey reports whether key does anything in the validator's
// view of the pending queue: moving, approving, saving, abandoning and
// leaving. Every other key of the shared editor edits something
// users.validate doesn't cover, or shows what it doesn't need to see.
// limitedEditorKey reports whether key works in the editor's users.edit /
// users.ban view: moving, saving and quitting, B/C/D (real name,
// group/location, note) with users.edit, and G (validated) and 0 (ban)
// with users.ban.
func limitedEditorKey(key int, edit, ban bool) bool {
	switch key {
	case 'k', 'K', 'w', 'W', 'j', 'J', 's', 'S', 'q', 'Q', 'x', 'X', '\r', '\n',
		editor.KeyArrowUp, editor.KeyArrowDown, editor.KeyEsc:
		return true
	case 'b', 'B', 'c', 'C', 'd', 'D':
		return edit
	case 'g', 'G', '0':
		return ban
	}
	return false
}

func validatorEditorKey(key int) bool {
	switch key {
	case 'k', 'K', 'w', 'W', 'j', 'J', 's', 'S', 'q', 'Q', 'x', 'X', 'g', 'G', '\r', '\n',
		editor.KeyArrowUp, editor.KeyArrowDown, editor.KeyEsc:
		return true
	}
	return false
}
