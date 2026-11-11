package configeditor

import (
	"strconv"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// sysFieldsToasts returns fields for the Toasts sub-screen.
func sysFieldsToasts(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Toast Logons", Help: "Announce logons to other online callers", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.ToastLogons) },
			Set: func(val string) error {
				cfg.ToastLogons = uitext.YNToBool(val)
				return nil
			},
		},
		{
			Label: "Toast Logoffs", Help: "Announce logoffs to other online callers", Type: ftYesNo, Col: 3, Row: 2, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.ToastLogoffs) },
			Set: func(val string) error {
				cfg.ToastLogoffs = uitext.YNToBool(val)
				return nil
			},
		},
		{
			Label: "Toast Posts", Help: "Announce new posts to other online callers who may read the area; N turns it off for everyone", Type: ftYesNo, Col: 3, Row: 3, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.ToastPosts) },
			Set: func(val string) error {
				cfg.ToastPosts = uitext.YNToBool(val)
				return nil
			},
		},
		{
			Label: "Toast Level", Help: "Minimum level to see logon, logoff and post announcements", Type: ftInteger, Col: 3, Row: 4, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.ToastAnnounceLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ToastAnnounceLevel = n
				return nil
			},
		},
		{
			Label: "Toast Edge", Help: "Screen edge a toast is placed from", Type: ftLookup, Col: 3, Row: 5, Width: 6,
			Get: func() string {
				if cfg.ToastEdge == config.ToastEdgeTop {
					return config.ToastEdgeTop
				}
				return config.ToastEdgeBottom
			},
			Set: func(val string) error { cfg.ToastEdge = val; return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: config.ToastEdgeBottom, Display: "Bottom"},
					{Value: config.ToastEdgeTop, Display: "Top"},
				}
			},
		},
		{
			Label: "Toast Offset", Help: "Rows in from that edge, towards the middle of the screen", Type: ftInteger, Col: 3, Row: 6, Width: 2, Min: 0, Max: 50,
			Get: func() string { return strconv.Itoa(cfg.ToastOffset) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ToastOffset = n
				return nil
			},
		},
		{
			Label: "Toast Center", Help: "Center on the caller's Konfig screen width; Toast Col is then ignored", Type: ftYesNo, Col: 3, Row: 7, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.ToastCenter) },
			Set: func(val string) error {
				cfg.ToastCenter = uitext.YNToBool(val)
				return nil
			},
		},
		{
			Label: "Toast Col", Help: "Column a toast starts at", Type: ftInteger, Col: 3, Row: 8, Width: 3, Min: 1, Max: 255,
			Get: func() string { return strconv.Itoa(max(1, cfg.ToastCol)) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ToastCol = n
				return nil
			},
		},
		{
			Label: "Toast Fade", Help: "Fade toasts in and out through the theme's Toast Fade Colors; N shows them at once, as before", Type: ftYesNo, Col: 3, Row: 9, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.ToastFade) },
			Set: func(val string) error {
				cfg.ToastFade = uitext.YNToBool(val)
				return nil
			},
		},
		{
			Label: "Fade Step ms", Help: "How long each fade colour shows, in milliseconds", Type: ftInteger, Col: 3, Row: 10, Width: 3,
			Min: config.MinToastFadeMs, Max: config.MaxToastFadeMs,
			Get: func() string { return strconv.Itoa(int(cfg.ToastFadeStep() / time.Millisecond)) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ToastFadeMs = n
				return nil
			},
		},
	}
}
