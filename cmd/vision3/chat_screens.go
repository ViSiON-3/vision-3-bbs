package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
)

// boardChatScreens reports the chat screen split-screen chat opens with by
// default and the screens the menu set defines, for the WFC's snoop header.
// Nothing is reported before the menu executor exists.
func boardChatScreens() (string, []string) {
	e := menuExecutor
	if e == nil {
		return "", nil
	}
	return e.Theme().SysopChatScreen, menu.ChatScreens(e.Menus())
}

// runConvertChatNfo converts a Vision/2 CHATNFO.n layout to chat screen JSON
// on out, for `vision3 -convert-chatnfo`. It returns the exit status.
func runConvertChatNfo(path string, out, errOut io.Writer) int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(errOut, "vision3: %v\n", err) //nolint:errcheck
		return 2
	}
	defer f.Close() //nolint:errcheck
	data, err := menu.ConvertChatNfo(f)
	if err != nil {
		fmt.Fprintf(errOut, "vision3: %s: %v\n", path, err) //nolint:errcheck
		return 1
	}
	if _, err := out.Write(data); err != nil {
		fmt.Fprintf(errOut, "vision3: %v\n", err) //nolint:errcheck
		return 2
	}
	return 0
}
