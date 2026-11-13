package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sponsorFinding struct {
	name, message string
	status        doctorSeverity
}

func writeOwnersFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSponsors(t *testing.T) {
	root := t.TempDir()
	writeOwnersFile(t, root, "configs/message_areas.json", `[
		{"id": 1, "tag": "OK", "sponsor": "Alice", "sponsor_id": 2},
		{"id": 2, "tag": "UNBOUND", "sponsor": "Alice"},
		{"id": 3, "tag": "NOBODY", "sponsor": "Ghost", "sponsor_id": -1},
		{"id": 4, "tag": "GONE", "sponsor": "Bob", "sponsor_id": 9},
		{"id": 5, "tag": "RENAMED", "sponsor": "Carol", "sponsor_id": 3}
	]`)
	writeOwnersFile(t, root, "data/users/users.json", `[
		{"id": 2, "handle": "Alice"},
		{"id": 3, "handle": "Caroline"}
	]`)
	var got []sponsorFinding
	checkSponsors(filepath.Join(root, "configs"), root, func(name string, status doctorSeverity, message, _ string) {
		got = append(got, sponsorFinding{name, message, status})
	})
	var msgs []string
	for _, c := range got {
		if c.status != doctorWarn || doctorSection(c.name) != "NETWORK & MESSAGING" {
			t.Errorf("check %+v", c)
		}
		msgs = append(msgs, c.message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		`area NOBODY: sponsor "Ghost" matched no account`,
		`area GONE: sponsor "Bob" was account #9, which no longer exists`,
		`area RENAMED: sponsor is account #3, now "Caroline", but the area names "Carol"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("checks lack %q:\n%s", want, joined)
		}
	}
	if len(got) != 3 {
		t.Errorf("%d findings, want 3:\n%s", len(got), joined)
	}
}

func TestCheckUserIDMark(t *testing.T) {
	for _, tc := range []struct {
		mark string
		want doctorSeverity
	}{
		{"", doctorInfo},
		{"lots", doctorFail},
		{"3", doctorWarn},
		{"9", doctorOK},
	} {
		root := t.TempDir()
		writeOwnersFile(t, root, "data/users/users.json", `[{"id": 1}, {"id": 4}]`)
		if tc.mark != "" {
			writeOwnersFile(t, root, "data/users/next_user_id", tc.mark+"\n")
		}
		var got []sponsorFinding
		checkUserIDMark(root, func(name string, status doctorSeverity, message, _ string) {
			got = append(got, sponsorFinding{name, message, status})
		})
		if len(got) != 1 || got[0].status != tc.want {
			t.Errorf("mark %q: %+v, want %s", tc.mark, got, tc.want)
		}
	}
}

func TestCheckHandleCollisions(t *testing.T) {
	root := t.TempDir()
	writeOwnersFile(t, root, "data/users/users.json", `[{"id": 1, "handle": "Sam"}, {"id": 2, "handle": "ſam"}, {"id": 3, "handle": "Ann"}]`)
	var got []sponsorFinding
	checkHandleCollisions(root, func(name string, status doctorSeverity, message, _ string) {
		got = append(got, sponsorFinding{name, message, status})
	})
	if len(got) != 1 || got[0].status != doctorWarn || !strings.Contains(got[0].message, `"Sam" (#1) and "ſam" (#2)`) {
		t.Errorf("findings = %+v", got)
	}
}
