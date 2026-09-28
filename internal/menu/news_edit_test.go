package menu

import (
	"testing"
	"time"
)

// EDITNEWS is for CoSysOps only.
func TestEditNewsRequiresCoSysOp(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))
	r := env.runCmd("EDITNEWS", env.caller, "", "D\r1\rY\r")
	if !r.has("CoSysOp access required.") || r.has("System News Management") {
		t.Errorf("caller:\n%s", r.text())
	}
	if n := len(loadEnvNews(t, env).Items); n != 4 {
		t.Errorf("items = %d, want 4", n)
	}
	if r := env.runCmd("EDITNEWS", nil, "", "Q\r"); !r.has("CoSysOp access required.") {
		t.Errorf("logged out:\n%s", r.text())
	}
}

// Adding an item prepends it with a fresh ID, the entered levels and flag,
// the sysop as default author, and a multi-line body.
func TestEditNewsAddItem(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, &NewsData{NextID: 10, Items: []NewsItem{{ID: 9, Title: "Older", Body: "x"}}})

	before := time.Now().Add(-time.Second)
	input := "a\r" +
		"  Brand New Item Title That Is Long  \r" + // capped at 28
		"20\r" + "90\r" + "y\r" + "\r" + // levels, always, default author
		"line one\r" + "line two\r" + "\r" + // body
		"Q\r"
	r := env.runCmd("EDITNEWS", env.sysop, "", input)
	if !r.has("Adding News Item", "News item added!", "(2 items)") {
		t.Errorf("add flow:\n%s", r.text())
	}

	nd := loadEnvNews(t, env)
	if len(nd.Items) != 2 {
		t.Fatalf("items = %+v", nd.Items)
	}
	got := nd.Items[0]
	if got.ID != 10 || nd.NextID != 11 {
		t.Errorf("ID=%d NextID=%d, want 10 and 11", got.ID, nd.NextID)
	}
	if got.Title != "Brand New Item Title That Is" || got.From != "Sysop" || got.Level != 20 ||
		got.MaxLevel != 90 || !got.Always || got.Body != "line one\nline two" {
		t.Errorf("added item = %+v", got)
	}
	if got.When.Before(before) {
		t.Errorf("When = %v, want now", got.When)
	}
	if nd.Items[1].ID != 9 {
		t.Errorf("older item moved: %+v", nd.Items[1])
	}
}

// An add with no title, or with no body, creates nothing.
func TestEditNewsAddAbandoned(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("EDITNEWS", env.sysop, "", "A\r\rA\rTitle\r\r\r\rSomeone\r\rQ\r")
	if !r.has("No body entered, item not created.") || r.has("News item added!") {
		t.Errorf("abandoned adds:\n%s", r.text())
	}
	if n := len(loadEnvNews(t, env).Items); n != 0 {
		t.Errorf("items = %d, want 0", n)
	}
}

// Deleting asks for confirmation and removes only the chosen item.
func TestEditNewsDeleteItem(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))

	input := "D\r9\r" + // out of range
		"D\r2\rN\r" + // declined
		"D\r2\rY\r" + // deleted
		"Q\r"
	r := env.runCmd("EDITNEWS", env.sysop, "", input)
	if !r.has("Invalid selection.", "Delete #2 (Always Item)?", "Item deleted.") {
		t.Errorf("delete flow:\n%s", r.text())
	}
	nd := loadEnvNews(t, env)
	var ids []int
	for _, it := range nd.Items {
		ids = append(ids, it.ID)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 3 || ids[2] != 4 {
		t.Errorf("IDs left = %v, want [1 3 4]", ids)
	}
}

// Editing changes each field in place and saves on Q; unparseable levels
// and blank values leave the field alone.
func TestEditNewsEditItem(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))

	input := "E\r0\r" + // invalid pick
		"E\r1\r" +
		"T\rRetitled\r" + "T\r\r" + // blank title keeps it
		"F\rNewAuthor\r" +
		"L\rlots\r" + "L\r15\r" + // bad then good min level
		"X\r50\r" +
		"A\ry\r" +
		"E\rnew body\r\r" +
		"Z\r" + // unknown key ignored
		"Q\r" + "Q\r"
	r := env.runCmd("EDITNEWS", env.sysop, "", input)
	if !r.has("Invalid selection.", "News #1", "Item saved.") {
		t.Errorf("edit flow:\n%s", r.text())
	}
	got := loadEnvNews(t, env).Items[0]
	want := NewsItem{ID: 1, Title: "Retitled", From: "NewAuthor", When: got.When, Level: 15, MaxLevel: 50, Always: true, Body: "new body"}
	if got != want {
		t.Errorf("edited item:\n got %+v\nwant %+v", got, want)
	}
}

// A disconnect while editing discards the changes.
func TestEditNewsEditDisconnectDiscards(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))
	env.runCmd("EDITNEWS", env.sysop, "", "E\r1\rT\rHalf done\r")
	if got := loadEnvNews(t, env).Items[0].Title; got != "Once Item" {
		t.Errorf("title = %q, want unchanged", got)
	}
}

// List, View (one and all) and the bare item number all display items
// without changing them.
func TestEditNewsListAndView(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))

	input := "L\r\r" + // list + pause
		"V\r3\r\r" + // view one
		"V\r0\r\r\r\r\r" + // view all four
		"V\rx\r" + // non-number returns to menu
		"2\r\r" + // bare number views
		"Q\r"
	r := env.runCmd("EDITNEWS", env.sysop, "", input)
	if !r.has("Title", "Always", "Once", "All", "staff body text", "newbie body text", "always body text") {
		t.Errorf("list/view:\n%s", r.text())
	}
	if n := len(loadEnvNews(t, env).Items); n != 4 {
		t.Errorf("items = %d, want 4", n)
	}
}

// With no news, Delete/Edit/View say there is nothing to act on.
func TestEditNewsEmpty(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("EDITNEWS", env.sysop, "", "D\rE\rV\rQ\r")
	if !r.has("(0 items)", "No news items to delete.", "No news items to edit.", "No news items.") {
		t.Errorf("empty:\n%s", r.text())
	}
}

// Opening the editor repairs duplicate and missing IDs on disk.
func TestEditNewsNormalizesIDs(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, &NewsData{Items: []NewsItem{
		{ID: 3, Title: "A", Body: "a"},
		{ID: 3, Title: "B", Body: "b"},
		{Title: "C", Body: "c"},
	}})
	env.runCmd("EDITNEWS", env.sysop, "", "Q\r")

	nd := loadEnvNews(t, env)
	seen := map[int]bool{}
	for _, it := range nd.Items {
		if it.ID <= 0 || seen[it.ID] {
			t.Fatalf("IDs not repaired: %+v", nd.Items)
		}
		seen[it.ID] = true
	}
	if nd.Items[0].ID != 3 {
		t.Errorf("first item's ID changed to %d; existing IDs must be kept", nd.Items[0].ID)
	}
}
