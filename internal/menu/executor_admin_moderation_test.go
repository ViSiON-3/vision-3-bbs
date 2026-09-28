package menu

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// modAdminLog reads the admin activity log from the data directory. A missing
// file is an empty log.
func modAdminLog(t *testing.T, env *menuEnv) []user.AdminActivityLog {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(env.dataDir(), "admin_activity.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var logs []user.AdminActivityLog
	if err := json.Unmarshal(b, &logs); err != nil {
		t.Fatalf("parse admin log: %v", err)
	}
	return logs
}

// modLogField returns the admin log entry for field on target, if any.
func modLogField(logs []user.AdminActivityLog, target int, field string) (user.AdminActivityLog, bool) {
	for _, l := range logs {
		if l.TargetUserID == target && l.FieldName == field {
			return l, true
		}
	}
	return user.AdminActivityLog{}, false
}

// TestSysopCommandsRefuseNonSysop pins that every sysop-only moderation
// command refuses a regular caller with "Access denied." and changes nothing
// on disk. Each case runs on its own env.
func TestSysopCommandsRefuseNonSysop(t *testing.T) {
	for _, cmd := range []string{"BANUSER", "DELETEUSER", "UNVALIDATEUSER", "PURGEUSERS", "ADMINLISTUSERS", "VALIDATEUSER", "TOGGLEALLOWNEWUSERS"} {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			env := newMenuEnv(t)
			r := env.runCmd(cmd, env.caller, "", "\r\rY\r\r")
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			if !r.has("Access denied.") {
				t.Errorf("output lacks refusal:\n%s", r.text())
			}
			if r.has("Sysop") {
				t.Errorf("refused caller was shown the user list:\n%s", r.text())
			}
			if u, _ := env.diskUser(2); u.AccessLevel != 10 || !u.Validated || u.DeletedUser {
				t.Errorf("caller record changed on refusal: %+v", u)
			}
			if logs := modAdminLog(t, env); len(logs) != 0 {
				t.Errorf("refusal wrote admin log: %+v", logs)
			}
		})
	}
}

// TestModerationRequiresLogin pins that the ban and delete commands refuse
// a session with no user rather than dereferencing it.
func TestModerationRequiresLogin(t *testing.T) {
	for _, cmd := range []string{"BANUSER", "DELETEUSER", "UNVALIDATEUSER", "PURGEUSERS"} {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			env := newMenuEnv(t)
			r := env.runCmd(cmd, nil, "", "\r")
			if r.err != nil || !r.has("You must be logged in") {
				t.Errorf("err = %v, output:\n%s", r.err, r.text())
			}
		})
	}
}

// TestBanUser_BansSelectedUser pins the happy path: picking the caller and
// confirming sets level 0 and unvalidated on disk, reports it, and writes an
// admin audit entry per changed field attributed to the sysop.
func TestBanUser_BansSelectedUser(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("BANUSER", env.sysop, "", "j\rY\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Ban User", "User banned: Caller") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	if u.AccessLevel != 0 || u.Validated {
		t.Errorf("caller on disk = level %d validated %v, want 0/false", u.AccessLevel, u.Validated)
	}
	logs := modAdminLog(t, env)
	for _, f := range []string{"level", "validated"} {
		l, ok := modLogField(logs, 2, f)
		if !ok {
			t.Errorf("no admin log entry for %s: %+v", f, logs)
			continue
		}
		if l.AdminID != 1 || l.AdminHandle != "Sysop" {
			t.Errorf("%s entry attributed to %d/%q", f, l.AdminID, l.AdminHandle)
		}
	}
	if l, _ := modLogField(logs, 2, "level"); l.OldValue != "10" || l.NewValue != "0" {
		t.Errorf("level log old/new = %q/%q, want 10/0", l.OldValue, l.NewValue)
	}
}

// TestBanUser_ProtectsUserOne pins that selecting User #1 is refused before
// any confirmation prompt, leaving the sysop intact.
func TestBanUser_ProtectsUserOne(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("BANUSER", env.sysop, "", "\r\r")
	if !r.has("Cannot ban User #1!") || r.has("set level 0") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.diskUser(1); u.AccessLevel != 255 || !u.Validated {
		t.Errorf("sysop changed: %+v", u)
	}
}

// TestBanUser_DeclineAndQuitChangeNothing pins that answering No at the
// confirmation, or quitting the picker, leaves the target and the audit log
// untouched.
func TestBanUser_DeclineAndQuitChangeNothing(t *testing.T) {
	for name, input := range map[string]string{"decline": "j\rN\r", "quit": "q"} {
		t.Run(name, func(t *testing.T) {
			env := newMenuEnv(t)
			r := env.runCmd("BANUSER", env.sysop, "", input)
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			if name == "decline" && !r.has("Cancelled.") {
				t.Errorf("output:\n%s", r.text())
			}
			if u, _ := env.diskUser(2); u.AccessLevel != 10 || !u.Validated {
				t.Errorf("caller changed: %+v", u)
			}
			if logs := modAdminLog(t, env); len(logs) != 0 {
				t.Errorf("admin log written: %+v", logs)
			}
		})
	}
}

// TestBanUser_DisconnectAtPickerLogsOff pins that running out of input in
// the picker is treated as a disconnect.
func TestBanUser_DisconnectAtPickerLogsOff(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("BANUSER", env.sysop, "", "j"); r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
}

// TestModeration_EmptyUserList pins the "No users found." screen when the
// user file is empty (the acting sysop is not in it).
func TestModeration_EmptyUserList(t *testing.T) {
	for _, cmd := range []string{"BANUSER", "DELETEUSER", "UNVALIDATEUSER", "ADMINLISTUSERS"} {
		t.Run(cmd, func(t *testing.T) {
			env := newMenuEnv(t)
			sysop := env.sysop
			env.writeUsers()
			r := env.runCmd(cmd, sysop, "", "\r")
			if r.err != nil || !r.has("No users found.") {
				t.Errorf("err = %v output:\n%s", r.err, r.text())
			}
		})
	}
}

// TestDeleteUser_SoftDeletesSelectedUser pins that DELETEUSER marks the
// record deleted with a timestamp but keeps it on disk, and audits it.
func TestDeleteUser_SoftDeletesSelectedUser(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("DELETEUSER", env.sysop, "", "j\rY\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("User deleted: Caller") {
		t.Errorf("output:\n%s", r.text())
	}
	u, ok := env.diskUser(2)
	if !ok {
		t.Fatal("soft delete removed the record")
	}
	if !u.DeletedUser || u.DeletedAt == nil {
		t.Errorf("caller on disk: deleted=%v deletedAt=%v", u.DeletedUser, u.DeletedAt)
	}
	if u.Handle != "Caller" || u.AccessLevel != 10 {
		t.Errorf("soft delete altered other data: %+v", u)
	}
	l, ok := modLogField(modAdminLog(t, env), 2, "deleted")
	if !ok || l.OldValue != "false" || l.NewValue != "true" {
		t.Errorf("deleted audit entry = %+v (found %v)", l, ok)
	}
}

// TestDeleteUser_ProtectsUserOneAndHonoursNo pins that User #1 cannot be
// deleted and that declining the confirmation keeps the target.
func TestDeleteUser_ProtectsUserOneAndHonoursNo(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("DELETEUSER", env.sysop, "", "\r\r"); !r.has("Cannot delete User #1!") {
		t.Errorf("user #1 output:\n%s", r.text())
	}
	if r := env.runCmd("DELETEUSER", env.sysop, "", "j\rN\r"); !r.has("Cancelled.") {
		t.Errorf("decline output:\n%s", r.text())
	}
	for _, id := range []int{1, 2} {
		if u, _ := env.diskUser(id); u.DeletedUser {
			t.Errorf("user %d deleted", id)
		}
	}
	if logs := modAdminLog(t, env); len(logs) != 0 {
		t.Errorf("admin log written: %+v", logs)
	}
}
