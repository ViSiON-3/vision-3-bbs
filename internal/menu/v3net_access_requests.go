package menu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// v3netAccessRow is one pending subscription request for an area this node
// manages.
type v3netAccessRow struct {
	network string
	tag     string
	req     protocol.AccessRequest
}

// runV3NetAccessRequests is the area manager's queue: every pending
// subscription request for areas whose manager is this node, across all
// subscribed networks. A = approve, D = deny (the hub adds denied nodes to
// the area's deny list), Q = quit. The list is refetched after each action.
func runV3NetAccessRequests(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}
	svc := e.V3NetStatus
	status := ""

	for {
		ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
		_, managed, errs := v3netRoles(ctx, svc)
		var rows []v3netAccessRow
		for _, m := range managed {
			reqs, err := svc.ListAccessRequests(ctx, m.network, m.area.Tag)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s/%s: %s", m.network, m.area.Tag, err))
				continue
			}
			for _, r := range reqs {
				rows = append(rows, v3netAccessRow{network: m.network, tag: m.area.Tag, req: r})
			}
		}
		cancel()

		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net: Area Access Requests|07\r\n" + v3netRule(termWidth))))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  #  NETWORK     AREA TAG          BBS NAME              NODE ID   REQUESTED|07\r\n")))
		now := time.Now()
		for i, r := range rows {
			line := fmt.Sprintf("|15%3d|07  %-10s  %-16s  %-20s  %-8s  %s",
				i+1, truncateStr(r.network, 10), truncateStr(r.tag, 16),
				truncateStr(r.req.BBSName, 20), truncateStr(r.req.NodeID, 8), v3netAgo(r.req.RequestedAt, now))
			buf.Write(ansi.ReplacePipeCodes([]byte(line + "\r\n")))
		}
		switch {
		case len(managed) == 0 && len(errs) == 0:
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  This node does not manage any areas.|07\r\n")))
		case len(rows) == 0:
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|08  No pending access requests for the %d area(s) you manage.|07\r\n", len(managed)))))
		}
		for _, msg := range errs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + truncateStr(msg, termWidth-4) + "|07\r\n")))
		}
		buf.WriteString("\r\n")
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		if len(rows) == 0 {
			return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
		}

		line, next, err := v3netPromptLine(s, terminal, outputMode,
			"|08  [|15A|08]pprove #  [|15D|08]eny #  [|15Q|08]uit: |07")
		if err != nil {
			return nil, next, err
		}
		if line == "" || strings.EqualFold(line, "Q") {
			return nil, "", nil
		}
		action, n, ok := parseListCommand(line)
		if !ok || n > len(rows) || (action != 'A' && action != 'D') {
			status = "|04Enter A or D followed by a row number, or Q.|07"
			continue
		}
		row := rows[n-1]
		ctx, cancel = context.WithTimeout(context.Background(), v3netManageTimeout)
		switch action {
		case 'A':
			err = svc.ApproveAccess(ctx, row.network, row.tag, []string{row.req.NodeID})
			if err == nil {
				status = fmt.Sprintf("|10Approved %s for %s.|07", row.req.BBSName, row.tag)
			}
		case 'D':
			reason, next, perr := v3netPromptLine(s, terminal, outputMode, "|07  Reason (optional): ")
			if perr != nil {
				cancel()
				return nil, next, perr
			}
			err = svc.DenyAccess(ctx, row.network, row.tag, []string{row.req.NodeID}, reason)
			if err == nil {
				status = fmt.Sprintf("|14Denied %s for %s and added it to the deny list.|07", row.req.BBSName, row.tag)
			}
		}
		cancel()
		if err != nil {
			status = "|04" + truncateStr(err.Error(), termWidth-4) + "|07"
		}
	}
}
