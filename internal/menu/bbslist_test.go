package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadBBSListDataEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	bld, err := loadBBSListData(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bld.Listings) != 0 {
		t.Errorf("expected 0 listings, got %d", len(bld.Listings))
	}
	if bld.NextID != 1 {
		t.Errorf("expected NextID=1, got %d", bld.NextID)
	}
}

func TestSaveAndLoadBBSListData(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Second)
	bld := &bbsListData{
		NextID: 3,
		Listings: []BBSListing{
			{
				ID:          1,
				Name:        "Test BBS",
				Sysop:       "TestSysOp",
				Address:     "test.bbs.com",
				TelnetPort:  "23",
				SSHPort:     "2222",
				Web:         "https://test.bbs.com",
				Software:    "ViSiON/3",
				Description: "A test BBS",
				AddedBy:     "user1",
				AddedDate:   now,
				Verified:    false,
			},
			{
				ID:         2,
				Name:       "Another BBS",
				Sysop:      "OtherOp",
				Address:    "other.bbs.com",
				TelnetPort: "2323",
				Software:   "Mystic",
				AddedBy:    "user2",
				AddedDate:  now,
				Verified:   true,
			},
		},
	}

	if err := saveBBSListData(dataDir, bld); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	fp := bbsListFilePath(dataDir)
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Fatal("bbslist.json was not created")
	}

	loaded, err := loadBBSListData(dataDir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.NextID != 3 {
		t.Errorf("NextID: got %d, want 3", loaded.NextID)
	}
	if len(loaded.Listings) != 2 {
		t.Fatalf("listings count: got %d, want 2", len(loaded.Listings))
	}

	e := loaded.Listings[0]
	if e.Name != "Test BBS" {
		t.Errorf("Name: got %q, want %q", e.Name, "Test BBS")
	}
	if e.Address != "test.bbs.com" {
		t.Errorf("Address: got %q, want %q", e.Address, "test.bbs.com")
	}
	if e.TelnetPort != "23" {
		t.Errorf("TelnetPort: got %q, want %q", e.TelnetPort, "23")
	}
	if e.SSHPort != "2222" {
		t.Errorf("SSHPort: got %q, want %q", e.SSHPort, "2222")
	}
	if e.Web != "https://test.bbs.com" {
		t.Errorf("Web: got %q, want %q", e.Web, "https://test.bbs.com")
	}
	if e.Verified {
		t.Error("first entry should not be verified")
	}

	e2 := loaded.Listings[1]
	if !e2.Verified {
		t.Error("second entry should be verified")
	}
	if e2.SSHPort != "" {
		t.Errorf("second entry SSHPort should be empty, got %q", e2.SSHPort)
	}
}

func TestBBSListDataJSON(t *testing.T) {
	bld := &bbsListData{
		NextID: 2,
		Listings: []BBSListing{
			{
				ID:         1,
				Name:       "JSON Test",
				Address:    "json.bbs.com",
				TelnetPort: "23",
				Software:   "TestWare",
			},
		},
	}

	data, err := json.MarshalIndent(bld, "", "    ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var loaded bbsListData
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if loaded.NextID != 2 {
		t.Errorf("NextID: got %d, want 2", loaded.NextID)
	}
	if len(loaded.Listings) != 1 {
		t.Fatalf("listings: got %d, want 1", len(loaded.Listings))
	}
	if loaded.Listings[0].Address != "json.bbs.com" {
		t.Errorf("Address: got %q, want %q", loaded.Listings[0].Address, "json.bbs.com")
	}
	if loaded.Listings[0].TelnetPort != "23" {
		t.Errorf("TelnetPort: got %q, want %q", loaded.Listings[0].TelnetPort, "23")
	}
}

func TestBBSListNextIDStartsAt1(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// With no listings, NextID should default to 1.
	badData := []byte(`{"listings":[],"next_id":0}`)
	fp := filepath.Join(dataDir, "bbslist.json")
	if err := os.WriteFile(fp, badData, 0644); err != nil {
		t.Fatal(err)
	}

	bld, err := loadBBSListData(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if bld.NextID != 1 {
		t.Errorf("NextID should default to 1, got %d", bld.NextID)
	}
}

func TestBBSListNextIDFromMaxExisting(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// With existing listings and next_id=0, NextID should be max(ID)+1.
	data := []byte(`{"listings":[{"id":5,"name":"A"},{"id":3,"name":"B"},{"id":10,"name":"C"}],"next_id":0}`)
	fp := filepath.Join(dataDir, "bbslist.json")
	if err := os.WriteFile(fp, data, 0644); err != nil {
		t.Fatal(err)
	}

	bld, err := loadBBSListData(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if bld.NextID != 11 {
		t.Errorf("NextID should be 11 (max existing ID 10 + 1), got %d", bld.NextID)
	}
}

func TestBBSListDeleteCompacts(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	bld := &bbsListData{
		NextID: 4,
		Listings: []BBSListing{
			{ID: 1, Name: "First", AddedBy: "user1"},
			{ID: 2, Name: "Second", AddedBy: "user2"},
			{ID: 3, Name: "Third", AddedBy: "user3"},
		},
	}

	if err := saveBBSListData(dataDir, bld); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Delete the middle entry (index 1) — same operation as runBBSListDelete
	idx := 1
	bld.Listings = append(bld.Listings[:idx], bld.Listings[idx+1:]...)

	if err := saveBBSListData(dataDir, bld); err != nil {
		t.Fatalf("save after delete failed: %v", err)
	}

	// Reload and verify compaction
	loaded, err := loadBBSListData(dataDir)
	if err != nil {
		t.Fatalf("load after delete failed: %v", err)
	}

	if len(loaded.Listings) != 2 {
		t.Fatalf("expected 2 listings after delete, got %d", len(loaded.Listings))
	}
	if loaded.Listings[0].Name != "First" {
		t.Errorf("listings[0]: got %q, want %q", loaded.Listings[0].Name, "First")
	}
	if loaded.Listings[0].ID != 1 {
		t.Errorf("listings[0] ID: got %d, want 1", loaded.Listings[0].ID)
	}
	if loaded.Listings[1].Name != "Third" {
		t.Errorf("listings[1]: got %q, want %q", loaded.Listings[1].Name, "Third")
	}
	if loaded.Listings[1].ID != 3 {
		t.Errorf("listings[1] ID: got %d, want 3", loaded.Listings[1].ID)
	}
	if loaded.NextID != 4 {
		t.Errorf("NextID should be preserved as 4, got %d", loaded.NextID)
	}
}

func TestBBSListSanitize(t *testing.T) {
	tests := []struct {
		input, expect string
	}{
		{"Normal text", "Normal text"},
		{"|15colored|07 text", "\xc2\xa615colored\xc2\xa607 text"},
		{"no pipes here", "no pipes here"},
		{"", ""},
		{"one|pipe", "one\xc2\xa6pipe"},
	}
	for _, tt := range tests {
		got := bbsListSanitize(tt.input)
		if got != tt.expect {
			t.Errorf("bbsListSanitize(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestBBSListConnectionSummary(t *testing.T) {
	tests := []struct {
		name   string
		entry  BBSListing
		expect string
	}{
		{"both ports", BBSListing{TelnetPort: "23", SSHPort: "22", Web: "https://bbs.com"}, "T:23 S:22 Web"},
		{"telnet only", BBSListing{TelnetPort: "23"}, "T:23"},
		{"ssh only", BBSListing{SSHPort: "2222"}, "S:2222"},
		{"web only", BBSListing{Web: "https://bbs.com"}, "Web"},
		{"telnet and ssh", BBSListing{TelnetPort: "23", SSHPort: "22"}, "T:23 S:22"},
		{"empty", BBSListing{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bbsListConnectionSummary(&tt.entry)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

// seedBBSList writes three listings to env's bbslist.json: one added by the
// sysop, one by the caller, and one by someone else.
func seedBBSList(t *testing.T, env *menuEnv) {
	t.Helper()
	bld := &bbsListData{NextID: 4, Listings: []BBSListing{
		{ID: 1, Name: "Sysop Board", Sysop: "Sam", Address: "sysop.example", TelnetPort: "23", Software: "ViSiON/3", AddedBy: "Sysop"},
		{ID: 2, Name: "Caller Board", Sysop: "Carl", Address: "caller.example", SSHPort: "2222", Web: "https://caller.example",
			Software: "Mystic", Description: "Door games galore", AddedBy: "caller"},
		{ID: 3, Name: "Other Board", Address: "other.example", AddedBy: "Someone"},
	}}
	if err := saveBBSListData(env.dataDir(), bld); err != nil {
		t.Fatalf("seed bbslist: %v", err)
	}
}

// loadEnvBBSList reloads bbslist.json the way the handlers do.
func loadEnvBBSList(t *testing.T, env *menuEnv) *bbsListData {
	t.Helper()
	bld, err := loadBBSListData(env.dataDir())
	if err != nil {
		t.Fatalf("reload bbslist: %v", err)
	}
	return bld
}

// BBSLISTADD stores a complete entry owned by the caller, trims and caps
// every field, defaults the software, and advances NextID.
func TestBBSListAddStoresEntry(t *testing.T) {
	env := newMenuEnv(t)
	longName := strings.Repeat("N", 45)
	input := longName + "\r" + // name, capped at 40
		"  bbs.example.org  \r" + // address, trimmed
		"23\r" + // telnet
		"12345678901234\r" + // ssh, capped at 10
		"https://bbs.example.org\r" + // web
		"Sam\r" + // sysop
		"\r" + // software -> ViSiON/3
		"Best board around\r" // description
	r := env.runCmd("BBSLISTADD", env.caller, "", input)
	if r.err != nil || !r.has("Your entry has been added!") {
		t.Fatalf("err=%v output:\n%s", r.err, r.text())
	}

	bld := loadEnvBBSList(t, env)
	if len(bld.Listings) != 1 || bld.NextID != 2 {
		t.Fatalf("listings=%d NextID=%d, want 1 and 2", len(bld.Listings), bld.NextID)
	}
	got := bld.Listings[0]
	want := BBSListing{ID: 1, Name: longName[:40], Sysop: "Sam", Address: "bbs.example.org", TelnetPort: "23",
		SSHPort: "1234567890", Web: "https://bbs.example.org", Software: "ViSiON/3", Description: "Best board around", AddedBy: "Caller"}
	got.AddedDate = time.Time{}
	if got != want {
		t.Errorf("stored entry:\n got %+v\nwant %+v", got, want)
	}
	if bld.Listings[0].AddedDate.IsZero() || bld.Listings[0].Verified {
		t.Errorf("AddedDate=%v Verified=%v, want stamped and unverified", bld.Listings[0].AddedDate, bld.Listings[0].Verified)
	}
}

// A blank name or address aborts the add without writing anything.
func TestBBSListAddRequiresNameAndAddress(t *testing.T) {
	for name, input := range map[string]string{
		"blank name":    "\r",
		"blank address": "My BBS\r   \r",
	} {
		t.Run(name, func(t *testing.T) {
			env := newMenuEnv(t)
			r := env.runCmd("BBSLISTADD", env.caller, "", input)
			if !r.has("Aborted.") || r.has("added") {
				t.Errorf("output:\n%s", r.text())
			}
			if _, err := os.Stat(bbsListFilePath(env.dataDir())); !os.IsNotExist(err) {
				t.Errorf("bbslist.json written on abort (stat err %v)", err)
			}
		})
	}
}

// BBSLISTEDIT lets an owner change fields (matching the handle
// case-insensitively), ignores blank values and bad field numbers, and saves
// on Q.
func TestBBSListEditOwnEntry(t *testing.T) {
	env := newMenuEnv(t)
	seedBBSList(t, env)

	input := "?\r2\r" + // list, then pick entry 2
		"9\r" + // invalid field
		"1\rRenamed Board\r" +
		"2\r\r" + // blank value keeps the address
		"3\r2323\r4\r22\r5\rhttps://new.example\r6\rCarla\r7\rSynchronet\r8\rNow with more doors\r" +
		"Q\r"
	r := env.runCmd("BBSLISTEDIT", env.caller, "", input)
	if !r.has("1. Sysop Board (sysop.example)", "Editing: Caller Board", "Invalid choice.", "Entry updated!") {
		t.Errorf("edit flow:\n%s", r.text())
	}

	got := loadEnvBBSList(t, env).Listings[1]
	if got.ID != 2 || got.Name != "Renamed Board" || got.Address != "caller.example" || got.TelnetPort != "2323" ||
		got.SSHPort != "22" || got.Web != "https://new.example" || got.Sysop != "Carla" ||
		got.Software != "Synchronet" || got.Description != "Now with more doors" || got.AddedBy != "caller" {
		t.Errorf("edited entry = %+v", got)
	}
}

// A caller may not edit someone else's listing; the sysop may; bad
// selections and an empty directory are reported.
func TestBBSListEditAccessAndValidation(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("BBSLISTEDIT", env.caller, "", "1\r"); !r.has("No BBS listings to edit.") {
		t.Errorf("empty directory:\n%s", r.text())
	}

	seedBBSList(t, env)
	r := env.runCmd("BBSLISTEDIT", env.caller, "", "1\r1\rHijacked\rQ\r")
	if !r.has("You can only edit your own listings.") {
		t.Errorf("non-owner edit:\n%s", r.text())
	}
	for _, in := range []string{"0\r", "4\r", "x\r"} {
		if r := env.runCmd("BBSLISTEDIT", env.caller, "", in); !r.has("Invalid selection.") {
			t.Errorf("selection %q:\n%s", in, r.text())
		}
	}
	if got := loadEnvBBSList(t, env).Listings[0].Name; got != "Sysop Board" {
		t.Errorf("refused edit changed the name to %q", got)
	}

	r = env.runCmd("BBSLISTEDIT", env.sysop, "", "3\r1\rModerated\r\r")
	if !r.has("Entry updated!") {
		t.Errorf("sysop edit:\n%s", r.text())
	}
	if got := loadEnvBBSList(t, env).Listings[2].Name; got != "Moderated" {
		t.Errorf("sysop edit saved name %q, want Moderated", got)
	}
}

// A disconnect in the middle of editing saves nothing.
func TestBBSListEditDisconnectDiscards(t *testing.T) {
	env := newMenuEnv(t)
	seedBBSList(t, env)
	env.runCmd("BBSLISTEDIT", env.caller, "", "2\r1\rHalf typed\r")
	if got := loadEnvBBSList(t, env).Listings[1].Name; got != "Caller Board" {
		t.Errorf("disconnect saved name %q", got)
	}
}

// BBSLISTDELETE is CoSysOp-only, confirms, and removes the chosen entry.
func TestBBSListDelete(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("BBSLISTDELETE", env.sysop, "", "1\r"); !r.has("No BBS listings to delete.") {
		t.Errorf("empty directory:\n%s", r.text())
	}
	seedBBSList(t, env)

	if r := env.runCmd("BBSLISTDELETE", env.caller, "", "2\rY"); !r.has("CoSysOp access required.") {
		t.Errorf("caller delete:\n%s", r.text())
	}
	if r := env.runCmd("BBSLISTDELETE", env.sysop, "", "2\rN"); !r.has("Cancelled.") {
		t.Errorf("declined delete:\n%s", r.text())
	}
	if r := env.runCmd("BBSLISTDELETE", env.sysop, "", "9\r"); !r.has("Invalid selection.") {
		t.Errorf("out-of-range delete:\n%s", r.text())
	}
	if n := len(loadEnvBBSList(t, env).Listings); n != 3 {
		t.Fatalf("listings = %d after refused deletes, want 3", n)
	}

	r := env.runCmd("BBSLISTDELETE", env.sysop, "", "?\r2\rY")
	if !r.has("3. Other Board (other.example)", "Delete Caller Board?", "Entry deleted.") {
		t.Errorf("delete flow:\n%s", r.text())
	}
	bld := loadEnvBBSList(t, env)
	if len(bld.Listings) != 2 || bld.Listings[0].ID != 1 || bld.Listings[1].ID != 3 || bld.NextID != 4 {
		t.Errorf("after delete: %+v NextID=%d", bld.Listings, bld.NextID)
	}
}

// BBSLISTVERIFY is CoSysOp-only and toggles the verified flag each time.
func TestBBSListVerifyToggles(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("BBSLISTVERIFY", env.sysop, "", "1\r"); !r.has("No BBS listings.") {
		t.Errorf("empty directory:\n%s", r.text())
	}
	seedBBSList(t, env)

	if r := env.runCmd("BBSLISTVERIFY", env.caller, "", "1\r"); !r.has("CoSysOp access required.") {
		t.Errorf("caller verify:\n%s", r.text())
	}
	if r := env.runCmd("BBSLISTVERIFY", env.sysop, "", "7\r"); !r.has("Invalid selection.") {
		t.Errorf("bad selection:\n%s", r.text())
	}
	if loadEnvBBSList(t, env).Listings[0].Verified {
		t.Fatal("refused verify changed the flag")
	}

	if r := env.runCmd("BBSLISTVERIFY", env.sysop, "", "1\r"); !r.has("Sysop Board is now verified.") {
		t.Errorf("first toggle:\n%s", r.text())
	}
	if !loadEnvBBSList(t, env).Listings[0].Verified {
		t.Error("entry 1 not verified on disk")
	}
	if r := env.runCmd("BBSLISTVERIFY", env.sysop, "", "1\r"); !r.has("Sysop Board is now unverified.") {
		t.Errorf("second toggle:\n%s", r.text())
	}
	if loadEnvBBSList(t, env).Listings[0].Verified {
		t.Error("entry 1 still verified on disk")
	}
}

// The BBSLIST browser shows names and the selected entry's details; a caller
// is offered Change only on their own entry and never Del/Verify.
func TestBBSListBrowseAsCaller(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("BBSLIST", env.caller, "", "\r"); !r.has("No BBS listings yet.") {
		t.Errorf("empty directory:\n%s", r.text())
	}
	seedBBSList(t, env)

	// Start on entry 1 (not the caller's), move down to entry 2, quit.
	r := env.runCmd("BBSLIST", env.caller, "", "\x1b[Bq")
	if r.err != nil || r.user != env.caller {
		t.Fatalf("err=%v user=%v", r.err, r.user)
	}
	if !r.has("BBS Listings Directory", "Sysop Board", "Caller Board", "Other Board",
		"sysop.example", "Door games galore", "https://caller.example", "Mystic") {
		t.Errorf("browser output:\n%s", r.text())
	}
	first, second, _ := strings.Cut(r.text(), "Door games galore")
	if strings.Contains(first, "[C]Change") {
		t.Errorf("Change offered on a listing the caller does not own")
	}
	if !strings.Contains(second, "[C]Change") {
		t.Errorf("Change not offered on the caller's own listing")
	}
	if r.has("[D]Del") {
		t.Errorf("caller offered delete")
	}

	// D and V are ignored for a caller.
	env.runCmd("BBSLIST", env.caller, "", "dVq")
	bld := loadEnvBBSList(t, env)
	if len(bld.Listings) != 3 || bld.Listings[0].Verified {
		t.Errorf("caller keys changed the directory: %+v", bld.Listings)
	}
}

// A sysop in the browser can verify, change and delete entries in place,
// each change written straight to disk; navigation keys move the selection.
func TestBBSListBrowseAsSysop(t *testing.T) {
	env := newMenuEnv(t)
	seedBBSList(t, env)

	input := "V" + // verify entry 1
		"\x1b[B" + "C" + "1\rEdited Board\rQ\r" + // change entry 2
		"\x1b[F" + "D" + "N" + // end, decline deleting entry 3
		"\x1b[6~\x1b[5~\x12\x03\x1b[H\x1b[A" + // page down/up, ^R, ^C, home, up (clamped)
		"\x1b[F" + "D" + "Y" + // end, delete entry 3
		"q"
	r := env.runCmd("BBSLIST", env.sysop, "", input)
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("err=%v user=%v", r.err, r.user)
	}
	if !r.has("[D]Del [V]Verify", "Editing: Caller Board", "Entry updated!", "Delete Other Board?", "Edited Board") {
		t.Errorf("browser output:\n%s", r.text())
	}

	bld := loadEnvBBSList(t, env)
	if len(bld.Listings) != 2 {
		t.Fatalf("listings = %+v, want entries 1 and 2", bld.Listings)
	}
	if !bld.Listings[0].Verified || bld.Listings[1].Verified {
		t.Errorf("verified flags = %v/%v, want only entry 1", bld.Listings[0].Verified, bld.Listings[1].Verified)
	}
	if bld.Listings[1].ID != 2 || bld.Listings[1].Name != "Edited Board" || bld.Listings[1].AddedBy != "caller" {
		t.Errorf("changed entry = %+v", bld.Listings[1])
	}
}

// Deleting the last listing from the browser leaves it, since there is
// nothing left to show.
func TestBBSListBrowseDeleteLastEntryExits(t *testing.T) {
	env := newMenuEnv(t)
	if err := saveBBSListData(env.dataDir(), &bbsListData{NextID: 2, Listings: []BBSListing{{ID: 1, Name: "Lonely", Address: "x"}}}); err != nil {
		t.Fatal(err)
	}
	// Anything after the confirmation would be read only if the browser stayed.
	r := env.runCmd("BBSLIST", env.sysop, "", "DY")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("err=%v user=%v, want a clean exit", r.err, r.user)
	}
	if n := len(loadEnvBBSList(t, env).Listings); n != 0 {
		t.Errorf("listings = %d, want 0", n)
	}
}

// A disconnect in the browser logs the caller off.
func TestBBSListBrowseDisconnect(t *testing.T) {
	env := newMenuEnv(t)
	seedBBSList(t, env)
	r := env.runCmd("BBSLIST", env.caller, "", "\x1b[B")
	if r.next != "LOGOFF" || r.user != nil {
		t.Errorf("next=%q user=%v, want LOGOFF/nil", r.next, r.user)
	}
}
