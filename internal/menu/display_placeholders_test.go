package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// defaults mirrors the logged-out seed values displayPrompt builds before
// applyUserPlaceholders runs.
func placeholderDefaults() map[string]string {
	return map[string]string{
		"|UH":     "Guest",
		"|ALIAS":  "Guest",
		"|HANDLE": "Guest",
		"|LEVEL":  "0",
		"|NAME":   "Guest User",
		"|GL":     "",
		"|UN":     "",
		"|UPLDS":  "0",
		"|DNLDS":  "0",
		"|POSTS":  "0",
		"|CALLS":  "0",
		"|LCALL":  "Never",
	}
}

func TestApplyUserPlaceholders(t *testing.T) {
	prev := time.Date(2026, 3, 14, 21, 30, 0, 0, time.UTC)
	u := &user.User{
		Handle:         "Felonius",
		RealName:       "Robbie",
		AccessLevel:    255,
		GroupLocation:  "Dallas",
		PrivateNote:    "sysop",
		NumUploads:     12,
		NumDownloads:   34,
		MessagesPosted: 56,
		TimesCalled:    78,
		LastLogin:      time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC), // this session
		PreviousLogin:  prev,
	}

	got := placeholderDefaults()
	applyUserPlaceholders(got, u)

	want := map[string]string{
		"|UH":     "Felonius",
		"|ALIAS":  "Felonius",
		"|HANDLE": "Felonius",
		"|LEVEL":  "255",
		"|NAME":   "Robbie",
		"|GL":     "Dallas",
		"|UN":     "sysop",
		"|UPLDS":  "12",
		"|DNLDS":  "34",
		"|POSTS":  "56",
		"|CALLS":  "78",
		// The previous visit, not this session's login stamp.
		"|LCALL": "03/14/26",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestApplyUserPlaceholdersFirstCallShowsNever(t *testing.T) {
	// A first-time caller: LastLogin is already stamped with this session, but
	// there is no previous visit to report.
	u := &user.User{Handle: "Newbie", LastLogin: time.Now()}

	got := placeholderDefaults()
	applyUserPlaceholders(got, u)

	if got["|LCALL"] != "Never" {
		t.Errorf("|LCALL = %q, want %q for a first-time caller", got["|LCALL"], "Never")
	}
}

func TestApplyUserPlaceholdersNilUserKeepsDefaults(t *testing.T) {
	got := placeholderDefaults()
	applyUserPlaceholders(got, nil)
	if got["|UH"] != "Guest" || got["|LCALL"] != "Never" {
		t.Errorf("nil user mutated defaults: %v", got)
	}
}
