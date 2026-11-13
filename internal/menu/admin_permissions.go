package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
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
)

// authorizeUserChanges decides whether actor may save changes to target,
// whose stored record is stored, re-reading the actor's own record first so
// a level or role removed while an editor is open stops the save. A sysop
// may make any change, as before. Below sysop level the one change allowed
// is approving an account awaiting validation (validated: true, which brings
// the level up to regularUserLevel), and only with users.validate.
//
// via is where a below-sysop grant came from, for the audit log
// ("users.validate via role validator"); it is "" for a sysop. denied is the
// status message for a refused save, "" when it may go ahead.
func (e *MenuExecutor) authorizeUserChanges(um *user.UserMgr, actor, stored *user.User, changes map[string]interface{}) (via, denied string) {
	fresh := storedActor(um, actor)
	if fresh == nil {
		return "", msgAdminNoLongerAllowed
	}
	if fresh.AccessLevel >= e.GetServerConfig().SysOpLevel {
		return "", ""
	}
	d := perm.Check(fresh, perm.UsersValidate, perm.Scope{}, e.levels(), nil)
	if !d.Allowed {
		return "", msgAdminNoLongerAllowed
	}
	if approved, ok := changes["validated"].(bool); !ok || !approved || len(changes) != 1 {
		return "", msgValidatorApproveOnly
	}
	if !isPendingValidationUser(stored) {
		return "", msgValidatorNotPending
	}
	return string(perm.UsersValidate) + " via " + d.Source(), ""
}

// validatorEditorKey reports whether key does anything in the validator's
// view of the pending queue: moving, approving, saving, abandoning and
// leaving. Every other key of the shared editor edits something
// users.validate doesn't cover, or shows what it doesn't need to see.
func validatorEditorKey(key int) bool {
	switch key {
	case 'k', 'K', 'w', 'W', 'j', 'J', 's', 'S', 'q', 'Q', 'x', 'X', 'g', 'G', '\r', '\n',
		editor.KeyArrowUp, editor.KeyArrowDown, editor.KeyEsc:
		return true
	}
	return false
}
