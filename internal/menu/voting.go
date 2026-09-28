package menu

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// VoteTopic represents a single voting topic.
type VoteTopic struct {
	ID        int                 `json:"id"`
	Question  string              `json:"question"`
	Options   []string            `json:"options"`
	Mandatory bool                `json:"mandatory,omitempty"`
	AddLevel  int                 `json:"add_level,omitempty"` // min sec level to add choices; 0=disabled
	Votes     map[string][]string `json:"votes"`               // option index → []handle
}

// VotingData holds all voting topics.
type VotingData struct {
	Topics []VoteTopic `json:"topics"`
}

var votingMu sync.Mutex

func votingFilePath(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "voting.json")
}

func loadVotingData(rootConfigPath string) (*VotingData, error) {
	data, err := os.ReadFile(votingFilePath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			return &VotingData{}, nil
		}
		return nil, fmt.Errorf("read voting.json: %w", err)
	}
	var vd VotingData
	if err := json.Unmarshal(data, &vd); err != nil {
		return nil, fmt.Errorf("parse voting.json: %w", err)
	}
	for i := range vd.Topics {
		if vd.Topics[i].Votes == nil {
			vd.Topics[i].Votes = make(map[string][]string)
		}
	}
	normalizeVoteIDs(&vd)
	return &vd, nil
}

// nextVoteTopicID returns an ID one past the highest in use. Deriving it from
// the topic count instead hands out a live ID once any topic is deleted.
func nextVoteTopicID(vd *VotingData) int {
	maxID := 0
	for _, t := range vd.Topics {
		if t.ID > maxID {
			maxID = t.ID
		}
	}
	return maxID + 1
}

// normalizeVoteIDs gives a new ID to any topic that has none or shares one
// with an earlier topic, so each ID names exactly one topic. voting.json files
// written while IDs were count-based can hold duplicates. Earlier topics keep
// their ID, and votes are stored inside each topic, so no vote moves.
//
// The repair is deterministic for a given file, so every session that loads
// the file agrees on the new IDs whether or not one of them has saved them yet.
// Reports whether anything changed.
func normalizeVoteIDs(vd *VotingData) bool {
	used := make(map[int]bool, len(vd.Topics))
	var fix []int
	for i, t := range vd.Topics {
		if t.ID > 0 && !used[t.ID] {
			used[t.ID] = true
			continue
		}
		fix = append(fix, i)
	}
	for _, i := range fix {
		vd.Topics[i].ID = nextVoteTopicID(vd)
	}
	return len(fix) > 0
}

// findVoteTopicByID returns the index of the topic with the given ID, or -1.
func findVoteTopicByID(vd *VotingData, id int) int {
	for i := range vd.Topics {
		if vd.Topics[i].ID == id {
			return i
		}
	}
	return -1
}

// errVoteTopicGone reports that another session deleted a topic after this
// one listed it.
var errVoteTopicGone = errors.New("voting topic no longer exists")

func saveVotingData(rootConfigPath string, vd *VotingData) error {
	data, err := json.MarshalIndent(vd, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal voting data: %w", err)
	}
	fp := votingFilePath(rootConfigPath)
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		return fmt.Errorf("create voting data dir: %w", err)
	}
	return os.WriteFile(fp, data, 0644)
}

func hasVoted(topic *VoteTopic, handle string) bool {
	lower := strings.ToLower(handle)
	for _, voters := range topic.Votes {
		for _, v := range voters {
			if strings.ToLower(v) == lower {
				return true
			}
		}
	}
	return false
}

func totalVotes(topic *VoteTopic) int {
	n := 0
	for _, v := range topic.Votes {
		n += len(v)
	}
	return n
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func wv(terminal *term.Terminal, msg string, outputMode ansi.OutputMode) {
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
}

// voteDrawHeader clears the screen and draws VOTEHDR.ANS from the menu set's
// ansi directory, falling back to a plain text title when the art is absent.
func voteDrawHeader(e *MenuExecutor, terminal *term.Terminal, outputMode ansi.OutputMode, termWidth int) {
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	headerContent, err := ansi.GetAnsiFileContent(e.menuFile("ansi", "VOTEHDR.ANS"))
	if err == nil {
		_ = writeArt(terminal, headerContent, outputMode, termWidth) // best-effort display
		if !bytes.HasSuffix(headerContent, []byte("\n")) {
			wv(terminal, "\r\n", outputMode)
		}
		return
	}
	wv(terminal, "|15Voting Booths\r\n", outputMode)
}

func voteListTopics(terminal *term.Terminal, vd *VotingData, currentUser *user.User, outputMode ansi.OutputMode) {
	wv(terminal, "|08"+strings.Repeat("\xc4", 50)+"\r\n", outputMode)
	for i, t := range vd.Topics {
		tags := ""
		if t.Mandatory {
			tags += " |12[MANDATORY]|07"
		}
		if currentUser != nil && hasVoted(&t, currentUser.Handle) {
			tags += " |10[Voted]|07"
		}
		wv(terminal, fmt.Sprintf("  |11%2d|07. |15%s|07%s\r\n", i+1, t.Question, tags), outputMode)
	}
	wv(terminal, "|08"+strings.Repeat("\xc4", 50)+"\r\n", outputMode)
}

func voteListChoices(terminal *term.Terminal, topic *VoteTopic, outputMode ansi.OutputMode) {
	wv(terminal, fmt.Sprintf("\r\n|15%s\r\n|08%s\r\n", topic.Question, strings.Repeat("\xc4", 40)), outputMode)
	for i, opt := range topic.Options {
		wv(terminal, fmt.Sprintf("  |11%2d|07. |15%s|07\r\n", i+1, opt), outputMode)
	}
}

func voteShowResults(terminal *term.Terminal, topic *VoteTopic, outputMode ansi.OutputMode) {
	total := totalVotes(topic)
	wv(terminal, fmt.Sprintf("\r\n|15%s |08(%d vote%s)\r\n", topic.Question, total, pluralS(total)), outputMode)
	for i, opt := range topic.Options {
		count := len(topic.Votes[strconv.Itoa(i)])
		pct := 0.0
		if total > 0 {
			pct = float64(count) / float64(total) * 100
		}
		barLen := int(pct / 5)
		bar := strings.Repeat("\xdb", barLen) + strings.Repeat("\xb0", 20-barLen)
		wv(terminal, fmt.Sprintf("  |11%-20s |09%s |15%5.1f%% |08(%d)\r\n", opt, bar, pct, count), outputMode)
	}
}

// voteRecordVote atomically records a user's vote on the topic with the given
// ID. Returns the updated topic, or errVoteTopicGone when another session has
// deleted it. The topic is found by ID, not by list position, because the list
// may have changed since the caller loaded it.
func voteRecordVote(rootConfigPath string, topicID, optionIdx int, handle string) (*VoteTopic, error) {
	votingMu.Lock()
	defer votingMu.Unlock()
	vd, err := loadVotingData(rootConfigPath)
	if err != nil {
		return nil, err
	}
	topicIdx := findVoteTopicByID(vd, topicID)
	if topicIdx < 0 {
		return nil, errVoteTopicGone
	}
	t := &vd.Topics[topicIdx]
	if optionIdx < 0 || optionIdx >= len(t.Options) {
		return nil, fmt.Errorf("option index %d out of range (have %d options)", optionIdx, len(t.Options))
	}
	if hasVoted(t, handle) {
		return t, nil
	}
	key := strconv.Itoa(optionIdx)
	t.Votes[key] = append(t.Votes[key], handle)
	return t, saveVotingData(rootConfigPath, vd)
}

// doVoteOnTopic handles the vote interaction for one topic. Returns true if the
// user voted; otherwise notice, when non-empty, says why the vote was not taken
// and is left for the caller to show.
func doVoteOnTopic(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	currentUser *user.User, vd *VotingData, topicIdx int,
	outputMode ansi.OutputMode, termWidth, termHeight int) (voted bool, notice string) {

	topic := &vd.Topics[topicIdx]
	voteListChoices(terminal, topic, outputMode)

	canAdd := topic.AddLevel > 0 && currentUser != nil && currentUser.AccessLevel >= topic.AddLevel
	prompt := fmt.Sprintf("\r\n|07Your selection |15[|111-%d|15]|07", len(topic.Options))
	if canAdd {
		prompt += ", |15[A]|07dd choice"
	}
	prompt += ": "
	wv(terminal, prompt, outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return false, ""
	}
	input = strings.TrimSpace(input)

	if strings.ToUpper(input) == "A" && canAdd {
		wv(terminal, "|07New choice: ", outputMode)
		choice, err := readLineFromSessionIH(s, terminal)
		if err != nil || strings.TrimSpace(choice) == "" {
			return false, ""
		}
		votingMu.Lock()
		defer votingMu.Unlock()
		fresh, loadErr := loadVotingData(e.RootConfigPath)
		if loadErr != nil {
			slog.Error("failed to load voting data for adding choice", "error", loadErr)
			return false, "|04Error loading voting data."
		}
		freshIdx := findVoteTopicByID(fresh, topic.ID)
		if freshIdx < 0 {
			return false, "|07Topic no longer exists. Choice not added."
		}
		fresh.Topics[freshIdx].Options = append(fresh.Topics[freshIdx].Options, strings.TrimSpace(choice))
		if saveErr := saveVotingData(e.RootConfigPath, fresh); saveErr != nil {
			slog.Error("failed to save voting data after adding choice", "error", saveErr)
			return false, "|04Error saving choice."
		}
		*vd = *fresh
		return false, "|10Choice added!"
	}

	if input == "" {
		return false, ""
	}

	n, err := strconv.Atoi(input)
	if err != nil || n < 1 || n > len(topic.Options) {
		return false, "|07Invalid selection. Vote not recorded."
	}

	updated, saveErr := voteRecordVote(e.RootConfigPath, topic.ID, n-1, currentUser.Handle)
	if errors.Is(saveErr, errVoteTopicGone) {
		return false, "|07Topic no longer exists. Vote not recorded."
	}
	if saveErr != nil {
		slog.Error("vote save failed", "error", saveErr)
		return false, "|04Error saving vote."
	}
	if updated != nil {
		vd.Topics[topicIdx] = *updated
	}
	wv(terminal, "\r\n|10Thanks for voting!\r\n\r\n", outputMode)
	voteShowResults(terminal, &vd.Topics[topicIdx], outputMode)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return true, ""
}

// runVoteOnMandatory forces the user to vote on any mandatory topics they haven't voted on.
// Intended for use in the login sequence via VOTEMANDATORY.
func runVoteOnMandatory(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}
	votingMu.Lock()
	vd, err := loadVotingData(e.RootConfigPath)
	votingMu.Unlock()
	if err != nil {
		return currentUser, "", nil
	}
	for i := range vd.Topics {
		if !vd.Topics[i].Mandatory || hasVoted(&vd.Topics[i], currentUser.Handle) {
			continue
		}
		wv(terminal, "\r\n|12Mandatory Voting!\r\n", outputMode)
		if _, notice := doVoteOnTopic(e, s, terminal, currentUser, vd, i, outputMode, termWidth, termHeight); notice != "" {
			wv(terminal, "\r\n"+notice+"\r\n", outputMode)
		}
	}
	return currentUser, "", nil
}

// voteBoothPrompt builds the voting booth command prompt. It stays on one
// 80-column line with room left to type, whichever options are offered.
func voteBoothPrompt(voted, isSysOp bool, topicCount int) string {
	prompt := "|15[V]|07ote |15[L]|07ist "
	if voted {
		prompt += "|15[R]|07esults "
	}
	prompt += fmt.Sprintf("|15[N]|07ext |15[|111-%d|15]|07 Topic ", topicCount)
	if isSysOp {
		prompt += "|15[A]|07dd |15[D]|07el "
	}
	return prompt + "|15[Q]|07uit: "
}

// runVote presents the full voting booths interface.
func runVote(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	slog.Debug("running VOTE", "node", nodeNumber, "handle", currentUser.Handle)
	isSysOp := e.isCoSysOpOrAbove(currentUser)

	voteDrawHeader(e, terminal, outputMode, termWidth)

	votingMu.Lock()
	vd, err := loadVotingData(e.RootConfigPath)
	votingMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading voting data.\r\n", outputMode)
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		return currentUser, "", nil
	}

	// notice is a one-line message from the last action, shown above the
	// prompt after the screen is redrawn.
	notice := ""

	if len(vd.Topics) == 0 {
		wv(terminal, "\r\n|07No voting topics right now.\r\n", outputMode)
		if !isSysOp {
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			return currentUser, "", nil
		}
		wv(terminal, "|07Create first topic? [Y/N]: ", outputMode)
		input, _ := readLineFromSessionIH(s, terminal)
		if strings.ToUpper(strings.TrimSpace(input)) == "Y" {
			vd, notice = voteAddTopic(e, s, terminal, vd, outputMode)
		}
		if len(vd.Topics) == 0 {
			if notice != "" {
				wv(terminal, notice+"\r\n", outputMode)
				e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			}
			return currentUser, "", nil
		}
	}

	curIdx := 0
	for {
		voteDrawHeader(e, terminal, outputMode, termWidth)
		voteListTopics(terminal, vd, currentUser, outputMode)
		topic := &vd.Topics[curIdx]
		voted := hasVoted(topic, currentUser.Handle)

		wv(terminal, fmt.Sprintf("\r\n|07Current topic |15[|11%d|15]|07: |15%s|07\r\n", curIdx+1, topic.Question), outputMode)
		if notice != "" {
			wv(terminal, notice+"|07\r\n", outputMode)
			notice = ""
		}

		wv(terminal, "\r\n"+voteBoothPrompt(voted, isSysOp, len(vd.Topics)), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return currentUser, "", nil
		}
		cmd := strings.ToUpper(strings.TrimSpace(input))

		switch {
		case cmd == "Q" || cmd == "":
			return currentUser, "", nil
		case cmd == "N":
			curIdx = (curIdx + 1) % len(vd.Topics)
		case cmd == "L":
			voteListChoices(terminal, topic, outputMode)
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		case cmd == "V":
			if voted {
				notice = "|07Sorry, can't vote twice!!"
			} else {
				_, notice = doVoteOnTopic(e, s, terminal, currentUser, vd, curIdx, outputMode, termWidth, termHeight)
			}
		case cmd == "R":
			if !voted {
				notice = "|07Sorry, you must vote first!"
			} else {
				voteShowResults(terminal, topic, outputMode)
				e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			}
		case cmd == "A" && isSysOp:
			vd, notice = voteAddTopic(e, s, terminal, vd, outputMode)
		case cmd == "D" && isSysOp:
			wv(terminal, fmt.Sprintf("\r\n|07Delete topic %d (%s)? [Y/N]: ", curIdx+1, topic.Question), outputMode)
			confirm, _ := readLineFromSessionIH(s, terminal)
			if strings.ToUpper(strings.TrimSpace(confirm)) == "Y" {
				votingMu.Lock()
				fresh, loadErr := loadVotingData(e.RootConfigPath)
				if loadErr != nil {
					slog.Error("failed to load voting data for topic deletion", "error", loadErr)
					notice = "|04Error loading voting data."
				} else if freshIdx := findVoteTopicByID(fresh, topic.ID); freshIdx < 0 {
					// Another session deleted it first; carry on with the
					// topics as they are now.
					notice = "|07Topic no longer exists."
					vd = fresh
				} else {
					fresh.Topics = append(fresh.Topics[:freshIdx], fresh.Topics[freshIdx+1:]...)
					if saveErr := saveVotingData(e.RootConfigPath, fresh); saveErr != nil {
						slog.Error("failed to save voting data after topic deletion", "error", saveErr)
						notice = "|04Error deleting topic."
					} else {
						vd = fresh
						curIdx = freshIdx
					}
				}
				votingMu.Unlock()
				if len(vd.Topics) == 0 {
					wv(terminal, "\r\n|07No voting topics right now.\r\n", outputMode)
					e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
					return currentUser, "", nil
				}
				if curIdx >= len(vd.Topics) {
					curIdx = 0
				}
			}
		default:
			n, err := strconv.Atoi(cmd)
			if err == nil && n >= 1 && n <= len(vd.Topics) {
				curIdx = n - 1
			}
		}
	}
}

// voteAddTopic interactively creates a new topic (sysop only). It returns the
// voting data to carry on with and a one-line notice of the outcome for the
// caller to show.
func voteAddTopic(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	vd *VotingData, outputMode ansi.OutputMode) (*VotingData, string) {

	wv(terminal, "\r\n|07Voting question: ", outputMode)
	question, err := readLineFromSessionIH(s, terminal)
	if err != nil || strings.TrimSpace(question) == "" {
		return vd, ""
	}

	wv(terminal, "|07Make this topic mandatory? [Y/N]: ", outputMode)
	mandInput, _ := readLineFromSessionIH(s, terminal)
	mandatory := strings.ToUpper(strings.TrimSpace(mandInput)) == "Y"

	addLevel := 0
	wv(terminal, "|07Allow users to add their own choices? [Y/N]: ", outputMode)
	addInput, _ := readLineFromSessionIH(s, terminal)
	if strings.ToUpper(strings.TrimSpace(addInput)) == "Y" {
		wv(terminal, "|07Minimum security level to add choices: ", outputMode)
		lvlInput, _ := readLineFromSessionIH(s, terminal)
		addLevel, _ = strconv.Atoi(strings.TrimSpace(lvlInput))
	}

	t := VoteTopic{
		Question:  strings.TrimSpace(question),
		Mandatory: mandatory,
		AddLevel:  addLevel,
		Votes:     make(map[string][]string),
	}

	wv(terminal, "|07Enter choices (blank line to end):\r\n", outputMode)
	for {
		wv(terminal, fmt.Sprintf("|07Choice %d: ", len(t.Options)+1), outputMode)
		choice, err := readLineFromSessionIH(s, terminal)
		if err != nil || strings.TrimSpace(choice) == "" {
			break
		}
		t.Options = append(t.Options, strings.TrimSpace(choice))
	}

	if len(t.Options) == 0 {
		return vd, "|07No choices entered, topic not created."
	}

	votingMu.Lock()
	defer votingMu.Unlock()
	fresh, loadErr := loadVotingData(e.RootConfigPath)
	if loadErr != nil {
		slog.Error("failed to load voting data for topic creation", "error", loadErr)
		return vd, "|04Error loading voting data."
	}
	// Assign the ID from the reloaded data, under the lock, so concurrent
	// creations cannot collide.
	t.ID = nextVoteTopicID(fresh)
	fresh.Topics = append(fresh.Topics, t)
	if saveErr := saveVotingData(e.RootConfigPath, fresh); saveErr != nil {
		slog.Error("failed to save voting data after topic creation", "error", saveErr)
		return vd, "|04Error saving topic."
	}
	return fresh, "|10Topic created!"
}
