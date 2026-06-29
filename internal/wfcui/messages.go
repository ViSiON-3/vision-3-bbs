package wfcui

import "github.com/ViSiON-3/vision-3-bbs/internal/admin"

type snapshotMsg struct{ snap *admin.SystemSnapshot }
type eventMsg struct{ ev admin.Event }
type errMsg struct{ err error }
