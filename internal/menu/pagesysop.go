package menu

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// SysopPager is the WFC side of PAGESYSOP; *admin.Server satisfies it.
type SysopPager interface {
	Consoles() int
	RaisePage(nodeID int, handle, reason string)
	ClearPage(nodeID int, handle, why string)
}

// pageCooldowns maps a lower-cased handle to the time of its last page.
var pageCooldowns sync.Map

// pageCountdownFrame rings the bell and redraws the seconds left in place.
// Every output mode is an ANSI terminal, so cursor save and restore apply to
// all of them.
func pageCountdownFrame(left int) []byte {
	return []byte(fmt.Sprintf("\a\x1b[s%3d\x1b[u", left))
}

func runPageSysop(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	if c.currentUser == nil {
		return nil, "", nil
	}
	handle := c.currentUser.Handle
	key := strings.ToLower(handle)
	write := func(s string) {
		terminalio.WriteProcessedBytes(c.terminal, ansi.ReplacePipeCodes([]byte(s)), c.outputMode)
	}

	if e.Pager == nil || e.Pager.Consoles() == 0 {
		write(e.Strings().PageSysopUnavailable)
		return nil, "", nil
	}

	cfg := e.GetServerConfig()
	cooldown := time.Duration(cfg.PageSysopCooldownSeconds) * time.Second
	if v, ok := pageCooldowns.Load(key); ok && cooldown > 0 {
		if left := cooldown - time.Since(v.(time.Time)); left > 0 {
			mins := int((left + time.Minute - 1) / time.Minute)
			write(fmt.Sprintf(e.Strings().PageSysopCooldown, mins))
			return nil, "", nil
		}
	}

	write(e.Strings().PageSysopReasonPrompt)
	reason, err := readLineFromSessionIH(c.s, c.terminal)
	if err != nil {
		if err == io.EOF {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, "", nil
	}

	pageCooldowns.Store(key, time.Now())
	e.Pager.RaisePage(c.nodeNumber, handle, reason)
	write(e.Strings().PageSysopPaging)

	ih := getSessionIH(c.s)
	tap := tapOf(c.s)
	var chats uint64
	if tap != nil {
		chats = tap.Chats()
	}
	for i := cfg.PageSysopTimeoutSeconds; i > 0; i-- {
		_ = terminalio.WriteProcessedBytes(c.terminal, pageCountdownFrame(i), c.outputMode)
		_, err := ih.ReadKeyWithTimeout(time.Second)
		if tap != nil {
			// The chat hook already cleared the page.
			if n := tap.Chats(); n != chats {
				return nil, "", nil
			}
		}
		switch {
		case err == nil:
			e.Pager.ClearPage(c.nodeNumber, handle, "cancelled")
			return nil, "", nil
		case errors.Is(err, editor.ErrIdleTimeout) && !errors.Is(err, editor.ErrTimeLimit):
		default:
			e.Pager.ClearPage(c.nodeNumber, handle, "cancelled")
			return nil, "", err
		}
	}

	write(e.Strings().PageSysopUnavailable)
	e.Pager.ClearPage(c.nodeNumber, handle, "timeout")
	return nil, "", nil
}
