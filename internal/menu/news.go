package menu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// NewsItem represents a single news item (maps to V2's newsrec).
type NewsItem struct {
	ID       int       `json:"id"`
	Title    string    `json:"title"` // max 28 chars (V2 String[28])
	From     string    `json:"from"`  // author handle
	When     time.Time `json:"when"`
	Level    int       `json:"level"`     // min access level
	MaxLevel int       `json:"max_level"` // max access level (0 = no max / all)
	Always   bool      `json:"always"`    // true = show every login; false = once (new since last login)
	Body     string    `json:"body"`      // news text body
}

// NewsData holds all news items.
type NewsData struct {
	Items []NewsItem `json:"items"`
	// NextID is the monotonic ID allocator. IDs must never be reused: users
	// carry seen-state keyed by ID, and handing a deleted item's ID to a new
	// one would hide the new item from everyone who had seen the old.
	// Deriving IDs from the live list alone is not enough, because deleting
	// the highest-numbered item frees its ID again.
	NextID int `json:"next_id,omitempty"`
}

var newsMu sync.Mutex

func newsFilePath(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "news.json")
}

func loadNewsData(rootConfigPath string) (*NewsData, error) {
	data, err := os.ReadFile(newsFilePath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			return &NewsData{}, nil
		}
		return nil, fmt.Errorf("read news.json: %w", err)
	}
	var nd NewsData
	if err := json.Unmarshal(data, &nd); err != nil {
		return nil, fmt.Errorf("parse news.json: %w", err)
	}
	return &nd, nil
}

func saveNewsData(rootConfigPath string, nd *NewsData) error {
	data, err := json.MarshalIndent(nd, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal news data: %w", err)
	}
	return os.WriteFile(newsFilePath(rootConfigPath), data, 0644)
}

// allocNewsID reserves the next unused ID and advances the allocator. IDs are
// never reused, including after the highest-numbered item is deleted.
//
// The floor is taken from the live items as well as NextID so that news.json
// files written before NextID existed cannot collide on the first allocation.
func allocNewsID(nd *NewsData) int {
	id := nd.NextID
	for _, it := range nd.Items {
		if it.ID >= id {
			id = it.ID + 1
		}
	}
	if id < 1 {
		id = 1
	}
	nd.NextID = id + 1
	return id
}

// normalizeNewsIDs assigns unique IDs to items missing one (ID <= 0) or
// sharing one with an earlier item. Earlier entries keep their ID so existing
// seen-sets stay valid. Reports whether anything changed.
//
// Repaired items draw from the monotonic allocator rather than filling the
// lowest free gap: a gap usually means a deleted item, and reusing its number
// would hide the repaired item from every user who had seen the original.
//
// This is deterministic for a given file, so callers that cannot write (user
// sessions) may normalize in memory and still agree with what the sysop editor
// eventually persists.
func normalizeNewsIDs(nd *NewsData) bool {
	used := make(map[int]bool, len(nd.Items))
	changed := false
	for i := range nd.Items {
		id := nd.Items[i].ID
		if id > 0 && !used[id] {
			used[id] = true
			continue
		}
		fresh := allocNewsID(nd)
		for used[fresh] {
			fresh = allocNewsID(nd)
		}
		nd.Items[i].ID = fresh
		used[fresh] = true
		changed = true
	}
	return changed
}

// newsSeenSet rebuilds the user's seen-ID set, dropping IDs for items that no
// longer exist so the list cannot grow without bound as news is deleted.
func newsSeenSet(u *user.User, items []NewsItem) map[int]bool {
	live := make(map[int]bool, len(items))
	for _, it := range items {
		if it.ID > 0 {
			live[it.ID] = true
		}
	}
	seen := make(map[int]bool, len(u.SeenNewsIDs))
	for _, id := range u.SeenNewsIDs {
		if live[id] {
			seen[id] = true
		}
	}
	return seen
}

// storeNewsSeen writes the seen set back to the user in a stable order and
// reports whether the stored value actually changed.
func storeNewsSeen(u *user.User, seen map[int]bool) bool {
	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) == len(u.SeenNewsIDs) {
		same := true
		for i, id := range ids {
			if u.SeenNewsIDs[i] != id {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}
	if len(ids) == 0 {
		ids = nil
	}
	u.SeenNewsIDs = ids
	return true
}

// initNewsSeen back-fills the seen set for a user who predates seen-tracking:
// everything posted on or before their previous visit counts as already read,
// so upgrading systems do not dump the whole news backlog on the next login.
func initNewsSeen(u *user.User, items []NewsItem, seen map[int]bool) {
	for _, it := range items {
		if it.ID > 0 && !it.Always && !it.When.After(u.PreviousLogin) {
			seen[it.ID] = true
		}
	}
	u.NewsSeenInitialized = true
}

// WarnIfNewsUnwired logs a one-time startup warning when the system has news
// items to show but no PRINTNEWS step to show them with.
//
// PRINTNEWS ships in the default login sequence, but setup only copies a
// template config when the target file does not already exist, so a system
// installed before PRINTNEWS was added keeps its old configs/login.json and
// silently never displays news. There is no config migration framework, and
// rewriting a sysop-owned file to add the step would not be able to tell
// "never had it" from "deliberately removed it" — so this only reports.
func WarnIfNewsUnwired(rootConfigPath string, loginSequence []config.LoginItem) {
	for _, item := range loginSequence {
		if strings.EqualFold(item.Command, "PRINTNEWS") {
			return
		}
	}

	newsMu.Lock()
	nd, err := loadNewsData(rootConfigPath)
	newsMu.Unlock()
	if err != nil || len(nd.Items) == 0 {
		// No news to show, so nothing is being missed.
		return
	}

	slog.Warn("system news items exist but will never be displayed at login",
		"items", len(nd.Items),
		"reason", "the login sequence has no PRINTNEWS step",
		"fix", `add {"command": "PRINTNEWS"} to configs/login.json`)
}

// displayNewsItem renders NEWSHDR.ANS with substitution vars, then the body text.
// Substitution vars (V2-compatible mapping):
//
//	^NM = item number   ^TI = title    ^FR = from/author
//	^DT = date          ^TM = time     ^LV = min level   ^MX = max level
func displayNewsItem(e *MenuExecutor, terminal *term.Terminal, item *NewsItem, idx int, outputMode ansi.OutputMode, termWidth int) {
	// Width of the header frame the body should line up under. Measured from
	// the header actually rendered, so a customized NEWSHDR.ANS or a different
	// menu set stays self-consistent instead of being hard-coded to 78.
	headerWidth := newsFallbackHeaderWidth

	ansiPath := filepath.Join(e.MenuSetPath, "ansi", "NEWSHDR.ANS")
	if raw, err := os.ReadFile(ansiPath); err == nil {
		headerWidth = headerTemplateWidth(string(raw))
		maxStr := strconv.Itoa(item.MaxLevel)
		if item.MaxLevel <= 0 {
			maxStr = "All"
		}
		hdr := string(raw)
		hdr = strings.ReplaceAll(hdr, "^NM", strconv.Itoa(idx))
		hdr = strings.ReplaceAll(hdr, "^TI", item.Title)
		hdr = strings.ReplaceAll(hdr, "^FR", item.From)
		hdr = strings.ReplaceAll(hdr, "^DT", item.When.Format("01/02/2006"))
		hdr = strings.ReplaceAll(hdr, "^TM", item.When.Format("3:04 pm"))
		hdr = strings.ReplaceAll(hdr, "^LV", strconv.Itoa(item.Level))
		hdr = strings.ReplaceAll(hdr, "^MX", maxStr)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(hdr)), outputMode)
	} else {
		// Fallback plain header if NEWSHDR.ANS is missing
		wv(terminal, fmt.Sprintf("\r\n|15News #%d: |11%s\r\n|07From: |11%s |07  Date: |11%s\r\n|08%s\r\n",
			idx, item.Title, item.From, item.When.Format("01/02/2006"),
			strings.Repeat("\xc4", newsFallbackHeaderWidth)), outputMode)
	}
	if item.Body != "" {
		// Word-wrap to the terminal, the same way the message reader renders a
		// message body. Without this the terminal hard-wraps at its own margin
		// and breaks words mid-word.
		//
		// Pipe codes are converted to ANSI *before* wrapping, matching the
		// reader. wrapAnsiString measures width with ANSI escapes stripped but
		// knows nothing about pipe codes, so wrapping first would count "|04"
		// as three visible columns and break lines far too early.
		//
		// wrapAnsiString leaves ANSI art alone, so a sysop who pastes art into
		// an item still gets it positioned correctly.
		width := newsBodyWidth(headerWidth, termWidth)
		body := string(ansi.ReplacePipeCodes([]byte(normalizeNewsBody(item.Body))))
		lines := wrapAnsiString(body, width)
		// wrapAnsiString breaks on spaces, so a token with no break opportunity
		// (a long URL, a path) comes back oversized; break those explicitly
		// rather than leaving the client terminal to chop them mid-token.
		//
		// Not for ANSI art: wrapAnsiString leaves art rows alone because they
		// are positioned absolutely, and hard-breaking a full-width row would
		// push everything below it down a line and wreck the picture.
		if !containsAnsiArt(body) {
			lines = breakOversizedLines(lines, width)
		}
		for _, line := range lines {
			// Already pipe-converted, so write straight through rather than
			// running it past ReplacePipeCodes a second time.
			terminalio.WriteProcessedBytes(terminal, []byte(line+"\r\n"), outputMode)
		}
	}
}

// normalizeNewsBody converts stored CRLF line endings to LF so wrapping sees
// clean lines; a stray CR would otherwise count toward the visible width.
func normalizeNewsBody(body string) string {
	return strings.ReplaceAll(body, "\r\n", "\n")
}

// newsFallbackHeaderWidth is the rule width of the built-in plain header used
// when NEWSHDR.ANS is missing, and the assumed frame width when a header
// cannot be measured.
const newsFallbackHeaderWidth = 70

// newsTerminalBudget is the widest a body line may be on this terminal.
//
// One column short of the terminal, because the body is written as a sequence
// of lines each ending in CRLF: a line filling the full width would trigger the
// terminal's own auto-wrap and the CRLF would then land on the next row,
// showing up as a blank line between every wrapped line. The same reservation
// is made elsewhere for sequentially written output.
//
// Falls back to 79 when the width is unknown, rather than to "do not wrap":
// 80 columns is the safe assumption for a BBS, and no wrapping is what
// produced broken words in the first place.
func newsTerminalBudget(termWidth int) int {
	if termWidth <= 1 {
		return 79
	}
	return termWidth - 1
}

// newsBodyWidth is the column budget for a wrapped news line: the header's
// frame width, so the text lines up under the rules NEWSHDR.ANS draws rather
// than running past them, but never wider than the terminal can show.
func newsBodyWidth(headerWidth, termWidth int) int {
	budget := newsTerminalBudget(termWidth)
	if headerWidth > 0 && headerWidth < budget {
		return headerWidth
	}
	return budget
}

// headerTemplateWidth is the visible width of the widest line in a header
// template — in practice the horizontal rules, which define the frame.
//
// Measured on the template before substitution: that is the width the header
// was designed to, and it keeps the body budget stable from item to item
// instead of drifting with the length of a title or author name.
func headerTemplateWidth(tmpl string) int {
	converted := string(ansi.ReplacePipeCodes([]byte(strings.ReplaceAll(tmpl, "\r\n", "\n"))))
	widest := 0
	for _, line := range strings.Split(converted, "\n") {
		if w := visibleWidth(line); w > widest {
			widest = w
		}
	}
	return widest
}

// runPrintNews displays news items the user has not seen yet, plus any
// Always-flagged items. Maps to V2's PrintNews(0, True) called at login,
// except that "unseen" is tracked per user by item ID instead of by date, so
// an item is not lost when a login is aborted before it is displayed.
func runPrintNews(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	newsMu.Lock()
	nd, err := loadNewsData(e.RootConfigPath)
	newsMu.Unlock()
	if err != nil {
		slog.Warn("failed to load news data", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	// Repair duplicate or missing IDs in memory. Seen-state is keyed by ID, so
	// duplicates would collapse several items under one key. This is
	// deterministic for a given file, so every session agrees; only the sysop
	// editor writes the repair back to disk.
	normalizeNewsIDs(nd)

	userLevel := currentUser.AccessLevel
	seen := newsSeenSet(currentUser, nd.Items)
	dirty := false
	if !currentUser.NewsSeenInitialized {
		// First run for this user: treat the existing backlog as read so an
		// upgrading system does not dump every old item on the next login.
		initNewsSeen(currentUser, nd.Items, seen)
		dirty = true
	}

	shown := 0
	for i, item := range nd.Items {
		if userLevel < item.Level {
			continue
		}
		if item.MaxLevel > 0 && userLevel > item.MaxLevel {
			continue
		}
		// Always-items reappear every login and are never marked seen.
		// Everything else shows exactly once, tracked by ID rather than by
		// date, so an item survives a dropped connection or a fast login and
		// is still waiting on the next call.
		if !item.Always {
			if item.ID <= 0 {
				// Untracked item (pre-normalization data): fall back to the
				// date filter rather than repeating it every single login.
				if !currentUser.PreviousLogin.IsZero() && !item.When.After(currentUser.PreviousLogin) {
					continue
				}
			} else if seen[item.ID] {
				continue
			}
		}

		displayNewsItem(e, terminal, &nd.Items[i], i+1, outputMode, termWidth)
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		shown++

		if !item.Always && item.ID > 0 {
			seen[item.ID] = true
		}
	}

	if storeNewsSeen(currentUser, seen) {
		dirty = true
	}
	if dirty && c.userManager != nil {
		if err := c.userManager.UpdateUser(currentUser); err != nil {
			// Non-fatal: the items simply show again next login.
			slog.Error("failed to save news seen-set", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		}
	}

	if shown > 0 {
		slog.Debug("displayed news items", "node", nodeNumber, "count", shown, "handle", currentUser.Handle)
	}
	return currentUser, "", nil
}

// runListNews presents all visible news items in a list and lets users read them.
// Maps to V2's PrintNews(0, False) — show all regardless of date.
func runListNews(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	slog.Debug("running LISTNEWS", "node", nodeNumber, "handle", currentUser.Handle)

	newsMu.Lock()
	nd, err := loadNewsData(e.RootConfigPath)
	newsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading news.\r\n", outputMode)
		return currentUser, "", nil
	}
	normalizeNewsIDs(nd)

	userLevel := currentUser.AccessLevel
	seen := newsSeenSet(currentUser, nd.Items)
	// Reaching the list before ever reaching PRINTNEWS still counts as the
	// user's first contact with seen-tracking; without the back-fill here the
	// entire backlog would be tagged [NEW].
	backfilled := false
	if !currentUser.NewsSeenInitialized {
		initNewsSeen(currentUser, nd.Items, seen)
		backfilled = true
	}
	var visible []int
	for i, item := range nd.Items {
		if userLevel < item.Level {
			continue
		}
		if item.MaxLevel > 0 && userLevel > item.MaxLevel {
			continue
		}
		visible = append(visible, i)
	}

	if len(visible) == 0 {
		wv(terminal, "\r\n|07No news available.\r\n", outputMode)
		return currentUser, "", nil
	}

	showList := func() {
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2J\x1b[H"), outputMode)
		wv(terminal, "\r\n|15System News\r\n|08"+strings.Repeat("\xc4", 70)+"\r\n", outputMode)
		for rank, idx := range visible {
			item := nd.Items[idx]
			newTag := "      "
			if !item.Always && item.ID > 0 && !seen[item.ID] {
				newTag = "|12[NEW]|07"
			}
			wv(terminal, fmt.Sprintf("  |11%2d|07. |15%-28s |07%s |11%s\r\n",
				rank+1, item.Title, newTag, item.When.Format("01/02/06")), outputMode)
		}
		wv(terminal, "|08"+strings.Repeat("\xc4", 70)+"\r\n", outputMode)
	}

	// Reading an item here counts as having seen it, so it does not reappear
	// at the next login.
	saveSeen := func() {
		if (storeNewsSeen(currentUser, seen) || backfilled) && c.userManager != nil {
			if err := c.userManager.UpdateUser(currentUser); err != nil {
				slog.Error("failed to save news seen-set", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
			}
		}
	}

	showList()
	for {
		prompt := fmt.Sprintf("|07Read which item |15[|111-%d|15]|07, or |15ENTER|07 to continue: ", len(visible))
		wv(terminal, prompt, outputMode)
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil || strings.TrimSpace(input) == "" {
			saveSeen()
			return currentUser, "", nil
		}
		n, nerr := strconv.Atoi(strings.TrimSpace(input))
		if nerr != nil || n < 1 || n > len(visible) {
			wv(terminal, "\r\n|07Invalid selection.\r\n", outputMode)
			continue
		}
		idx := visible[n-1]
		displayNewsItem(e, terminal, &nd.Items[idx], n, outputMode, termWidth)
		if item := nd.Items[idx]; !item.Always && item.ID > 0 {
			seen[item.ID] = true
		}
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		showList()
	}
}
