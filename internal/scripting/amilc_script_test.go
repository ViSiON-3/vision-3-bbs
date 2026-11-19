package scripting

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// screen replays cursor moves and text from a script's output onto a
// fixed grid, enough to check where amilc.js puts each column.
func screen(out string, w, h int) []string {
	grid := make([][]rune, h)
	for i := range grid {
		grid[i] = []rune(strings.Repeat(" ", w))
	}
	csi := regexp.MustCompile(`^\x1b\[([0-9;]*)([A-Za-z])`)
	row, col := 0, 0
	for len(out) > 0 {
		if m := csi.FindStringSubmatch(out); m != nil {
			out = out[len(m[0]):]
			switch m[2] {
			case "H", "f":
				parts := strings.Split(m[1], ";")
				row, col = 0, 0
				if len(parts) == 2 {
					r, _ := strconv.Atoi(parts[0])
					c, _ := strconv.Atoi(parts[1])
					row, col = r-1, c-1
				}
			case "J":
				for i := range grid {
					grid[i] = []rune(strings.Repeat(" ", w))
				}
				row, col = 0, 0
			}
			continue
		}
		r := []rune(out)[0]
		out = out[len(string(r)):]
		switch r {
		case '\r':
			col = 0
		case '\n':
			row++
		default:
			if row < h && col < w {
				grid[row][col] = r
			}
			col++
		}
	}
	lines := make([]string, h)
	for i, g := range grid {
		lines[i] = strings.TrimRight(string(g), " ")
	}
	return lines
}

func TestAmilcScript(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "examples", "amilc.js"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := newUserProviders(t)
	start := time.Now().Add(-48 * time.Hour)
	p.UserMgr.AddCallRecord(user.CallRecord{UserID: 7, Handle: "Tester", GroupLocation: "Testville", NodeID: 2,
		ConnectTime: start, DisconnectTime: start.Add(20 * time.Minute), Duration: 20 * time.Minute})
	sysopIn := start.Add(time.Hour)
	p.UserMgr.AddCallRecord(user.CallRecord{UserID: 1, Handle: "Sysop", NodeID: 1, From: "MO/US",
		ConnectTime: sysopIn, DisconnectTime: sysopIn.Add(6 * time.Minute), Duration: 6 * time.Minute})
	sysop, _ := p.UserMgr.GetUserByID(1)
	sysop.NumDownloads = 2
	sysop.DownloadedBytes = 1536 * 1024
	sysop.MessagesPosted = 1
	if err := p.UserMgr.UpdateUserByID(sysop); err != nil {
		t.Fatal(err)
	}

	// DOWN then UP returns to Sysop; ENTER opens the box; ENTER closes it;
	// q quits.
	h := newHarness(t, harnessOpts{providers: p, input: "\x1b[B\x1b[A\r\rq"})
	// Sysop logged on with nothing to their name; the gain since is the call.
	h.writeFile("scripts/data/test.json", `{"snaps":{"1":[{"t":`+strconv.FormatInt(sysopIn.Unix()+5, 10)+
		`,"up":0,"dn":0,"wr":0,"ub":0,"db":0}]}}`)
	h.mustRun(string(src))

	out := h.output()
	lines := screen(out, 80, 24)
	for i, l := range lines {
		if len(l) > 79 {
			t.Errorf("line %d is %d columns: %q", i+1, len(l), l)
		}
	}
	find := func(handle string) string {
		for _, l := range lines {
			if len(l) > 5 && strings.HasPrefix(l[5:], handle) {
				return l
			}
		}
		t.Fatalf("no row for %s in:\n%s", handle, strings.Join(lines, "\n"))
		return ""
	}
	col := func(l string, from, to int) string {
		for len(l) < to {
			l += " "
		}
		return strings.TrimSpace(l[from-1 : to])
	}
	// Sysop is the selected row in the final frame: a solid bar whose
	// inner dividers are dropped, so the border check goes on Tester.
	s := find("Sysop")
	checks := []struct{ name, got, want string }{
		{"node", col(s, 3, 4), "1"},
		{"did", col(s, 40, 42), "D-W"},
		{"minutes", col(s, 44, 46), "6"},
		{"from", col(s, 57, 61), "US"},
		{"time", col(s, 49, 53), sysopIn.Format("01/02")},
		{"up", col(s, 64, 68), "-"},
		{"down", col(s, 70, 74), "1.5M"},
		{"wrote", col(s, 76, 77), "1"},
	}
	tr := find("Tester")
	checks = append(checks,
		struct{ name, got, want string }{"border", col(tr, 1, 1) + col(tr, 22, 22) + col(tr, 55, 55) + col(tr, 62, 62) + col(tr, 79, 79), "|||||"},
		struct{ name, got, want string }{"selected bar keeps borders edge to edge", col(s, 1, 1) + col(s, 22, 22) + col(s, 55, 55) + col(s, 62, 62) + col(s, 79, 79), "|||||"},
		struct{ name, got, want string }{"unknown did", col(tr, 40, 42), "---"},
		struct{ name, got, want string }{"unknown bytes", col(tr, 64, 78), ""},
		struct{ name, got, want string }{"where", col(tr, 24, 36), "Testville"},
		struct{ name, got, want string }{"tail in top border", col(lines[12], 30, 31), "::"},
		struct{ name, got, want string }{"scroller", col(lines[23], 1, 16), "amilc v1.3"},
		struct{ name, got, want string }{"star face", col(lines[4], 8, 9), "oO"},
	)
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if !strings.Contains(out, "ENTER view a caller") {
		t.Errorf("scroller missing key hint")
	}
	// ENTER, back on the first row, opened that caller's box. The box
	// colours each character, so strip the ANSI before matching the value.
	plain := regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]").ReplaceAllString(out, "")
	if !strings.Contains(plain, "2 files, 1.5M") {
		t.Errorf("caller box missing download totals")
	}
	if !strings.Contains(plain, "did:") {
		t.Errorf("caller box missing did legend")
	}
	// This logon was recorded for the current caller.
	data, _ := os.ReadFile(filepath.Join(h.dataDir, "test.json"))
	if !strings.Contains(string(data), `"7"`) {
		t.Errorf("no snapshot for the current caller: %s", data)
	}
	t.Logf("\n%s", strings.Join(lines, "\n"))
}
