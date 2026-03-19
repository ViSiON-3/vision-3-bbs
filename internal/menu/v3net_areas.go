package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// v3netAreaEntry holds one NAL area and its local subscription status.
type v3netAreaEntry struct {
	network    string
	hubURL     string
	area       protocol.Area
	subscribed bool
}

func runV3NetAreas(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, _ *user.UserMgr, currentUser *user.User, _ int, _ time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}

	svc := e.V3NetStatus
	log.Printf("DEBUG: V3NetAreas: termWidth=%d termHeight=%d outputMode=%d", termWidth, termHeight, outputMode)

	// Determine which networks to show.
	var networks []string
	if args != "" {
		networks = []string{args}
	} else {
		networks = svc.LeafNetworks()
	}
	if len(networks) == 0 {
		return v3netAreasShowMessage(e, terminal, s, outputMode, termWidth, termHeight,
			"|08No V3Net subscriptions configured.|07")
	}

	// Load current v3net.json to determine subscription status.
	v3cfg, err := config.LoadV3NetConfig(e.RootConfigPath)
	if err != nil {
		return v3netAreasShowMessage(e, terminal, s, outputMode, termWidth, termHeight,
			fmt.Sprintf("|04Error loading config: %s|07", err))
	}

	// Build set of subscribed board tags per network.
	subSet := make(map[string]bool) // "network:board" → true
	for _, lcfg := range v3cfg.Leaves {
		for _, board := range lcfg.Boards {
			subSet[lcfg.Network+":"+board] = true
		}
	}

	// Fetch NAL for each network and build entry list.
	var entries []v3netAreaEntry
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Show loading message.
	terminalio.WriteProcessedBytes(terminal,
		ansi.ReplacePipeCodes([]byte(ansi.ClearScreen()+"|08Fetching area lists...|07\r\n")), outputMode)

	var fetchErrors []string
	for _, net := range networks {
		hubURL := svc.HubURLForNetwork(net)
		nalData, fetchErr := svc.FetchNALForNetwork(ctx, net)
		if fetchErr != nil {
			fetchErrors = append(fetchErrors, fmt.Sprintf("%s: %s", net, fetchErr.Error()))
			continue
		}
		for _, a := range nalData.Areas {
			entries = append(entries, v3netAreaEntry{
				network:    net,
				hubURL:     hubURL,
				area:       a,
				subscribed: subSet[net+":"+a.Tag],
			})
		}
	}

	if len(entries) == 0 {
		msg := "|08No areas found in NAL.|07"
		if len(fetchErrors) > 0 {
			// Simplify common error for display.
			errStr := fetchErrors[0]
			if strings.Contains(errStr, "status 404") {
				msg = fmt.Sprintf("|04No area list published for %s yet.|07", networks[0])
			} else {
				msg = fmt.Sprintf("|04Could not fetch area list: %s|07", errStr)
			}
		}
		return v3netAreasShowMessage(e, terminal, s, outputMode, termWidth, termHeight, msg)
	}

	// Interactive lightbar loop.
	ih := getSessionIH(s)
	selectedIndex := 0
	topIndex := 0
	statusMsg := ""
	needFullRedraw := true

	// Layout: row 1 = header, row 2 = separator, row 3 = col headers,
	// rows 4..N = items, N+1 = separator, N+2 = help/status.
	// We use only 2 footer rows to keep the last content on termHeight-1,
	// avoiding writes to termHeight which cause terminal scroll.
	headerRows := 3
	footerRows := 2
	visibleRows := termHeight - headerRows - footerRows
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Hide cursor during lightbar.
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	clampSelection := func() {
		if selectedIndex < 0 {
			selectedIndex = 0
		}
		if selectedIndex >= len(entries) {
			selectedIndex = len(entries) - 1
		}
		if selectedIndex < topIndex {
			topIndex = selectedIndex
		}
		if selectedIndex >= topIndex+visibleRows {
			topIndex = selectedIndex - visibleRows + 1
		}
		if topIndex < 0 {
			topIndex = 0
		}
	}

	renderRow := func(idx int, highlight bool) string {
		if idx < 0 || idx >= len(entries) {
			return strings.Repeat(" ", termWidth)
		}
		ent := entries[idx]
		status := "[ ]"
		if ent.subscribed {
			status = "[*]"
		}
		tag := padRight(truncateStr(ent.area.Tag, 24), 24)
		name := padRight(truncateStr(ent.area.Name, 28), 28)
		access := padRight(ent.area.Access.Mode, 8)
		net := ent.network

		line := fmt.Sprintf(" %s %-24s %-28s %-8s %s", status, tag, name, access, net)
		maxW := termWidth - 1 // leave 1 col to prevent terminal auto-wrap
		if len(line) < maxW {
			line += strings.Repeat(" ", maxW-len(line))
		} else if len(line) > maxW {
			line = line[:maxW]
		}

		if highlight {
			return "\x1b[1;37;44m" + line + "\x1b[0m"
		}
		return line
	}

	renderFull := func() {
		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())

		// Row 1: Header.
		buf.WriteString(ansi.MoveCursor(1, 1))
		header := "|12V3Net Area Subscriptions|07"
		if len(networks) == 1 {
			header = fmt.Sprintf("|12V3Net Areas: %s|07", networks[0])
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(header)))

		// Row 2: Separator.
		buf.WriteString(ansi.MoveCursor(2, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("─", termWidth-1) + "|07")))

		// Row 3: Column headers.
		buf.WriteString(ansi.MoveCursor(3, 1))
		colHdr := fmt.Sprintf("|03 Sub %-24s %-28s %-8s %s|07", "Tag", "Name", "Access", "Network")
		buf.Write(ansi.ReplacePipeCodes([]byte(colHdr)))

		// Rows 4..4+visibleRows-1: Item rows.
		for i := 0; i < visibleRows; i++ {
			buf.WriteString(ansi.MoveCursor(headerRows+1+i, 1))
			idx := topIndex + i
			hl := idx == selectedIndex
			buf.WriteString(renderRow(idx, hl))
		}

		// Footer separator (row = headerRows + visibleRows + 1 = termHeight - 1).
		footerSepRow := headerRows + visibleRows + 1
		buf.WriteString(ansi.MoveCursor(footerSepRow, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("─", termWidth-1) + "|07")))

		// Help line (row = termHeight — the last row, but we only write
		// partial content and never write a newline, so no scroll).
		helpRow := footerSepRow + 1
		buf.WriteString(ansi.MoveCursor(helpRow, 1))
		help := "|08 [|15Space|08] Toggle  [|15Q|08] Quit|07"
		scrollInfo := fmt.Sprintf("  |08%d/%d|07", selectedIndex+1, len(entries))
		if statusMsg != "" {
			help += "  " + statusMsg
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(help + scrollInfo)))

		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
	}

	renderItemRow := func(idx int) {
		screenRow := headerRows + 1 + (idx - topIndex)
		if screenRow < headerRows+1 || screenRow > headerRows+visibleRows {
			return
		}
		hl := idx == selectedIndex
		line := ansi.MoveCursor(screenRow, 1) + renderRow(idx, hl)
		terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
	}

	// renderHelpRow redraws the combined help/status/scroll line on the last content row.
	renderHelpRow := func() {
		helpRow := headerRows + visibleRows + 2
		line := ansi.MoveCursor(helpRow, 1) + "\x1b[2K"
		help := "|08 [|15Space|08] Toggle  [|15Q|08] Quit|07"
		scrollInfo := fmt.Sprintf("  |08%d/%d|07", selectedIndex+1, len(entries))
		if statusMsg != "" {
			help += "  " + statusMsg
		}
		line += string(ansi.ReplacePipeCodes([]byte(help + scrollInfo)))
		terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
	}

	prevSelected := -1
	prevTop := -1

	for {
		clampSelection()

		if needFullRedraw || topIndex != prevTop {
			renderFull()
			needFullRedraw = false
		} else if selectedIndex != prevSelected {
			// Redraw old and new highlighted rows.
			if prevSelected >= 0 {
				renderItemRow(prevSelected)
			}
			renderItemRow(selectedIndex)
			renderHelpRow()
		}

		prevSelected = selectedIndex
		prevTop = topIndex

		keyInt, keyErr := ih.ReadKey()
		if keyErr != nil {
			if errors.Is(keyErr, editor.ErrIdleTimeout) {
				return nil, "LOGOFF", editor.ErrIdleTimeout
			}
			if errors.Is(keyErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", keyErr
		}

		statusMsg = ""

		switch keyInt {
		case editor.KeyArrowUp, editor.KeyCtrlE:
			selectedIndex--
		case editor.KeyArrowDown, editor.KeyCtrlX:
			selectedIndex++
		case editor.KeyPageUp, editor.KeyCtrlR:
			selectedIndex -= visibleRows
			topIndex -= visibleRows
		case editor.KeyPageDown, editor.KeyCtrlC:
			selectedIndex += visibleRows
			topIndex += visibleRows
		case editor.KeyHome:
			selectedIndex = 0
			topIndex = 0
		case editor.KeyEnd:
			selectedIndex = len(entries) - 1

		case ' ':
			// Toggle subscription.
			ent := &entries[selectedIndex]
			if ent.area.Tag == "(error)" {
				statusMsg = "|04Cannot subscribe to error entries.|07"
				needFullRedraw = true
				continue
			}

			if ent.subscribed {
				// Unsubscribe: remove leaf config.
				if err := v3netUnsubscribe(e.RootConfigPath, ent.network, ent.area.Tag); err != nil {
					statusMsg = fmt.Sprintf("|04Unsubscribe failed: %s|07", err)
				} else {
					ent.subscribed = false
					subSet[ent.network+":"+ent.area.Tag] = false
					statusMsg = fmt.Sprintf("|10Unsubscribed from %s. Restart to apply.|07", ent.area.Tag)
				}
			} else {
				// Subscribe: add leaf config + create message area.
				if err := v3netSubscribe(e.RootConfigPath, e.MessageMgr, ent.network, ent.hubURL, ent.area); err != nil {
					statusMsg = fmt.Sprintf("|04Subscribe failed: %s|07", err)
				} else {
					ent.subscribed = true
					subSet[ent.network+":"+ent.area.Tag] = true
					statusMsg = fmt.Sprintf("|10Subscribed to %s. Restart to activate.|07", ent.area.Tag)
				}
			}
			needFullRedraw = true

		case 'q', 'Q', editor.KeyEsc:
			return nil, "", nil
		}
	}
}

// v3netSubscribe adds a leaf config entry and auto-creates the message area.
func v3netSubscribe(configPath string, mgr *message.MessageManager, network, hubURL string, area protocol.Area) error {
	cfg, err := config.LoadV3NetConfig(configPath)
	if err != nil {
		return err
	}

	// Check if already subscribed.
	for _, l := range cfg.Leaves {
		if l.Network == network && containsBoard(l.Boards, area.Tag) {
			return nil
		}
	}

	// Append to existing leaf for this network, or create a new one.
	found := false
	for i, l := range cfg.Leaves {
		if l.Network == network {
			cfg.Leaves[i].Boards = append(cfg.Leaves[i].Boards, area.Tag)
			found = true
			break
		}
	}
	if !found {
		cfg.Leaves = append(cfg.Leaves, config.V3NetLeafConfig{
			HubURL:       hubURL,
			Network:      network,
			Boards:       []string{area.Tag},
			PollInterval: "5m",
		})
	}

	if err := config.SaveV3NetConfig(configPath, cfg); err != nil {
		return fmt.Errorf("save v3net.json: %w", err)
	}

	// Auto-create message area if missing.
	if _, ok := mgr.GetAreaByTag(area.Tag); !ok {
		// Infer conference ID from existing v3net areas in this network.
		confID := 0
		for _, a := range mgr.ListAreas() {
			if a.AreaType == "v3net" && a.Network == network && a.ConferenceID != 0 {
				confID = a.ConferenceID
				break
			}
		}

		_, err := mgr.AddArea(message.MessageArea{
			Tag:          area.Tag,
			Name:         area.Name,
			AreaType:     "v3net",
			Network:      network,
			EchoTag:      area.Tag,
			ConferenceID: confID,
			AutoJoin:     true,
			ACSRead:      "s10",
			ACSWrite:     "s20",
		})
		if err != nil {
			return fmt.Errorf("create area: %w", err)
		}
	}

	return nil
}

// v3netUnsubscribe removes a board tag from the leaf config entry for the given
// network. Removes the entire leaf entry if it has no remaining boards.
// Does not delete the message area.
func v3netUnsubscribe(configPath, network, tag string) error {
	cfg, err := config.LoadV3NetConfig(configPath)
	if err != nil {
		return err
	}

	for i, l := range cfg.Leaves {
		if l.Network == network {
			var filtered []string
			for _, b := range l.Boards {
				if b != tag {
					filtered = append(filtered, b)
				}
			}
			if len(filtered) == 0 {
				cfg.Leaves = append(cfg.Leaves[:i], cfg.Leaves[i+1:]...)
			} else {
				cfg.Leaves[i].Boards = filtered
			}
			break
		}
	}

	return config.SaveV3NetConfig(configPath, cfg)
}

// containsBoard reports whether boards contains the given tag.
func containsBoard(boards []string, tag string) bool {
	for _, b := range boards {
		if b == tag {
			return true
		}
	}
	return false
}

// v3netAreasShowMessage displays a single message with a pause prompt.
func v3netAreasShowMessage(e *MenuExecutor, terminal *term.Terminal, s ssh.Session, outputMode ansi.OutputMode, termWidth, termHeight int, msg string) (*user.User, string, error) {
	var buf strings.Builder
	buf.WriteString(ansi.ClearScreen())
	buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net Area Subscriptions|07\r\n")))
	buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("─", termWidth-1) + "|07\r\n\r\n")))
	buf.Write(ansi.ReplacePipeCodes([]byte("  " + msg + "\r\n\r\n")))
	terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight); err != nil {
		return nil, "", err
	}
	return nil, "", nil
}
