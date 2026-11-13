package perm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

const scopedRoles = `{"roles": [
	{"name": "general_mod", "permissions": [{"permission": "messages.moderate", "areas": [" general ", ""]}]},
	{"name": "fsx_mod", "permissions": [{"permission": "messages.moderate", "conferences": ["fsxnet"]}]},
	{"name": "all_mod", "permissions": ["messages.moderate", "messages.moderate"]}
]}`

func moderates(u *user.User, at Scope, roles *Roles) Decision {
	return Check(u, MessagesModerate, at, levels, roles)
}

func TestScopedGrants(t *testing.T) {
	roles := mustParse(t, scopedRoles)
	general := Scope{Area: "GENERAL", Conference: "LOCAL"}
	fsxGen := Scope{Area: "FSX_GEN", Conference: "FSXNET"}
	mail := Scope{Area: "PRIVMAIL", Conference: "LOCAL", SponsorID: 9}
	tests := []struct {
		name  string
		roles []string
		at    Scope
		want  string // Source, "" for not allowed
	}{
		{"area grant in its area", []string{"general_mod"}, general, "role general_mod"},
		{"area grant elsewhere", []string{"general_mod"}, fsxGen, ""},
		{"conference grant in its conference", []string{"fsx_mod"}, fsxGen, "role fsx_mod"},
		{"conference grant elsewhere", []string{"fsx_mod"}, general, ""},
		{"everywhere grant", []string{"all_mod"}, fsxGen, "role all_mod"},
		{"everywhere grant, no area", []string{"all_mod"}, Scope{}, "role all_mod"},
		{"area grant, no area", []string{"general_mod"}, Scope{}, ""},
		{"no role reaches private mail", []string{"all_mod", "general_mod"}, mail, ""},
		{"two roles", []string{"all_mod", "general_mod"}, general, "roles all_mod, general_mod"},
	}
	for _, tt := range tests {
		d := moderates(&user.User{Handle: "Mod", AccessLevel: 20, Roles: tt.roles}, tt.at, roles)
		if d.Source() != tt.want || d.Allowed != (tt.want != "") {
			t.Errorf("%s: %+v (%q), want %q", tt.name, d, d.Source(), tt.want)
		}
	}
	if got := len(roles.List()[2].Permissions); got != 1 {
		t.Errorf("repeated everywhere grant kept %d times", got)
	}
	if got := roles.List()[0].Permissions[0].Areas; strings.Join(got, ",") != "GENERAL" {
		t.Errorf("area tags kept as %v", got)
	}
}

func TestSponsorAndLevelsHoldModeration(t *testing.T) {
	general := Scope{Area: "GENERAL", SponsorID: 7}
	tests := []struct {
		name string
		u    *user.User
		at   Scope
		want string
	}{
		{"the sponsor's account", &user.User{ID: 7, Handle: "TechGuru", AccessLevel: 20}, general, "sponsor of GENERAL"},
		{"someone else", &user.User{ID: 8, Handle: "Other", AccessLevel: 20}, general, ""},
		{"another account with the sponsor's handle", &user.User{ID: 8, Handle: "TechGuru", AccessLevel: 20}, general, ""},
		{"sponsor field on private mail", &user.User{ID: 7, Handle: "TechGuru", AccessLevel: 20}, Scope{Area: "privmail", SponsorID: 7}, ""},
		{"co-sysop everywhere", &user.User{AccessLevel: 250}, Scope{Area: "PRIVMAIL"}, "co-sysop level"},
		{"sysop", &user.User{AccessLevel: 255}, general, "sysop level"},
		{"deleted sponsor", &user.User{ID: 7, Handle: "TechGuru", AccessLevel: 20, DeletedUser: true}, general, ""},
	}
	for _, tt := range tests {
		d := moderates(tt.u, tt.at, &Roles{})
		if d.Source() != tt.want {
			t.Errorf("%s: %+v (%q), want %q", tt.name, d, d.Source(), tt.want)
		}
	}
	// A co-sysop's level holds moderation but not users.validate.
	if Check(&user.User{AccessLevel: 250}, UsersValidate, Scope{}, levels, &Roles{}).Allowed {
		t.Error("co-sysop level holds users.validate")
	}
	// An unset coSysOpLevel is not everyone.
	if moderates(&user.User{AccessLevel: 1}, general, &Roles{}).Allowed {
		t.Fatal("level 1 moderates")
	}
	if Check(&user.User{AccessLevel: 1}, MessagesModerate, general, Levels{SysOp: 255}, &Roles{}).Allowed {
		t.Error("coSysOpLevel 0 let level 1 moderate")
	}
	d := moderates(&user.User{ID: 7, Handle: "TechGuru", AccessLevel: 20, Roles: []string{"all_mod"}}, general, mustParse(t, scopedRoles))
	if d.Source() != "sponsor of GENERAL and role all_mod" {
		t.Errorf("sponsor and role: %q", d.Source())
	}
}

func TestGrantDiagnostics(t *testing.T) {
	_, diags, err := Parse([]byte(`{"roles": [
		{"name": "a", "permissions": [{"permission": "users.validate", "areas": ["GENERAL"]}]},
		{"name": "b", "permissions": [{"permission": "messages.moderate", "areas": [" "]}]},
		{"name": "c", "permissions": [{"permission": "messages.moderate", "area": ["GENERAL"]}, "users.validate"]},
		{"name": "d", "permissions": [{"permission": "messages.moderate", "areas": []}, {"permission": "messages.moderate", "conferences": null}, "users.validate"]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`role 1 ("a"): users.validate can't be limited to areas or conferences, so this grant is ignored`,
		`role 1 ("a"): grants no permissions`,
		`role 2 ("b"): messages.moderate lists only blank areas and conferences, so this grant is ignored`,
		`role 2 ("b"): grants no permissions`,
		`role 3 ("c"): a grant of "messages.moderate" has "area", which isn't one of permission, areas and conferences, so the grant is ignored`,
		`role 4 ("d"): messages.moderate names areas or conferences but lists none, so this grant is ignored`,
		`role 4 ("d"): messages.moderate names areas or conferences but lists none, so this grant is ignored`,
	}
	if len(diags) != len(want) {
		t.Fatalf("diagnostics = %v", diags)
	}
	for i, w := range want {
		if !strings.HasPrefix(diags[i].String(), w) {
			t.Errorf("diagnostic %d = %q, want prefix %q", i, diags[i], w)
		}
	}
	// An emptied list doesn't leave a grant that moderates everywhere.
	r, _, _ := Parse([]byte(`{"roles": [{"name": "e", "permissions": [{"permission": "messages.moderate", "areas": []}]}]}`))
	if moderates(&user.User{AccessLevel: 20, Roles: []string{"e"}}, Scope{Area: "OTHER"}, r).Allowed {
		t.Error(`a grant with "areas": [] moderates`)
	}
	// "area" for "areas" doesn't leave a grant that moderates everywhere.
	r, _, _ = Parse([]byte(`{"roles": [{"name": "c", "permissions": [{"permission": "messages.moderate", "area": ["GENERAL"]}]}]}`))
	if moderates(&user.User{AccessLevel: 20, Roles: []string{"c"}}, Scope{Area: "OTHER"}, r).Allowed {
		t.Error("a grant with a misspelt key moderates")
	}
	if _, _, err := Parse([]byte(`{"roles": [{"name": "c", "permissions": [42]}]}`)); err == nil {
		t.Error("a grant that is neither a name nor an object parsed")
	}
}

func TestGrantJSON(t *testing.T) {
	for _, tt := range []struct {
		g    Grant
		json string
		str  string
	}{
		{Grant{Permission: "users.validate"}, `"users.validate"`, "users.validate"},
		{Grant{Permission: "messages.moderate", Areas: []string{"GENERAL"}, Conferences: []string{"FSXNET"}},
			`{"permission":"messages.moderate","areas":["GENERAL"],"conferences":["FSXNET"]}`,
			"messages.moderate in areas GENERAL and conferences FSXNET"},
	} {
		b, err := json.Marshal(tt.g)
		if err != nil || string(b) != tt.json {
			t.Errorf("Marshal = %s, %v; want %s", b, err, tt.json)
		}
		var back Grant
		if err := json.Unmarshal(b, &back); err != nil || back.String() != tt.str {
			t.Errorf("round trip = %+v, %v", back, err)
		}
	}
}

// files.manage is scoped to file areas: a grant names file area and
// conference tags, a co-sysop holds it everywhere, and a file area doesn't
// count as private mail whatever it is called.
func TestFilesManage(t *testing.T) {
	roles := mustParse(t, `{"roles": [
		{"name": "utils", "permissions": [{"permission": "files.manage", "areas": ["UTILS"]}]},
		{"name": "fsx_files", "permissions": [{"permission": "files.manage", "conferences": ["FSXNET"]}]}
	]}`)
	if !IsScoped(FilesManage) || KindOf(FilesManage) != FileAreas || KindOf(MessagesModerate) != MessageAreas {
		t.Fatal("files.manage isn't a file-area permission")
	}
	member := func(r string) *user.User { return &user.User{ID: 5, AccessLevel: 20, Roles: []string{r}} }
	for _, tt := range []struct {
		name string
		u    *user.User
		at   Scope
		want string
	}{
		{"by area", member("utils"), Scope{Area: "UTILS"}, "role utils"},
		{"another area", member("utils"), Scope{Area: "GAMES"}, ""},
		{"by conference", member("fsx_files"), Scope{Area: "FSX_NODELIST", Conference: "FSXNET"}, "role fsx_files"},
		{"co-sysop", &user.User{AccessLevel: 250}, Scope{Area: "GAMES"}, "co-sysop level"},
		{"a file area called PRIVMAIL", member("utils"), Scope{Area: "PRIVMAIL"}, ""},
		{"no area", member("utils"), Scope{}, ""},
	} {
		d := Check(tt.u, FilesManage, tt.at, levels, roles)
		if d.Source() != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, d.Source(), tt.want)
		}
	}
	// A message-area moderation grant gives nothing over files.
	mod := mustParse(t, `{"roles": [{"name": "m", "permissions": [{"permission": "messages.moderate", "areas": ["UTILS"]}]}]}`)
	if Check(member("m"), FilesManage, Scope{Area: "UTILS"}, levels, mod).Allowed {
		t.Error("messages.moderate granted files.manage")
	}
}
