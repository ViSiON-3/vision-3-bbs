package menu

import (
	"fmt"
	"io"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// showFileInfo displays the "i" info overlay for the selected file and
// blocks for a keypress to dismiss it. It follows the same signal
// convention as handleNavKey/action handlers: refresh tells run() to set
// needFullRedraw; exit tells run() to return (result, action, err)
// immediately (used for the LOGOFF/error paths out of the blocking
// ReadKey call).
func (lb *fileLightbar) showFileInfo() (refresh bool, exit bool, result *user.User, action string, err error) {
	if len(lb.allFiles) == 0 {
		return false, false, nil, "", nil
	}
	sel := lb.allFiles[lb.selectedIndex]
	descLines := formatDIZLines(sel.Description, dizMaxWidth, dizMaxLines)

	_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.ClearScreen()), lb.outputMode)

	d1 := fmt.Sprintf("|15Filename  : |07%s\r\n", sel.Filename)
	d2 := fmt.Sprintf("|15Size      : |07%s\r\n", compactFileSize(sel.Size))
	_ = lb.writePipe(d1)
	_ = lb.writePipe(d2)

	for i, dl := range descLines {
		var dLine string
		if i == 0 {
			dLine = fmt.Sprintf("|15Desc      : |07%s\r\n", dl)
		} else {
			dLine = fmt.Sprintf("|07            %s\r\n", dl)
		}
		_ = lb.writePipe(dLine)
	}
	if len(descLines) == 0 {
		_ = lb.writePipe("|15Desc      : |07(none)\r\n")
	}

	d3 := fmt.Sprintf("|15Uploaded  : |07%s\r\n", sel.UploadedAt.Format("01/02/2006 15:04"))
	d4 := fmt.Sprintf("|15Uploader  : |07%s\r\n", sel.UploadedBy)
	d5 := fmt.Sprintf("|15Downloads : |07%d\r\n", sel.DownloadCount)
	_ = lb.writePipe(d3)
	_ = lb.writePipe(d4)
	_ = lb.writePipe(d5)

	_ = lb.writePipe("\r\n|08Press any key to return...|07")
	if _, waitErr := lb.ih.ReadKey(); waitErr != nil {
		if logoffIfDisconnected(waitErr) {
			return false, true, nil, "LOGOFF", io.EOF
		}
		return false, true, nil, "", waitErr
	}
	return true, false, nil, "", nil
}
