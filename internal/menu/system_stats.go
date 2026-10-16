package menu

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

func runSystemStats(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	topPath := e.templateFile("SYSSTATS.TOP")
	botPath := e.templateFile("SYSSTATS.BOT")

	topBytes, err := readTemplateFile(topPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to read template", "node", nodeNumber, "path", topPath, "error", err)
	}
	botBytes, err := readTemplateFile(botPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to read template", "node", nodeNumber, "path", botPath, "error", err)
	}

	topBytes = stripSauceMetadata(topBytes)
	botBytes = stripSauceMetadata(botBytes)
	topBytes = normalizePipeCodeDelimiters(topBytes)
	botBytes = normalizePipeCodeDelimiters(botBytes)

	sysopName := ""
	if sysopUser, ok := userManager.GetUserByID(1); ok {
		sysopName = sysopUser.Handle
	}

	now := config.NowIn(e.GetServerConfig().Timezone)
	tokens := map[string]string{
		"BBSNAME":     e.GetServerConfig().BoardName,
		"SYSOP":       sysopName,
		"VERSION":     version.Number,
		"TOTALUSERS":  strconv.Itoa(userManager.GetUserCount()),
		"TOTALCALLS":  strconv.FormatUint(userManager.GetTotalCalls(), 10),
		"TOTALMSGS":   strconv.Itoa(e.MessageMgr.GetTotalMessageCount()),
		"TOTALFILES":  strconv.Itoa(e.FileMgr.GetTotalFileCount()),
		"ACTIVENODES": strconv.Itoa(e.activeNodeCount()),
		"MAXNODES":    strconv.Itoa(e.GetServerConfig().MaxNodes),
		"DATE":        now.Format("01/02/2006"),
		"TIME":        now.Format("03:04 PM"),
	}

	// Apply @TOKEN@ substitution to template content
	replaceTokens := func(data []byte) []byte {
		s := string(data)
		for key, val := range tokens {
			s = strings.ReplaceAll(s, "@"+key+"@", val)
		}
		return []byte(s)
	}
	topBytes = replaceTokens(topBytes)
	botBytes = replaceTokens(botBytes)

	raw := func(b []byte) { terminalio.WriteProcessedBytes(terminal, b, outputMode) }
	crlf := []byte("\r\n")

	raw([]byte(ansi.ClearScreen()))
	if len(topBytes) > 0 {
		raw(ansi.ReplacePipeCodes(topBytes))
		raw(crlf)
	}

	// One call per line, each through writeStringf so any line can be a hook,
	// and spelled out so the stringformat call-site check sees every key.
	e.writeStringf(terminal, outputMode, e.Strings().StatsBBSName, tokens["BBSNAME"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsSysOp, tokens["SYSOP"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsVersion, tokens["VERSION"])
	raw(crlf)
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsTotalUsers, tokens["TOTALUSERS"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsTotalCalls, tokens["TOTALCALLS"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsTotalMsgs, tokens["TOTALMSGS"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsTotalFiles, tokens["TOTALFILES"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsActiveNodes, tokens["ACTIVENODES"], tokens["MAXNODES"])
	raw(crlf)
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsDate, tokens["DATE"])
	raw(crlf)
	e.writeStringf(terminal, outputMode, e.Strings().StatsTime, tokens["TIME"])
	raw(crlf)

	if len(botBytes) > 0 {
		raw(ansi.ReplacePipeCodes(botBytes))
		raw(crlf)
	}

	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := e.writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight); err != nil {
		return nil, "", err
	}

	return nil, "", nil
}
