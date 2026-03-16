module github.com/ViSiON-3/vision-3-bbs

go 1.25.0

require (
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/creack/pty v1.1.24
	github.com/dop251/goja v0.0.0-20260311135729-065cd970411c
	github.com/fsnotify/fsnotify v1.9.0
	github.com/gliderlabs/ssh v0.3.8
	github.com/google/uuid v1.6.0
	golang.org/x/crypto v0.45.0
	golang.org/x/term v0.37.0
	golang.org/x/text v0.31.0
	modernc.org/sqlite v1.46.2
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/bitbored/go-ansicon v0.0.0-20150813185003-cc47149d371d // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

require (
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/robfig/cron/v3 v3.0.1
	golang.org/x/sys v0.42.0
)

// Patched copies of upstream packages to fix ENABLE_VIRTUAL_TERMINAL_INPUT
// failures on Windows 10 32-bit / pre-1709 builds where the console does not
// support VT input mode. Input still works via coninput (raw console events).
replace (
	github.com/charmbracelet/bubbletea v1.3.10 => ./third_party/charmbracelet-bubbletea
	github.com/charmbracelet/x/term => ./third_party/charmbracelet-x-term
)
