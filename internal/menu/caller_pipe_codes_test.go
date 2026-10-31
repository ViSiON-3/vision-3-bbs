package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// hostileUser is an old account whose caller-set fields hold every screen code,
// as signup and the user editors used to allow.
func hostileUser(id int) *user.User {
	return &user.User{ID: id, Handle: chatScreenCodesMsg, RealName: chatScreenCodesMsg, GroupLocation: chatScreenCodesMsg,
		PrivateNote: chatScreenCodesMsg, AccessLevel: 10, Validated: true, TimeLimit: 60}
}

// The admin screens name the user they act on, and an old handle holding |CL
// must print on them instead of clearing the sysop's screen.
func TestAdminScreensCannotRunHandleScreenCodes(t *testing.T) {
	for _, cmd := range []string{"BANUSER", "DELETEUSER", "UNVALIDATEUSER"} {
		t.Run(cmd, func(t *testing.T) {
			env := newMenuEnv(t)
			env.seedUsers(hostileUser(3))
			r := env.runCmd(cmd, env.sysop, "", "jj\rY\r\r")
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			t.Logf("sysop received %q", r.raw)
			chatCheckNoScreenCodes(t, cmd, r.raw)
			if !strings.Contains(r.raw, chatScreenCodesMsg) {
				t.Errorf("the handle is not shown as typed: %q", r.raw)
			}
		})
	}
}

// A colour code in an old handle still colours it, as it does in chat.
func TestAdminScreensKeepHandleColourCodes(t *testing.T) {
	env := newMenuEnv(t)
	u := hostileUser(3)
	u.Handle = "|04Red"
	env.seedUsers(u)
	r := env.runCmd("BANUSER", env.sysop, "", "jj\rY\r\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if want := string(ansi.ReplacePipeCodes([]byte("|04Red"))); !strings.Contains(r.raw, want) {
		t.Errorf("output lacks the coloured handle %q: %q", want, r.raw)
	}
}

// The listings put caller-set fields into a template that has been expanded
// already, by expanding each field on its own.
func TestListingsCannotRunCallerScreenCodes(t *testing.T) {
	t.Run("LISTUSERS", func(t *testing.T) {
		env := newMenuEnv(t)
		env.seedUsers(hostileUser(3))
		r := env.runCmd("LISTUSERS", env.caller, "", "\r")
		if r.err != nil {
			t.Fatalf("err = %v", r.err)
		}
		chatCheckNoScreenCodes(t, "the user list", r.raw)
	})
	t.Run("LASTCALLERS", func(t *testing.T) {
		env := newMenuEnv(t)
		env.seedUsers(hostileUser(3))
		env.um.AddCallRecord(user.CallRecord{UserID: 3, Handle: chatScreenCodesMsg, GroupLocation: chatScreenCodesMsg, NodeID: 1})
		r := env.runCmd("LASTCALLERS", env.caller, "", "\r")
		if r.err != nil {
			t.Fatalf("err = %v", r.err)
		}
		if !strings.Contains(r.raw, "a|CL") {
			t.Fatalf("the call is not listed: %q", r.raw)
		}
		chatCheckNoScreenCodes(t, "last callers", r.raw)
	})
	t.Run("ONELINER", func(t *testing.T) {
		env, path := onelinerEnv(t)
		recs := []onelinerRecord{{Text: chatScreenCodesMsg + " ENDMARK", PostedByHandle: "x|CLy"}, {Text: "|04tinted", PostedByHandle: "Poster"}}
		if err := saveOnelinerRecords(path, recs); err != nil {
			t.Fatal(err)
		}
		r := env.runCmd("ONELINER", env.caller, "", "N")
		if !strings.Contains(r.raw, "ENDMARK") {
			t.Fatalf("the one-liner is not shown: %q", r.raw)
		}
		chatCheckNoScreenCodes(t, "a one-liner", r.raw)
		if strings.Contains(r.raw, "x\x1b[2J") {
			t.Errorf("a one-liner's poster ran a screen code: %q", r.raw)
		}
		if want := string(ansi.ReplacePipeCodes([]byte("|04tinted"))); !strings.Contains(r.raw, want) {
			t.Errorf("a one-liner lost its colour code: %q", r.raw)
		}
	})
}

// The central substitutions: prompts and templates keep their own codes and the
// caller's fields only get to colour.
func TestUserPlaceholdersOnlyColour(t *testing.T) {
	env := newMenuEnv(t)
	u := hostileUser(3)
	placeholders := map[string]string{}
	applyUserPlaceholders(placeholders, u)
	for _, key := range []string{"|UH", "|ALIAS", "|HANDLE", "|NAME", "|GL", "|UN"} {
		got := string(env.e.renderPromptText("|CL"+key, placeholders, 0, 0, 0))
		if !strings.HasPrefix(got, "\x1b[2J") {
			t.Errorf("%s: the prompt's own |CL did not run: %q", key, got)
		}
		chatCheckNoScreenCodes(t, key, got)
	}

	tokens := string(ansi.ReplacePipeCodes(env.e.applyCommonTemplateTokens([]byte("|UH |ALIAS |HANDLE"), u, 1)))
	chatCheckNoScreenCodes(t, "the template tokens", tokens)

	u.Handle = "|04Red"
	tokens = string(ansi.ReplacePipeCodes(env.e.applyCommonTemplateTokens([]byte("|UH"), u, 1)))
	if want := string(ansi.ReplacePipeCodes([]byte("|04Red"))); tokens != want {
		t.Errorf("|UH = %q, want the colour kept: %q", tokens, want)
	}
}

// writeStringf and writeStringSubst are the central points for strings.json
// lines, so they escape arguments and keep the format's codes. A script gets the
// text as it is.
func TestStringArgumentsOnlyColour(t *testing.T) {
	got := formatStringValue("|CL%s %d", []any{chatScreenCodesMsg, 7})
	out := string(ansi.ReplacePipeCodes([]byte(got)))
	if !strings.HasPrefix(out, "\x1b[2J") {
		t.Errorf("the format's own |CL did not run: %q", out)
	}
	chatCheckNoScreenCodes(t, "a string argument", out)
	if !strings.HasSuffix(out, " 7") {
		t.Errorf("a number argument changed: %q", out)
	}

	f := newStringHookFixture(t, "")
	if out := f.write(t, "!say extra", chatScreenCodesMsg); !strings.Contains(out, chatScreenCodesMsg) {
		t.Errorf("a script did not get the argument as it is: %q", out)
	}
}

// A FILE_ID.DIZ comes from whoever made the archive, and its later lines are
// expanded on their own under the first.
func TestFileListContinuationsCannotRunScreenCodes(t *testing.T) {
	rec := file.FileRecord{Filename: "A.ZIP", Description: "first\n" + chatScreenCodesMsg}
	u := &user.User{}
	_, more := formatFileListLine(rec, u, false, "^DESC", 1)
	if len(more) != 1 {
		t.Fatalf("continuations = %q", more)
	}
	chatCheckNoScreenCodes(t, "a description line", string(ansi.ReplacePipeCodes([]byte(more[0]))))
}

func TestValidateHandleRejectsPipe(t *testing.T) {
	for _, h := range []string{"|CL", "Bad|Guy", "abc|", "|04Red", "||x"} {
		if validateHandle(h) {
			t.Errorf("validateHandle(%q) = true, want false", h)
		}
	}
}

func TestSignupRefusesPipesWithClearMessage(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)
	in := "Y\r" + "|CLHack\r" + "Fresh\r" + "pw123\rpw123\r" + "Fr|CLesh Face\r" + "Fresh Face\r\rHere\r\r"
	r := env.runCmd("NEWUSER", nil, "", in)
	if !r.has("Handles cannot contain the | character", "Real names cannot contain the | character") {
		t.Errorf("output lacks the pipe messages: %q", r.text())
	}
	if _, ok := env.um.GetUser("|CLHack"); ok {
		t.Error("the handle with a pipe was accepted")
	}
	got, ok := env.um.GetUser("Fresh")
	if !ok || got.RealName != "Fresh Face" {
		t.Errorf("the retry was not accepted: %+v", got)
	}
}

// An old account can save other settings, and leave its name as it is, but
// cannot set a new name with a pipe.
func TestOldAccountKeepsPipeNameAndSavesOtherSettings(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	u.RealName = "Old|CL Name"
	if err := um.UpdateUser(u); err != nil {
		t.Fatal(err)
	}

	// [G] is the real name, [H] the location.
	_, got, _ := runKonfig(t, um, u, "h"+keyClear+"Elsewhere\rq", "")
	if got.GroupLocation != "Elsewhere" || got.RealName != "Old|CL Name" {
		t.Fatalf("location = %q, real name = %q", got.GroupLocation, got.RealName)
	}
	_, got, _ = runKonfig(t, um, got, "g\rq", "")
	if got.RealName != "Old|CL Name" {
		t.Fatalf("leaving the name alone changed it to %q", got.RealName)
	}
	screen, got, _ := runKonfig(t, um, got, "g"+keyClear+"New|CL Name\rq", "")
	if got.RealName != "Old|CL Name" {
		t.Fatalf("real name = %q, want the new one refused", got.RealName)
	}
	if !strings.Contains(screen.Row(konfigEditRow), "cannot contain") {
		t.Errorf("status row = %q, want the pipe message", screen.Row(konfigEditRow))
	}
}

func TestAdminEditorRefusesPipeRename(t *testing.T) {
	env := newMenuEnv(t)
	target, _ := env.um.GetUser("Caller")
	orig := map[int]time.Time{target.ID: target.UpdatedAt}
	msg, saved := env.e.applyPendingUserChanges(env.um, env.sysop, target, map[string]interface{}{"handle": "New|CL"}, orig)
	if saved || !strings.Contains(msg, "pipe") {
		t.Errorf("saved = %v, msg = %q", saved, msg)
	}
	if strings.Contains(string(ansi.ReplacePipeCodes([]byte(msg))), "\x1b[2J") {
		t.Errorf("the message itself ran a code: %q", msg)
	}
	msg, saved = env.e.applyPendingUserChanges(env.um, env.sysop, target, map[string]interface{}{"realname": "New|CL Name"}, orig)
	if saved || !strings.Contains(msg, "pipe") {
		t.Errorf("real name: saved = %v, msg = %q", saved, msg)
	}
}
