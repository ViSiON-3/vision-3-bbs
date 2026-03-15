package menu

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// NUVVote represents a single vote cast on a NUV candidate.
type NUVVote struct {
	Voter   string    `json:"voter"`
	Yes     bool      `json:"yes"`
	Comment string    `json:"comment,omitempty"`
	VotedAt time.Time `json:"votedAt,omitempty"`
}

// NUVCandidate represents a new user pending community voting.
// Maps to V2's NuvRec.
type NUVCandidate struct {
	Handle string    `json:"handle"`
	When   time.Time `json:"when"`
	Votes  []NUVVote `json:"votes"`
}

// NUVData holds all NUV candidates.
type NUVData struct {
	Candidates []NUVCandidate `json:"candidates"`
}

var nuvMu sync.Mutex

func nuvFilePath(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "nuv.json")
}

func loadNUVData(rootConfigPath string) (*NUVData, error) {
	data, err := os.ReadFile(nuvFilePath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			return &NUVData{}, nil
		}
		return nil, fmt.Errorf("read nuv.json: %w", err)
	}
	var nd NUVData
	if err := json.Unmarshal(data, &nd); err != nil {
		return nil, fmt.Errorf("parse nuv.json: %w", err)
	}
	return &nd, nil
}

func saveNUVData(rootConfigPath string, nd *NUVData) error {
	data, err := json.MarshalIndent(nd, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal nuv data: %w", err)
	}
	return os.WriteFile(nuvFilePath(rootConfigPath), data, 0644)
}

// nuvAddCandidate adds a new user handle to the NUV queue.
// Called automatically after new user registration when AutoAddNUV is true.
func nuvAddCandidate(rootConfigPath, handle string) error {
	nuvMu.Lock()
	defer nuvMu.Unlock()
	nd, err := loadNUVData(rootConfigPath)
	if err != nil {
		log.Printf("WARN: NUV: failed to load nuv.json: %v", err)
		return fmt.Errorf("load nuv data: %w", err)
	}
	lower := strings.ToLower(handle)
	for _, c := range nd.Candidates {
		if strings.ToLower(c.Handle) == lower {
			return nil // already queued
		}
	}
	nd.Candidates = append(nd.Candidates, NUVCandidate{
		Handle: handle,
		When:   time.Now(),
	})
	if err := saveNUVData(rootConfigPath, nd); err != nil {
		log.Printf("WARN: NUV: failed to save nuv.json: %v", err)
		return fmt.Errorf("save nuv data: %w", err)
	}
	log.Printf("INFO: NUV: added candidate '%s' to queue", handle)
	return nil
}

// nuvVoteIndex returns the index of the voter's vote in candidate.Votes, or -1.
func nuvVoteIndex(c *NUVCandidate, handle string) int {
	lower := strings.ToLower(handle)
	for i, v := range c.Votes {
		if strings.ToLower(v.Voter) == lower {
			return i
		}
	}
	return -1
}

// nuvYesCount returns the number of YES votes for a candidate.
func nuvYesCount(c *NUVCandidate) int {
	n := 0
	for _, v := range c.Votes {
		if v.Yes {
			n++
		}
	}
	return n
}

// nuvDisplayStats shows the voting stats for a candidate (clears screen first).
// Matches V2's DisplayStats procedure in NUV.PAS.
func nuvDisplayStats(e *MenuExecutor, terminal *term.Terminal, c *NUVCandidate, idx int, outputMode ansi.OutputMode) {
	cfg := e.GetServerConfig()
	yes := nuvYesCount(c)
	no := len(c.Votes) - yes
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2J\x1b[H"), outputMode)
	wv(terminal, fmt.Sprintf("\r\n|15New User Voting — Candidate #%d\r\n|08%s\r\n", idx, strings.Repeat("\xc4", 50)), outputMode)

	// V2: NUV_Voting_On = '|08U|07s|15er |08N|07a|15me|09: |15|NA'
	nameStr := e.LoadedStrings.WhosBeingVotedOn
	if nameStr == "" {
		nameStr = "|08U|07s|15er |08N|07a|15me|09: |15|NA"
	}
	nameStr = strings.ReplaceAll(nameStr, "|NA", c.Handle)
	wv(terminal, nameStr+"\r\n", outputMode)

	// V2: NUV_Yes_Votes includes threshold: '...|YV  |09(|13Required|01: |05|YT Votes|09)'
	// Macros: |YV = current yes votes, |YT = yes threshold from config
	yesStr := e.LoadedStrings.NumYesVotes
	if yesStr == "" {
		yesStr = "|08Y|07e|15s |08V|07o|15tes|09: |15|YV"
		if cfg.NUVYesVotes > 0 {
			yesStr += "  |09(|13Required|01: |05|YT Votes|09)"
		}
	}
	yesStr = strings.ReplaceAll(yesStr, "|YV", fmt.Sprintf("%d", yes))
	yesStr = strings.ReplaceAll(yesStr, "|YT", fmt.Sprintf("%d", cfg.NUVYesVotes))
	wv(terminal, yesStr+"\r\n", outputMode)

	// V2: NUV_No_Votes includes threshold: '...|NV  |09(|13Deletion|01: |05|NT Votes|09)'
	// Macros: |NV = current no votes, |NT = no threshold from config
	noStr := e.LoadedStrings.NumNoVotes
	if noStr == "" {
		noStr = "|08N|07o |08V|07o|15tes|09: |15|NV"
		if cfg.NUVNoVotes > 0 {
			noStr += "  |09(|13Deletion|01: |05|NT Votes|09)"
		}
	}
	noStr = strings.ReplaceAll(noStr, "|NV", fmt.Sprintf("%d", no))
	noStr = strings.ReplaceAll(noStr, "|NT", fmt.Sprintf("%d", cfg.NUVNoVotes))
	wv(terminal, noStr+"\r\n", outputMode)

	wv(terminal, fmt.Sprintf("|07Added      : |11%s\r\n", c.When.Format("01/02/2006")), outputMode)

	// V2: NUV_Comment_Header = '|08C|07o|15mments |08S|07o |08F|07a|15r|09...'
	commentHdr := e.LoadedStrings.NUVCommentHeader
	if commentHdr == "" {
		commentHdr = "\r\n|08C|07o|15mments |08S|07o |08F|07a|15r|09..."
	}
	wv(terminal, commentHdr+"\r\n", outputMode)
	if len(c.Votes) > 0 {
		hasComments := false
		for _, v := range c.Votes {
			if v.Comment != "" {
				hasComments = true
				dateSuffix := ""
				if !v.VotedAt.IsZero() {
					dateSuffix = fmt.Sprintf(" |08%s", v.VotedAt.Format("01/02/06"))
				}
				// V2 style: Tab(Voter,27) then ': "comment"'
				wv(terminal, fmt.Sprintf("|07%-27s|09: |07\"%s\"%s\r\n", v.Voter, v.Comment, dateSuffix), outputMode)
			}
		}
		if !hasComments {
			wv(terminal, "|07No Comments Now!\r\n", outputMode)
		}
	} else {
		wv(terminal, "|07No Comments Now!\r\n", outputMode)
	}
	wv(terminal, "\r\n", outputMode)
}

// nuvApplyThresholds checks vote counts against config thresholds and acts.
// Returns true if the candidate was removed (validated or deleted).
func nuvApplyThresholds(e *MenuExecutor, nd *NUVData, idx int, userManager *user.UserMgr) bool {
	cfg := e.GetServerConfig()
	c := &nd.Candidates[idx]
	yes := nuvYesCount(c)
	no := len(c.Votes) - yes

	if cfg.NUVYesVotes > 0 && yes >= cfg.NUVYesVotes {
		shouldRemove := true
		if cfg.NUVValidate {
			shouldRemove = false
			if u, ok := userManager.GetUser(c.Handle); ok {
				u.AccessLevel = cfg.NUVLevel
				u.Validated = true
				if err := userManager.UpdateUser(u); err != nil {
					log.Printf("ERROR: NUV: failed to validate user '%s': %v", c.Handle, err)
				} else {
					log.Printf("INFO: NUV: auto-validated '%s' (level %d)", c.Handle, cfg.NUVLevel)
					shouldRemove = true
				}
			} else {
				log.Printf("ERROR: NUV: user '%s' not found during validation", c.Handle)
			}
		} else {
			log.Printf("INFO: NUV: '%s' reached YES threshold — notify SysOp to validate", c.Handle)
		}
		if shouldRemove {
			nd.Candidates = append(nd.Candidates[:idx], nd.Candidates[idx+1:]...)
			return true
		}
		return false
	}

	if cfg.NUVNoVotes > 0 && no >= cfg.NUVNoVotes {
		if cfg.NUVKill {
			if u, ok := userManager.GetUser(c.Handle); ok {
				u.DeletedUser = true
				if err := userManager.UpdateUser(u); err != nil {
					log.Printf("ERROR: NUV: failed to delete user '%s': %v", c.Handle, err)
				} else {
					log.Printf("INFO: NUV: auto-deleted '%s' (voted off)", c.Handle)
				}
			}
		} else {
			log.Printf("INFO: NUV: '%s' reached NO threshold — notify SysOp to delete", c.Handle)
		}
		nd.Candidates = append(nd.Candidates[:idx], nd.Candidates[idx+1:]...)
		return true
	}

	return false
}

// nuvPromptComment prompts for a comment on the current vote.
// Matches V2's NuvComment function.
func nuvPromptComment(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	currentUser *user.User, nd *NUVData, c *NUVCandidate,
	outputMode ansi.OutputMode) {

	commentPrompt := e.LoadedStrings.EnterNUVCommentPrompt
	if commentPrompt == "" {
		// V2: '|08E|07n|15ter |08a C|07o|15mment |08o|07n |15|NA ...'
		commentPrompt = "\r\n|08E|07n|15ter |08a C|07o|15mment |08o|07n |15|NA |09(|07Cr|09/|07Aborts|09)\r\n|09: "
	}
	commentPrompt = strings.ReplaceAll(commentPrompt, "|NA", c.Handle)
	wv(terminal, commentPrompt, outputMode)
	comment, _ := readLineFromSessionIH(s, terminal)
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return
	}
	if runes := []rune(comment); len(runes) > 80 {
		comment = string(runes[:80])
	}
	nuvMu.Lock()
	fresh, loadErr := loadNUVData(e.RootConfigPath)
	if loadErr == nil {
		freshIdx := nuvFindCandidate(fresh, c.Handle)
		if freshIdx >= 0 {
			vi := nuvVoteIndex(&fresh.Candidates[freshIdx], currentUser.Handle)
			if vi >= 0 {
				fresh.Candidates[freshIdx].Votes[vi].Comment = comment
				if err := saveNUVData(e.RootConfigPath, fresh); err != nil {
					log.Printf("WARN: NUV: failed to save comment: %v", err)
				} else {
					*nd = *fresh
				}
			}
		}
	}
	nuvMu.Unlock()
}

// nuvFindCandidate returns the index of a candidate by handle, or -1.
func nuvFindCandidate(nd *NUVData, handle string) int {
	for i := range nd.Candidates {
		if strings.EqualFold(nd.Candidates[i].Handle, handle) {
			return i
		}
	}
	return -1
}

// nuvShowHelp displays the NUV voting help screen. Matches V2's Help procedure.
func nuvShowHelp(terminal *term.Terminal, outputMode ansi.OutputMode) {
	wv(terminal, "\r\n|15New User Voting Help\r\n", outputMode)
	wv(terminal, "|07[|15Y|07] - Yes\r\n", outputMode)
	wv(terminal, "|07[|15N|07] - No\r\n", outputMode)
	wv(terminal, "|07[|15C|07] - Comment About User\r\n", outputMode)
	wv(terminal, "|07[|15I|07] - View Infoform\r\n", outputMode)
	wv(terminal, "|07[|15R|07] - Reshow Stats\r\n", outputMode)
	wv(terminal, "|07[|15Q|07] - Quit\r\n\r\n", outputMode)
}

// nuvVoteOn handles the interactive vote on a single candidate.
// Returns true if the candidate was removed from the queue after threshold.
// Matches V2's VoteOn procedure in NUV.PAS.
func nuvVoteOn(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User,
	nd *NUVData, idx int, outputMode ansi.OutputMode,
	termWidth, termHeight int) bool {

	ih := getSessionIH(s)
	c := &nd.Candidates[idx]
	cfg := e.GetServerConfig()

	// V2: NUV_Vote_Prompt = '|09New User Voting |01- |09(|10?|02/|10Help|09): '
	votePrompt := e.LoadedStrings.NUVVotePrompt
	if votePrompt == "" {
		votePrompt = "|09New User Voting |01- |09(|10?|02/|10Help|09): "
	}

	nuvDisplayStats(e, terminal, c, idx+1, outputMode)

	voterIdx := nuvVoteIndex(c, currentUser.Handle)
	if voterIdx >= 0 {
		vote := "|12No"
		if c.Votes[voterIdx].Yes {
			vote = "|10Yes"
		}
		// V2: 'Your Vote: Yes/No'
		wv(terminal, fmt.Sprintf("|07Your Vote|09: %s\r\n\r\n", vote), outputMode)
	}

	wv(terminal, votePrompt, outputMode)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return false
		}
		switch {
		case key == 'Q' || key == 'q' || key == editor.KeyEsc:
			return false

		case key == '?':
			nuvShowHelp(terminal, outputMode)
			wv(terminal, votePrompt, outputMode)

		case key == 'R' || key == 'r':
			nuvDisplayStats(e, terminal, c, idx+1, outputMode)
			if voterIdx >= 0 {
				vote := "|12No"
				if c.Votes[voterIdx].Yes {
					vote = "|10Yes"
				}
				wv(terminal, fmt.Sprintf("|07Your Vote|09: %s\r\n\r\n", vote), outputMode)
			}
			wv(terminal, votePrompt, outputMode)

		case key == 'I' || key == 'i':
			if cfg.NUVForm > 0 && cfg.NUVForm <= 5 {
				if u, ok := userManager.GetUser(c.Handle); ok {
					showInfoForm(e, s, terminal, outputMode, u.ID, cfg.NUVForm, termHeight)
					wv(terminal, "\r\n", outputMode)
					e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
				} else {
					wv(terminal, "\r\n|07User not found in database.\r\n", outputMode)
				}
				nuvDisplayStats(e, terminal, c, idx+1, outputMode)
				if voterIdx >= 0 {
					vote := "|12No"
					if c.Votes[voterIdx].Yes {
						vote = "|10Yes"
					}
					wv(terminal, fmt.Sprintf("|07Your Vote|09: %s\r\n\r\n", vote), outputMode)
				}
			} else {
				wv(terminal, "\r\n|07Infoform viewing is not configured.\r\n", outputMode)
			}
			wv(terminal, votePrompt, outputMode)

		case key == 'C' || key == 'c':
			if voterIdx < 0 {
				// V2: 'You have to Vote First!'
				wv(terminal, "\r\n|07You have to Vote First!\r\n", outputMode)
				wv(terminal, votePrompt, outputMode)
				continue
			}
			nuvPromptComment(e, s, terminal, currentUser, nd, c, outputMode)
			// Refresh pointers after comment save.
			freshIdx := nuvFindCandidate(nd, c.Handle)
			if freshIdx >= 0 {
				idx = freshIdx
				c = &nd.Candidates[idx]
			}
			wv(terminal, votePrompt, outputMode)

		case key == 'Y' || key == 'y' || key == 'N' || key == 'n':
			castYes := key == 'Y' || key == 'y'
			nuvMu.Lock()
			fresh, loadErr := loadNUVData(e.RootConfigPath)
			removed := false
			if loadErr == nil {
				freshIdx := nuvFindCandidate(fresh, c.Handle)
				if freshIdx >= 0 {
					vi := nuvVoteIndex(&fresh.Candidates[freshIdx], currentUser.Handle)
					isChange := vi >= 0
					if isChange {
						fresh.Candidates[freshIdx].Votes[vi].Yes = castYes
						fresh.Candidates[freshIdx].Votes[vi].VotedAt = time.Now()
					} else {
						fresh.Candidates[freshIdx].Votes = append(fresh.Candidates[freshIdx].Votes, NUVVote{
							Voter:   currentUser.Handle,
							Yes:     castYes,
							VotedAt: time.Now(),
						})
					}
					if castYes {
						if isChange {
							wv(terminal, "\r\n|07Vote changed to |10YES\r\n", outputMode)
						} else {
							// V2: NUV_Yes_Cast = '|04Y|12e|14s |09Vote Cast!'
							yesMsg := e.LoadedStrings.YesVoteCast
							if yesMsg == "" {
								yesMsg = "|04Y|12e|14s |09Vote Cast!"
							}
							wv(terminal, "\r\n"+yesMsg+"\r\n", outputMode)
						}
					} else {
						if isChange {
							wv(terminal, "\r\n|07Vote changed to |12NO\r\n", outputMode)
						} else {
							// V2: NUV_No_Cast = '|04N|12a|14h |09Vote Cast!'
							noMsg := e.LoadedStrings.NoVoteCast
							if noMsg == "" {
								noMsg = "|04N|12a|14h |09Vote Cast!"
							}
							wv(terminal, "\r\n"+noMsg+"\r\n", outputMode)
						}
					}
					_ = saveNUVData(e.RootConfigPath, fresh)
					*nd = *fresh
					idx = freshIdx
					c = &nd.Candidates[idx]
					voterIdx = nuvVoteIndex(c, currentUser.Handle)
					voteStr := "NO"
					if castYes {
						voteStr = "YES"
					}
					log.Printf("INFO: NUV: %s voted %s on '%s'", currentUser.Handle, voteStr, c.Handle)
					removed = nuvApplyThresholds(e, nd, idx, userManager)
					_ = saveNUVData(e.RootConfigPath, nd)

					// V2 auto-prompts for comment immediately after new vote.
					// Unlock before nuvPromptComment — it acquires nuvMu internally.
					if !removed && !isChange {
						nuvMu.Unlock()
						nuvPromptComment(e, s, terminal, currentUser, nd, c, outputMode)
						nuvMu.Lock()
						// Reload after comment to ensure consistency.
						fresh, loadErr = loadNUVData(e.RootConfigPath)
						if loadErr == nil {
							*nd = *fresh
						}
						freshIdx2 := nuvFindCandidate(nd, c.Handle)
						if freshIdx2 >= 0 {
							idx = freshIdx2
							c = &nd.Candidates[idx]
							voterIdx = nuvVoteIndex(c, currentUser.Handle)
						}
					}
				}
			}
			nuvMu.Unlock()
			if removed {
				wv(terminal, "|10Threshold reached — candidate processed.\r\n", outputMode)
				return true
			}
			wv(terminal, votePrompt, outputMode)
		}
	}
}
