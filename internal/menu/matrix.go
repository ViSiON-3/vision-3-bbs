package menu

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/pace"
	"github.com/ViSiON-3/vision-3-bbs/internal/sixel"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// RunMatrixScreen displays the pre-login matrix menu and returns the selected action.
// Actions: "LOGIN", "NEWUSER", "CHECKACCESS", "DISCONNECT"
// Called from main.go sessionHandler before the login loop for telnet users.
// Uses standard .BAR/.CFG menu files (PDMATRIX.BAR, PDMATRIX.CFG) for configuration.
func (e *MenuExecutor) RunMatrixScreen(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth int,
	termHeight int,
) (string, *user.User, error) {
	const menuName = "PDMATRIX"
	// A terminal whose answer to the connect probe is late gets it applied
	// here, before the first draw if it is in by then and with one repaint
	// if it comes while the matrix waits for a key. After the matrix the
	// encoding stays as it is.
	late := lateProbe(s)
	defer late.settle()
	// Sampled once: an answer that completes after this, even during the
	// setup below, is applied by the repaint in the key loop.
	awaitLate := late != nil && late.waiting()
	if late != nil && !awaitLate {
		outputMode = late.adoptEncoding(outputMode)
	}
	// The matrix's files and wording are the board's, whatever its theme
	// (preloginView); what follows it (PRELOGON, login, the new-user form)
	// is drawn by e.
	pre := e.preloginView()

	// Load lightbar options from PDMATRIX.BAR
	options, err := loadLightbarOptions(menuName, pre)
	if err != nil {
		slog.Warn("failed to load BAR file, skipping matrix", "node", nodeNumber, "menu", menuName, "error", err)
		return "LOGIN", nil, nil
	}
	if len(options) == 0 {
		slog.Warn("no options in BAR file, skipping matrix", "node", nodeNumber, "menu", menuName)
		return "LOGIN", nil, nil
	}

	// Load commands from PDMATRIX.CFG to map hotkeys to actions
	commands, err := LoadCommands(menuName, pre.Menus())
	if err != nil {
		slog.Warn("failed to load CFG file, skipping matrix", "node", nodeNumber, "menu", menuName, "error", err)
		return "LOGIN", nil, nil
	}

	// Build hotkey → command map
	commandMap := make(map[string]string)
	for _, cmd := range commands {
		commandMap[strings.ToUpper(cmd.Keys)] = strings.ToUpper(cmd.Command)
	}

	// Load the ANSI background (convention: PDMATRIX.ANS)
	// Use GetAnsiFileContent to automatically strip SAUCE metadata
	ansPath := pre.menuFile("ansi", menuName+".ANS")
	ansArt, err := ansi.GetAnsiFileContent(ansPath)
	if err != nil {
		slog.Warn("failed to load ANS file, skipping matrix", "node", nodeNumber, "menu", menuName, "error", err)
		return "LOGIN", nil, nil
	}
	var ansBackground, graphic []byte
	var graphicCol int
	var scene *matrixScene
	var drawnSixel, drawnAudio bool // the answers the screen was prepared for
	// prepare fits the art to the encoding and loads the picture and scene
	// the terminal's answers allow.
	prepare := func() {
		drawnSixel, drawnAudio = sessionSixel(s, nil), sessionAudio(s)
		ansBackground = fitArt(terminal, ansi.ArtForOutput(ansArt, outputMode), termWidth, outputMode == ansi.OutputModeUTF8)
		var graphicSize image.Point
		graphic, graphicCol, graphicSize = pre.matrixGraphic(s, termWidth, termHeight)
		scene = nil
		if len(graphic) > 0 {
			scene = pre.newMatrixScene(termWidth, termHeight, graphicCol, nodeNumber)
		}
		// Without a scene the picture is drawn above the choices, where only
		// 640x160 fits; the larger side-layout picture would cover them.
		if scene == nil && (graphicSize.X > 640 || graphicSize.Y > 160) {
			graphic = nil
		}
	}
	prepare()
	animationStart := time.Now()

	slog.Info("displaying pre-login matrix screen", "node", nodeNumber, "count", len(options))

	// Ensure cursor is restored when we exit the matrix screen
	defer func() {
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)
	}()

	selectedIndex := 0
	maxTries := 10
	tries := 0

	// Picture frames go to the terminal one at a time, each once it has
	// answered for the last, so a terminal that reads slowly gets fewer
	// frames and the loop goes on reading keys (see pace.Gate).
	frames := frameGateFor(s)

	// The optional pre-login effect plays only once, before the first draw.
	if scene != nil && scene.config.CRTOn {
		if err := drawMatrixCRTOn(terminal, termWidth, termHeight); err != nil {
			return "DISCONNECT", nil, err
		}
		if err := drawMatrixCRTWarp(terminal, scene, options, selectedIndex, frames); err != nil {
			return "DISCONNECT", nil, err
		}
	}

	// repaint says the screen was drawn without its picture, which the next
	// frame draws whole.
	repaint := false
	// picture sends the next picture frame if the terminal has answered for
	// the last: the whole picture after a repaint, else the scene's next
	// frame. It sends nothing while an answer is due; the scene then keeps
	// the last frame sent as the one to draw the next from.
	picture := func() error {
		if scene == nil && (!repaint || len(graphic) == 0) {
			repaint = false
			return nil
		}
		if !frames.Begin() {
			return nil
		}
		var err error
		if repaint {
			repaint = false
			if scene != nil && scene.config.ProjectText {
				// The scene fills the screen and carries the choices on
				// its cube: the screen is cleared only as the frame that
				// covers it goes.
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()+"\x1b[?25l"), outputMode)
			} else {
				if err = drawMatrixHeaderGraphic(terminal, graphic, graphicCol, outputMode); err == nil {
					err = drawMatrixOptions(terminal, options, selectedIndex, outputMode)
				}
			}
			if scene != nil {
				scene.reset()
			}
		}
		if err == nil && scene != nil {
			err = scene.draw(terminal, time.Since(animationStart).Seconds(), options, selectedIndex, outputMode)
		}
		if _, qerr := terminal.Write(frames.Query()); err == nil {
			err = qerr
		}
		return err
	}

	// Draw the initial screen
	drawScreen := func() error {
		// The projectText scene has no text screen: the repaint is the
		// picture frame, which clears the screen as it goes (see picture),
		// so nothing is drawn while the terminal owes an answer.
		if scene == nil || !scene.config.ProjectText {
			if err := drawMatrixScreenGraphic(terminal, ansBackground, options, selectedIndex, outputMode); err != nil {
				return err
			}
		}
		repaint = true
		return picture()
	}
	if err := drawScreen(); err != nil {
		slog.Error("failed to draw matrix screen", "node", nodeNumber, "error", err)
		return "LOGIN", nil, nil
	}
	// Loaded after the first draw, so the screen does not wait for them.
	tones := pre.loadMatrixTones(s, terminal, nodeNumber)

	// Apply the pre-login idle timeout on the shared InputHandler.
	// nil = no authenticated user yet; sysop exemption applies post-login only.
	getSessionIH(s).SetSessionIdleTimeout(e.idleTimeout(nil))

	sessionIH := getSessionIH(s)
	idleStart := time.Now()
	for tries < maxTries {
		var key int
		var err error
		if scene == nil && !awaitLate && !repaint {
			key, err = sessionIH.ReadKey()
		} else {
			key, err = sessionIH.ReadKeyWithPoll(50*time.Millisecond, idleStart)
		}
		if awaitLate && !late.waiting() {
			awaitLate = false
			// The whole answer is in, or the wait for it is over: if it
			// changed the encoding, sixel or audio, draw what it allows, in
			// its encoding, before acting on a key typed behind it.
			mode := late.adoptEncoding(outputMode)
			if mode != outputMode || sessionSixel(s, nil) != drawnSixel || sessionAudio(s) != drawnAudio {
				outputMode = mode
				prepare()
				slog.Info("repainting matrix for a late terminal answer", "node", nodeNumber, "sixel", len(graphic) > 0, "audio", sessionAudio(s))
				_ = drawScreen() // best-effort redraw
				if tones == nil {
					tones = pre.loadMatrixTones(s, terminal, nodeNumber)
				}
			}
		}
		if errors.Is(err, editor.ErrReadPoll) {
			if err := picture(); err != nil {
				return "DISCONNECT", nil, err
			}
			continue
		}
		if err != nil {
			if errors.Is(err, editor.ErrIdleTimeout) {
				e.handleIdleTimeout(terminal, outputMode, nodeNumber, termWidth, termHeight)
				return "DISCONNECT", nil, nil
			}
			if errors.Is(err, io.EOF) {
				return "DISCONNECT", nil, io.EOF
			}
			return "DISCONNECT", nil, fmt.Errorf("failed reading matrix input: %w", err)
		}
		idleStart = time.Now()

		newIndex := selectedIndex
		selectionMade := false

		switch key {
		case editor.KeyArrowUp:
			newIndex = selectedIndex - 1
			if newIndex < 0 {
				newIndex = len(options) - 1 // Wrap to bottom
			}

		case editor.KeyArrowDown:
			newIndex = selectedIndex + 1
			if newIndex >= len(options) {
				newIndex = 0 // Wrap to top
			}

		case editor.KeyEnter:
			selectionMade = true

		case editor.KeyEsc:
			// Bare ESC (sequences already decoded by ReadKey) — ignore

		case ' ':
			// Spacebar redraws screen (matches Pascal behavior)
			_ = drawScreen() // best-effort redraw

		default:
			if key < 32 || key > 126 {
				continue // ignore non-printable / special keys
			}
			// The move has to go through newIndex: the shared "did the
			// highlight move?" block below copies newIndex back over
			// selectedIndex, so assigning selectedIndex here would be
			// undone and every hotkey press would act on whatever was
			// already highlighted instead of what was typed.
			if idx, ok := matrixPrintableKey(rune(key), options); ok {
				newIndex = idx
				selectionMade = true
			} else {
				e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
				_ = drawScreen() // best-effort redraw
			}
		}

		if newIndex != selectedIndex {
			selectedIndex = newIndex
			if scene != nil && scene.config.ProjectText {
				scene.selectionActive = true
				scene.selectionChange = time.Since(animationStart).Seconds()
			}
			if scene != nil && scene.config.ProjectText {
				// The choices are on the cube: the move shows in the next
				// frame, now if the terminal has answered for the last,
				// else at the first poll after its answer. Nothing is
				// written over the picture meanwhile.
				_ = picture()
			} else if scene != nil && scene.config.TextFade {
				_ = drawMatrixFadedOptions(terminal, options, selectedIndex, time.Since(animationStart).Seconds(), outputMode)
			} else {
				_ = drawMatrixOptions(terminal, options, selectedIndex, outputMode) // best-effort redraw
			}
			if !selectionMade {
				tones.play(matrixToneMove)
			}
		}

		if selectionMade {
			// Look up the command for this option's hotkey
			hotkey := options[selectedIndex].HotKey
			action, ok := commandMap[hotkey]
			if !ok {
				slog.Warn("no command mapped for hotkey", "node", nodeNumber, "hotkey", hotkey)
				continue
			}
			tones.play(matrixToneChoose)
			if scene != nil && scene.config.ProjectText {
				// Briefly turn the selected face toward the caller and flare it.
				for frame := 1; frame <= 6; frame++ {
					scene.activation = float64(frame) / 6
					if err := picture(); err != nil {
						break
					}
					time.Sleep(45 * time.Millisecond)
				}
				scene.activation = 0
			}
			slog.Info("matrix selection", "node", nodeNumber, "text", options[selectedIndex].Text, "action", action)

			result, authed, err := e.processMatrixAction(pre, action, s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
			if err != nil {
				return result, authed, err
			}
			// A completed signup can hand back an account to continue as, so
			// the caller is not sent round to type the credentials they just
			// chose.
			if result == "LOGIN" || result == "DISCONNECT" || authed != nil {
				return result, authed, nil
			}

			// For actions that return to the matrix (like NEWUSER, CHECKACCESS),
			// redraw the screen and continue
			tries++
			selectedIndex = 0
			_ = drawScreen() // best-effort redraw
		}
	}

	// Max tries exceeded
	slog.Info("matrix max tries exceeded, disconnecting", "node", nodeNumber)
	return "DISCONNECT", nil, nil
}

// matrixPrintableKey resolves a printable keypress against the matrix options:
// 1-9 pick by position, anything else matches the HotKey column of the .BAR
// file (case-insensitively). It returns the option index to select and false
// if the key matches nothing, in which case the caller reports undefined
// input.
func matrixPrintableKey(r rune, options []LightbarOption) (int, bool) {
	if r >= '1' && r <= '9' {
		idx := int(r - '1')
		if idx < len(options) {
			return idx, true
		}
		return 0, false
	}

	keyStr := strings.ToUpper(string(r))
	for i, opt := range options {
		if keyStr == opt.HotKey {
			return i, true
		}
	}
	return 0, false
}

// processMatrixAction handles the selected matrix menu action. Check
// access and the goodbye are part of the matrix and are worded by pre, its
// pre-login view; login and the new-user form are e's.
func (e *MenuExecutor) processMatrixAction(
	pre *MenuExecutor,
	action string,
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth int,
	termHeight int,
) (string, *user.User, error) {
	switch action {
	case "LOGIN":
		// Show PRELOGON ANSI file before login screen (matches Pascal: Printfile(PRELOGON.x) + HoldScreen)
		e.showPrelogon(s, terminal, nodeNumber, outputMode, termWidth, termHeight)
		return "LOGIN", nil, nil

	case "NEWUSER":
		// Clear screen immediately when transitioning from matrix to new user flow
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode) // Show cursor
		newUser, err := e.handleNewUserApplication(s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "DISCONNECT", nil, io.EOF
			}
			slog.Error("new user application error from matrix", "node", nodeNumber, "error", err)
		}
		if started := e.continueAsNewUser(s, userManager, newUser, nodeNumber); started != nil {
			return "LOGIN", started, nil
		}
		return "MATRIX", nil, nil // Could not continue: back to the matrix

	case "CHECKACCESS":
		pre.handleCheckAccess(s, terminal, userManager, nodeNumber, outputMode)
		return "MATRIX", nil, nil // Return to matrix after check

	case "DISCONNECT":
		pre.writeString(terminal, outputMode, pre.Strings().MatrixDisconnecting)
		return "DISCONNECT", nil, nil

	default:
		slog.Warn("unknown matrix action", "node", nodeNumber, "action", action)
		e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
		return "MATRIX", nil, nil
	}
}

// handleCheckAccess prompts for a handle and shows their validation status.
func (e *MenuExecutor) handleCheckAccess(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	nodeNumber int,
	outputMode ansi.OutputMode,
) {
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	e.writeString(terminal, outputMode, e.Strings().MatrixCheckAccessPrompt)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return
	}

	handle := strings.TrimSpace(input)
	if handle == "" {
		return
	}

	foundUser, exists := userManager.GetUser(handle)

	if !exists {
		e.writeString(terminal, outputMode, e.Strings().MatrixUserNotFound)
	} else if cfg := e.GetServerConfig(); canLogonAtLevel(cfg, foundUser.AccessLevel) {
		// This option answers "check your access", so it reports whether the
		// caller can get on. It used to branch on the validated flag, which
		// decides nothing about login, and so told people waiting for approval
		// they did not need.
		e.writeStringf(terminal, outputMode, e.Strings().MatrixAccountValidated, foundUser.Handle, foundUser.AccessLevel)
	} else {
		cfg := e.GetServerConfig()
		e.writeStringf(terminal, outputMode, e.Strings().MatrixAccountCannotLogon,
			foundUser.Handle, foundUser.AccessLevel, cfg.LogonLevel)

		// If NUV is enabled, show voting progress for this candidate.
		if cfg.UseNUV {
			nuvMu.Lock()
			nd, err := loadNUVData(e.dataDir())
			nuvMu.Unlock()
			if err == nil {
				for _, c := range nd.Candidates {
					if strings.EqualFold(c.Handle, foundUser.Handle) {
						yes := nuvYesCount(&c)
						no := len(c.Votes) - yes
						nuvSuffix := "yes needed to reach threshold"
						if cfg.NUVValidate {
							nuvSuffix = "yes needed to validate"
						}
						nuvMsg := fmt.Sprintf("\r\n|07Your application is in the voting queue.\r\n"+
							"|07Votes so far: |10%d Yes|07, |12%d No|07  |08(|10%d|08 %s)\r\n",
							yes, no, cfg.NUVYesVotes, nuvSuffix)
						terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(nuvMsg)), outputMode)
						break
					}
				}
			}
		}
	}

	// Pause
	_ = e.pauseHere(s, terminal, outputMode)
}

// showPrelogon displays a random PRELOGON ANSI file before the login screen.
// Matches Pascal: Printfile(PRELOGON.x) + HoldScreen where x is random 1..NumPrelogon.
// Looks for numbered files (PRELOGON.1, PRELOGON.2, ...) first, falls back to PRELOGON.ANS.
func (e *MenuExecutor) showPrelogon(s ssh.Session, terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode, termWidth, termHeight int) {
	menus := e.Menus()

	// Look for numbered PRELOGON files (Pascal pattern: PRELOGON.1, PRELOGON.2, ...)
	// Each number resolves on its own, so an overlay can replace PRELOGON.2
	// and leave PRELOGON.1 to the shipped set.
	var candidates []string
	for i := 1; i <= 20; i++ {
		path, _, ok, err := menus.Locate("ansi", fmt.Sprintf("PRELOGON.%d", i))
		if err != nil {
			slog.Warn("resolving prelogon file", "error", err)
			return
		}
		if !ok {
			break // Stop at first gap
		}
		candidates = append(candidates, path)
	}

	// Fall back to single PRELOGON.ANS
	if len(candidates) == 0 {
		path, _, ok, err := menus.Locate("ansi", "PRELOGON.ANS")
		if err != nil {
			slog.Warn("resolving prelogon file", "error", err)
			return
		}
		if ok {
			candidates = append(candidates, path)
		}
	}

	if len(candidates) == 0 {
		return // No PRELOGON files found
	}

	// Pick a random file
	idx := 0
	if len(candidates) > 1 {
		idx = int(time.Now().UnixNano() % int64(len(candidates)))
	}

	// Use GetAnsiFileContent to automatically strip SAUCE metadata
	rawContent, err := ansi.GetAnsiFileContent(candidates[idx])
	if err != nil {
		slog.Warn("failed to read prelogon file", "node", nodeNumber, "file", candidates[idx], "error", err)
		return
	}

	slog.Info("displaying prelogon screen", "node", nodeNumber, "file", filepath.Base(candidates[idx]))
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	_ = writeArt(terminal, rawContent, outputMode, termWidth) // best-effort display

	// HoldScreen — pause before proceeding to login
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
}

// matrixGraphic loads optional overlay-aware PDMATRIX.PNG only for a
// terminal that said at connect it draws sixel. The size checks happen before decoding untrusted dimensions.
// It returns the sixel data, the column to draw it at and its size in pixels.
func (e *MenuExecutor) matrixGraphic(s ssh.Session, width, height int) ([]byte, int, image.Point) {
	if !sessionSixel(s, nil) || width < 80 || height < 24 {
		return nil, 0, image.Point{}
	}
	path, _, ok, err := e.Menus().Locate("ansi", "PDMATRIX.PNG")
	if err != nil || !ok {
		return nil, 0, image.Point{}
	}
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("cannot open matrix graphic", "error", err)
		return nil, 0, image.Point{}
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		slog.Warn("invalid matrix graphic size", "error", err)
		return nil, 0, image.Point{}
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	// Reserve the lower half of an 80x24 screen for the menu choices.
	maxW, maxH := 640, 160
	if efx, ok := e.matrixEffects(); ok {
		maxW, maxH = efx.pictureLimit()
	}
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxW || cfg.Height > maxH || cfg.Width > width*8 {
		slog.Warn("invalid matrix graphic dimensions", "width", cfg.Width, "height", cfg.Height, "error", err)
		return nil, 0, image.Point{}
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		slog.Warn("invalid matrix graphic", "error", err)
		return nil, 0, image.Point{}
	}
	return sixel.Encode(img), (width-(cfg.Width+7)/8)/2 + 1, img.Bounds().Size()
}

// drawMatrixScreenGraphic draws the text screen: the background and the
// choices. The picture is drawn over it as a frame (drawMatrixHeaderGraphic).
func drawMatrixScreenGraphic(terminal *term.Terminal, ansBackground []byte, options []LightbarOption, selectedIndex int, outputMode ansi.OutputMode) error {
	// Clear screen and draw background
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(ansBackground) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, ansBackground, outputMode)
	}

	// Draw options with highlighting
	return drawMatrixOptions(terminal, options, selectedIndex, outputMode)
}

// drawMatrixHeaderGraphic draws the picture in the header rows.
func drawMatrixHeaderGraphic(terminal *term.Terminal, graphic []byte, graphicCol int, outputMode ansi.OutputMode) error {
	// The text fallback may be wider than the picture. Clear the entire
	// reserved header, not just the graphic's rectangle, before drawing it.
	for row := 1; row <= 10; row++ {
		terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H\x1b[2K", row)), outputMode)
	}
	terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[1;%dH", graphicCol)), outputMode)
	_, err := terminal.Write(graphic)
	return err
}

// drawMatrixOptions redraws the menu option text with the current selection highlighted.
// Uses DOS color codes from LightbarOption (via colorCodeToAnsi) for rendering.
func drawMatrixOptions(
	terminal *term.Terminal,
	options []LightbarOption,
	selectedIndex int,
	outputMode ansi.OutputMode,
) error {
	for i, opt := range options {
		// Position cursor at this option
		posCmd := fmt.Sprintf("\x1b[%d;%dH", opt.Y, opt.X)
		terminalio.WriteProcessedBytes(terminal, []byte(posCmd), outputMode)

		// Apply color based on selection (DOS color code → ANSI escape)
		var colorCode int
		if i == selectedIndex {
			colorCode = opt.HighlightColor
		} else {
			colorCode = opt.RegularColor
		}
		terminalio.WriteProcessedBytes(terminal, []byte(colorCodeToAnsi(colorCode)), outputMode)

		// Write the option text
		terminalio.WriteProcessedBytes(terminal, []byte(opt.Text), outputMode)

		// Reset attributes
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
	}

	// Hide cursor after drawing
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	return nil
}

// drawMatrixCRTOn evokes the narrow flash of a CRT warming up. It uses only
// standard cursor positioning and runs once, before the matrix is painted.
func drawMatrixCRTOn(t *term.Terminal, width, height int) error {
	row, col := (height+1)/2, (width+1)/2
	frames := []struct {
		half, brightness int
		delay            time.Duration
	}{
		{0, 255, 65 * time.Millisecond},
		{5, 255, 55 * time.Millisecond},
		{17, 230, 55 * time.Millisecond},
		{min(36, (width-2)/2), 195, 55 * time.Millisecond},
	}
	for _, f := range frames {
		line := strings.Repeat(" ", f.half*2+1)
		seq := fmt.Sprintf("\x1b[?25l\x1b[0m\x1b[2J\x1b[%d;%dH\x1b[48;2;%d;%d;%dm%s\x1b[0m", row, col-f.half, f.brightness, f.brightness, f.brightness, line)
		if _, err := t.Write([]byte(seq)); err != nil {
			return err
		}
		time.Sleep(f.delay)
	}
	// A faint, wider raster bloom bridges the scan line and the full screen.
	for _, offset := range []int{-2, -1, 0, 1, 2} {
		level := 32
		switch offset {
		case 0:
			level = 110
		case -1, 1:
			level = 55
		}
		seq := fmt.Sprintf("\x1b[%d;2H\x1b[48;2;%d;%d;%dm%s\x1b[0m", row+offset, level, level, level, strings.Repeat(" ", width-2))
		if _, err := t.Write([]byte(seq)); err != nil {
			return err
		}
	}
	time.Sleep(65 * time.Millisecond)
	return nil
}

// drawMatrixCRTWarp expands the actual matrix artwork from the centre of the
// raster, stretching the picture vertically as a CRT's scan opens up. A
// frame the terminal is not ready for (see pace.Gate) is skipped.
func drawMatrixCRTWarp(t *term.Terminal, scene *matrixScene, options []LightbarOption, selected int, frames *pace.Gate) error {
	const w, h = 640, 384
	artwork := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(artwork, artwork.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	left := (scene.col - 1) * 8
	draw.Draw(artwork, image.Rect(left, 0, left+scene.source.Bounds().Dx(), scene.source.Bounds().Dy()), scene.source, image.Point{}, draw.Src)
	cube := matrixTextCube(0, options, selected)
	for y := 0; y < cube.Bounds().Dy(); y++ {
		for x := 0; x < cube.Bounds().Dx(); x++ {
			c := cube.NRGBAAt(x, y)
			if c.R != 0 || c.G != 0 || c.B != 0 {
				artwork.SetNRGBA(160+x, 128+y, c)
			}
		}
	}
	scales := [][2]float64{{.10, .025}, {.28, .06}, {.48, .16}, {.70, .38}, {.88, .70}, {1, 1}}
	for _, scale := range scales {
		if !frames.Begin() {
			time.Sleep(45 * time.Millisecond)
			continue
		}
		frame := image.NewNRGBA(artwork.Bounds())
		for y := 0; y < h; y++ {
			sy := int(float64(h)/2 + (float64(y)-float64(h)/2)/scale[1])
			for x := 0; x < w; x++ {
				sx := int(float64(w)/2 + (float64(x)-float64(w)/2)/scale[0])
				p := frame.PixOffset(x, y)
				if sx >= 0 && sx < w && sy >= 0 && sy < h {
					copy(frame.Pix[p:p+4], artwork.Pix[artwork.PixOffset(sx, sy):artwork.PixOffset(sx, sy)+4])
				} else {
					frame.Pix[p+3] = 255
				}
			}
		}
		if _, err := t.Write([]byte("\x1b[1;1H")); err != nil {
			return err
		}
		if _, err := t.Write(append(sixel.EncodePalette(frame, scene.pal), frames.Query()...)); err != nil {
			return err
		}
		time.Sleep(45 * time.Millisecond)
	}
	return nil
}
