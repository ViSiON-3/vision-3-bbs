package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// topTenUser is an active user with posts, calls and bytes uploaded.
func topTenUser(id int, handle string, posts, calls int, up int64) *user.User {
	return &user.User{ID: id, Handle: handle, AccessLevel: 10, Validated: true,
		MessagesPosted: posts, TimesCalled: calls, UploadedBytes: up}
}

func TestTopTenListFor(t *testing.T) {
	for args, want := range map[string]string{
		"": "posts", " Calls ": "calls", "UPLOADED": "uploaded", "downloads": "downloads",
	} {
		if l, ok := topTenListFor(args); !ok || l.arg != want {
			t.Errorf("topTenListFor(%q) = %s, %v; want %s", args, l.arg, ok, want)
		}
	}
	if l, ok := topTenListFor("karma"); ok || l.arg != "posts" {
		t.Errorf("unknown list = %s, %v; want posts, false", l.arg, ok)
	}
}

// Users are ranked by the list's value, most first, equal values by user
// number. Deleted, banned and unvalidated accounts, and users with none,
// are left out, and the list stops at n.
func TestTopTenRank(t *testing.T) {
	users := []*user.User{
		topTenUser(5, "Five", 7, 0, 0),
		topTenUser(3, "Three", 9, 0, 0),
		topTenUser(4, "Four", 7, 0, 0),
		topTenUser(6, "None", 0, 0, 0),
		{ID: 7, Handle: "Deleted", AccessLevel: 10, Validated: true, DeletedUser: true, MessagesPosted: 99},
		{ID: 8, Handle: "Pending", AccessLevel: 10, MessagesPosted: 99},
		{ID: 9, Handle: "Banned", Validated: true, MessagesPosted: 99},
		nil,
		topTenUser(10, "Two", 1, 0, 0),
	}
	posts, _ := topTenListFor("posts")
	var got []string
	for _, r := range topTenRank(users, posts, 3) {
		got = append(got, r.user.Handle+":"+topTenValue(posts, r.value))
		if r.rank != len(got) {
			t.Errorf("%s ranked %d, want %d", r.user.Handle, r.rank, len(got))
		}
	}
	if want := "Three:9 Four:7 Five:7"; strings.Join(got, " ") != want {
		t.Errorf("ranking = %v, want %s", got, want)
	}
	if n := len(topTenRank(users, posts, 10)); n != 4 {
		t.Errorf("%d listed, want 4", n)
	}
}

func TestTopTenTokens(t *testing.T) {
	up, _ := topTenListFor("uploaded")
	row := &topTenEntry{rank: 2, user: &user.User{Handle: "Zed|CL", GroupLocation: "Somewhere"}, value: 3 << 20}
	got := renderTopTenTokens("@TITLE@|@RK:3@|@UN:8@|@GL:4@|@VA:-6@|@XX@", "Best", up, row)
	if want := "Best|  2|Zed||CL  |Some|  3.0M|@XX@"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
	if got := renderTopTenTokens("@TITLE@ @RK@", "Best", up, nil); got != "Best @RK@" {
		t.Errorf("top = %q", got)
	}
}

// RUN:TOPTEN draws the list in the stock templates; an argument picks it.
func TestRunTopTen(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(topTenUser(3, "Poster", 42, 5, 0), topTenUser(4, "Uploader", 1, 9, 5<<20))

	r := env.runCmd("TOPTEN", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Top Ten Posters", "Poster", "42", "Uploader") || r.has("@TITLE@", "@UN") {
		t.Errorf("output:\n%s", r.text())
	}
	if strings.Index(r.text(), "Poster ") > strings.Index(r.text(), "Uploader") {
		t.Errorf("Poster isn't first:\n%s", r.text())
	}

	r = env.runCmd("TOPTEN", env.caller, "uploaded", "\r")
	if !r.has("Top Ten Uploaders by Size", "Uploader", "5.0M") || r.has("Poster ") {
		t.Errorf("uploaded:\n%s", r.text())
	}
}

// A list nobody is on yet says so.
func TestRunTopTenNobody(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("TOPTEN", env.caller, "downloaded", "\r")
	if !r.has("Top Ten Downloaders by Size", "Nobody is on this list yet.") {
		t.Errorf("output:\n%s", r.text())
	}
}

func TestRunTopTenRequiresLogin(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("TOPTEN", nil, "", "\r"); r.raw != "" || r.err != nil {
		t.Errorf("err = %v output: %q", r.err, r.text())
	}
}
