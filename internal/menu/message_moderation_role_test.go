package menu

import (
	"testing"
	"time"
)

// moderatorRoles are roles granting messages.moderate in GENERAL by area
// tag, in conference LOCAL (GENERAL's) by conference tag, everywhere, and in
// another area only.
const moderatorRoles = `{"roles": [
	{"name": "general_mod", "permissions": [{"permission": "messages.moderate", "areas": ["general"]}]},
	{"name": "local_mod", "permissions": [{"permission": "messages.moderate", "conferences": ["LOCAL"]}]},
	{"name": "all_mod", "permissions": ["messages.moderate"]},
	{"name": "other_mod", "permissions": [{"permission": "messages.moderate", "areas": ["SYSOP"]}]}
]}`

// giveCallerRoles stores roles on the caller (level 30, below co-sysop) and
// reloads env.caller.
func giveCallerRoles(env *menuEnv, roles ...string) {
	env.t.Helper()
	u, _ := env.um.GetUserByID(2)
	u.Roles = roles
	if err := env.um.UpdateUserByID(u); err != nil {
		env.t.Fatal(err)
	}
	env.caller, _ = env.um.GetUserByID(2)
}

// A role granting messages.moderate where the reader is lets its member
// delete any message there, as a sponsor can: by area tag, by the area's
// conference, or everywhere.
func TestMessageReaderRoleModeratorDeletes(t *testing.T) {
	for _, role := range []string{"general_mod", "local_mod", "all_mod"} {
		t.Run(role, func(t *testing.T) {
			useRoles(t, moderatorRoles)
			env := newMsgEnv(t)
			env.generalMsgs(3)
			giveCallerRoles(env, role)

			r := env.runCmd("READMSGS", env.caller, "", "DY\x1b")
			if !r.has(" Del ", "Delete this message?") {
				t.Errorf("no delete offered; output:\n%s", r.text())
			}
			if n := env.msgCount(generalAreaID); n != 2 {
				t.Fatalf("GENERAL has %d messages after the moderator's delete, want 2", n)
			}
		})
	}
}

// A grant for another area gives nothing here, and neither does a role
// roles.json doesn't define.
func TestMessageReaderRoleModeratorOnlyWhereGranted(t *testing.T) {
	for _, role := range []string{"other_mod", "undefined_mod"} {
		t.Run(role, func(t *testing.T) {
			useRoles(t, moderatorRoles)
			env := newMsgEnv(t)
			env.generalMsgs(2)
			giveCallerRoles(env, role)

			r := env.runCmd("READMSGS", env.caller, "", "DY\x1b")
			if r.has(" Del ") || r.has("Delete this message?") {
				t.Errorf("delete offered outside the grant; output:\n%s", r.text())
			}
			if n := env.msgCount(generalAreaID); n != 2 {
				t.Errorf("GENERAL has %d messages, want 2", n)
			}
		})
	}
}

// A grant removed from roles.json while the delete is being confirmed
// deletes nothing.
func TestMessageReaderRoleModeratorRevokedBeforeConfirm(t *testing.T) {
	useRoles(t, moderatorRoles)
	env := newMsgEnv(t)
	env.generalMsgs(2)
	giveCallerRoles(env, "general_mod")
	env.whenOutput("Delete this message?", func() { useRoles(t, `{"roles": []}`) })

	r := env.runCmd("READMSGS", env.caller, "", "DY\x1b")
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("GENERAL has %d messages, want 2: the delete went ahead after the role was removed", n)
	}
	if !r.has("You can no longer delete messages in this area.") {
		t.Errorf("no notice; output:\n%s", r.text())
	}
}

// PERM:messages.moderate in a menu answers for the caller's current area,
// as the reader does.
func TestPermModerateFollowsCurrentArea(t *testing.T) {
	useRoles(t, moderatorRoles)
	env := newMsgEnv(t)
	giveCallerRoles(env, "general_mod")
	if !checkACS("PERM:messages.moderate", env.caller, nil, nil, time.Now()) {
		t.Error("PERM:messages.moderate fails in GENERAL for its moderator")
	}
	inMail := *env.caller
	inMail.CurrentMessageAreaID, inMail.CurrentMessageAreaTag = privmailAreaID, "PRIVMAIL"
	if checkACS("PERM:messages.moderate", &inMail, nil, nil, time.Now()) {
		t.Error("PERM:messages.moderate passes in PRIVMAIL")
	}
	// A sponsor holds it in their area, and a co-sysop everywhere.
	giveCallerRoles(env)
	sponsorSetSponsor(env, generalAreaID, "caller")
	if !checkACS("PERM:messages.moderate", env.caller, nil, nil, time.Now()) {
		t.Error("PERM:messages.moderate fails for GENERAL's sponsor")
	}
	cosysop := *env.caller
	cosysop.AccessLevel = env.e.GetServerConfig().CoSysOpLevel
	cosysop.CurrentMessageAreaID, cosysop.CurrentMessageAreaTag = privmailAreaID, "PRIVMAIL"
	if !checkACS("PERM:messages.moderate", &cosysop, nil, nil, time.Now()) {
		t.Error("PERM:messages.moderate fails for a co-sysop")
	}
}
