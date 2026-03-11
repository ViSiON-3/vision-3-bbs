package menu

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/editor"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
	"golang.org/x/term"
)

// runCheckNUV is the login-sequence hook: if the user qualifies to vote (UseNUV
// and AccessLevel >= NUVUseLevel), and there are pending candidates they
// haven't voted on yet, notify them and offer a quick-scan.
// Mapped to login command "CHECKNUV".
func runCheckNUV(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	cfg := e.GetServerConfig()
	if !cfg.UseNUV || currentUser == nil {
		return currentUser, "", nil
	}
	if currentUser.AccessLevel < cfg.NUVUseLevel {
		return currentUser, "", nil
	}

	nuvMu.Lock()
	nd, err := loadNUVData(e.RootConfigPath)
	nuvMu.Unlock()
	if err != nil || len(nd.Candidates) == 0 {
		return currentUser, "", nil
	}

	// Count how many candidates this user hasn't voted on yet.
	unvoted := 0
	for i := range nd.Candidates {
		if nuvVoteIndex(&nd.Candidates[i], currentUser.Handle) < 0 {
			unvoted++
		}
	}
	if unvoted == 0 {
		return currentUser, "", nil
	}

	waitingStr := e.LoadedStrings.NewUsersWaiting
	if waitingStr == "" {
		waitingStr = "\r\n|15New User Voting: |11|NE candidate(s)|15 awaiting your vote!"
	}
	waitingStr = strings.ReplaceAll(waitingStr, "|NE", fmt.Sprintf("%d", unvoted))
	wv(terminal, waitingStr+"\r\n", outputMode)

	voteNowStr := e.LoadedStrings.VoteOnNewUsers
	if voteNowStr == "" {
		voteNowStr = "|07Vote now? |15[Y/N]|07: "
	}
	wv(terminal, voteNowStr, outputMode)

	ih := getSessionIH(s)
	key, err := ih.ReadKey()
	if err != nil {
		return currentUser, "", nil
	}
	if key != 'Y' && key != 'y' {
		wv(terminal, "\r\n", outputMode)
		return currentUser, "", nil
	}
	wv(terminal, "\r\n", outputMode)

	// Quick-scan: iterate through unvoted candidates.
	nuvMu.Lock()
	nd, err = loadNUVData(e.RootConfigPath)
	nuvMu.Unlock()
	if err != nil {
		return currentUser, "", nil
	}
	nuvRunScan(e, s, terminal, userManager, currentUser, nd, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runNUVScan lets the current user vote on all pending NUV candidates they
// haven't voted on. Mapped to menu command "SCANNUV".
func runNUVScan(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	cfg := e.GetServerConfig()
	if !cfg.UseNUV {
		wv(terminal, "\r\n|07New User Voting is disabled.\r\n", outputMode)
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		return currentUser, "", nil
	}
	if currentUser.AccessLevel < cfg.NUVUseLevel {
		wv(terminal, "\r\n|12You do not have access to New User Voting.\r\n", outputMode)
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		return currentUser, "", nil
	}

	nuvMu.Lock()
	nd, err := loadNUVData(e.RootConfigPath)
	nuvMu.Unlock()
	if err != nil {
		log.Printf("WARN: Node %d: SCANNUV: load error: %v", nodeNumber, err)
		return currentUser, "", nil
	}

	if len(nd.Candidates) == 0 {
		noPendingStr := e.LoadedStrings.NoNewUsersPending
		if noPendingStr == "" {
			noPendingStr = "|07No candidates pending in NUV queue."
		}
		wv(terminal, "\r\n"+noPendingStr+"\r\n", outputMode)
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		return currentUser, "", nil
	}

	nuvRunScan(e, s, terminal, userManager, currentUser, nd, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runNUVList displays all current NUV candidates with their vote tallies.
// Mapped to menu command "LISTNUV". SysOp-level; no UseNUV guard so the
// queue is always inspectable regardless of whether NUV is currently enabled.
func runNUVList(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	nuvMu.Lock()
	nd, err := loadNUVData(e.RootConfigPath)
	nuvMu.Unlock()
	if err != nil {
		log.Printf("WARN: Node %d: LISTNUV: load error: %v", nodeNumber, err)
		return currentUser, "", nil
	}

	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2J\x1b[H"), outputMode)
	wv(terminal, fmt.Sprintf("|15New User Voting Queue — %d Candidate(s)\r\n", len(nd.Candidates)), outputMode)
	wv(terminal, fmt.Sprintf("|08%s\r\n", strings.Repeat("\xc4", 60)), outputMode)

	if len(nd.Candidates) == 0 {
		wv(terminal, "|07No candidates pending.\r\n", outputMode)
	} else {
		wv(terminal, fmt.Sprintf("|08%-4s %-20s %-10s %4s %4s %6s\r\n", "#", "Handle", "Added", "Yes", "No", "Voted?"), outputMode)
		wv(terminal, fmt.Sprintf("|08%s\r\n", strings.Repeat("\xc4", 60)), outputMode)
		for i, c := range nd.Candidates {
			yes := nuvYesCount(&c)
			no := len(c.Votes) - yes
			voted := "|12No "
			if nuvVoteIndex(&c, currentUser.Handle) >= 0 {
				voted = "|10Yes"
			}
			wv(terminal, fmt.Sprintf("|07%-4d |11%-20s |07%-10s |10%4d |12%4d |07%s\r\n",
				i+1, c.Handle, c.When.Format("01/02/06"), yes, no, voted), outputMode)
		}
	}
	wv(terminal, "\r\n", outputMode)

	// Non-sysop users just see the list.
	if currentUser.AccessLevel < 255 {
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		return currentUser, "", nil
	}

	// Sysop interactive management loop.
	ih := getSessionIH(s)
	for {
		wv(terminal, "|15[A]|07dd  |15[R]|07emove #  |15[V]|07ote on #  |15[Q]|07uit: ", outputMode)
		key, err := ih.ReadKey()
		if err != nil {
			return currentUser, "", nil
		}

		switch {
		case key == 'Q' || key == 'q' || key == editor.KeyEsc:
			wv(terminal, "\r\n", outputMode)
			return currentUser, "", nil

		case key == 'A' || key == 'a':
			wv(terminal, "\r\n|07Handle to add: ", outputMode)
			handle, err := readLineFromSessionIH(s, terminal)
			if err != nil || strings.TrimSpace(handle) == "" {
				wv(terminal, "\r\n", outputMode)
				continue
			}
			handle = strings.TrimSpace(handle)
			if _, ok := userManager.GetUser(handle); !ok {
				wv(terminal, fmt.Sprintf("|12User '%s' not found.\r\n", handle), outputMode)
				continue
			}
			nuvAddCandidate(e.RootConfigPath, handle)
			wv(terminal, fmt.Sprintf("|10Added '%s' to NUV queue.\r\n", handle), outputMode)

		case key == 'R' || key == 'r':
			wv(terminal, "\r\n|07Remove candidate #: ", outputMode)
			numStr, err := readLineFromSessionIH(s, terminal)
			if err != nil || strings.TrimSpace(numStr) == "" {
				wv(terminal, "\r\n", outputMode)
				continue
			}
			num, err := strconv.Atoi(strings.TrimSpace(numStr))
			if err != nil || num < 1 {
				wv(terminal, "|12Invalid number.\r\n", outputMode)
				continue
			}
			nuvMu.Lock()
			nd, err = loadNUVData(e.RootConfigPath)
			if err != nil || num > len(nd.Candidates) {
				nuvMu.Unlock()
				wv(terminal, "|12Invalid candidate number.\r\n", outputMode)
				continue
			}
			removed := nd.Candidates[num-1]
			nd.Candidates = append(nd.Candidates[:num-1], nd.Candidates[num:]...)
			_ = saveNUVData(e.RootConfigPath, nd)
			nuvMu.Unlock()
			log.Printf("INFO: NUV: SysOp %s removed candidate '%s' from queue", currentUser.Handle, removed.Handle)
			wv(terminal, fmt.Sprintf("|10Removed '%s' from queue.\r\n", removed.Handle), outputMode)

		case key == 'V' || key == 'v':
			wv(terminal, "\r\n|07Vote on candidate #: ", outputMode)
			numStr, err := readLineFromSessionIH(s, terminal)
			if err != nil || strings.TrimSpace(numStr) == "" {
				wv(terminal, "\r\n", outputMode)
				continue
			}
			num, err := strconv.Atoi(strings.TrimSpace(numStr))
			if err != nil || num < 1 {
				wv(terminal, "|12Invalid number.\r\n", outputMode)
				continue
			}
			nuvMu.Lock()
			nd, err = loadNUVData(e.RootConfigPath)
			if err != nil || num > len(nd.Candidates) {
				nuvMu.Unlock()
				wv(terminal, "|12Invalid candidate number.\r\n", outputMode)
				continue
			}
			nuvMu.Unlock()
			nuvVoteOn(e, s, terminal, userManager, currentUser, nd, num-1, outputMode, termWidth, termHeight)

		default:
			continue
		}

		// Reload and redisplay the list.
		nuvMu.Lock()
		nd, err = loadNUVData(e.RootConfigPath)
		nuvMu.Unlock()
		if err != nil {
			return currentUser, "", nil
		}
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2J\x1b[H"), outputMode)
		wv(terminal, fmt.Sprintf("|15New User Voting Queue — %d Candidate(s)\r\n", len(nd.Candidates)), outputMode)
		wv(terminal, fmt.Sprintf("|08%s\r\n", strings.Repeat("\xc4", 60)), outputMode)
		if len(nd.Candidates) == 0 {
			wv(terminal, "|07No candidates pending.\r\n", outputMode)
		} else {
			wv(terminal, fmt.Sprintf("|08%-4s %-20s %-10s %4s %4s %6s\r\n", "#", "Handle", "Added", "Yes", "No", "Voted?"), outputMode)
			wv(terminal, fmt.Sprintf("|08%s\r\n", strings.Repeat("\xc4", 60)), outputMode)
			for i, c := range nd.Candidates {
				yes := nuvYesCount(&c)
				no := len(c.Votes) - yes
				voted := "|12No "
				if nuvVoteIndex(&c, currentUser.Handle) >= 0 {
					voted = "|10Yes"
				}
				wv(terminal, fmt.Sprintf("|07%-4d |11%-20s |07%-10s |10%4d |12%4d |07%s\r\n",
					i+1, c.Handle, c.When.Format("01/02/06"), yes, no, voted), outputMode)
			}
		}
		wv(terminal, "\r\n", outputMode)
	}
}

// nuvRunScan iterates through all candidates the user hasn't voted on and
// calls nuvVoteOn for each. Matches V2's NewScan procedure in NUV.PAS.
func nuvRunScan(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User,
	nd *NUVData, outputMode ansi.OutputMode, termWidth, termHeight int) {

	found := 0
	for i := 0; i < len(nd.Candidates); {
		if nuvVoteIndex(&nd.Candidates[i], currentUser.Handle) >= 0 {
			i++
			continue
		}
		found++
		removed := nuvVoteOn(e, s, terminal, userManager, currentUser, nd, i, outputMode, termWidth, termHeight)
		if removed {
			// candidate was deleted from nd; don't increment i
			continue
		}
		// V2: shows "Continuing New User Scan..." with brief pause between candidates.
		i++
		if i < len(nd.Candidates) {
			wv(terminal, "\r\n|07Continuing New User Scan...\r\n", outputMode)
			time.Sleep(500 * time.Millisecond)
		}
	}
	if found == 0 {
		wv(terminal, "|07No New Users Found!\r\n", outputMode)
	}
}
