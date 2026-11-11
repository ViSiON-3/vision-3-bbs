package configeditor

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart/colorpick"
)

// The colour ramp editor (#1002): a dialog over the edit screen for an
// ftColorRamp field, the theme's toast fade colours. The ramp shows as
// swatches; each step's colour is chosen with the colour picker the string
// editor uses (internal/tuiart/colorpick), and a preview plays a sample
// toast through the ramp, in, hold and out, at the board's fade step time,
// so the sysop sees the fade before saving it.

// rampTitle heads the dialog.
const rampTitle = "Toast Fade Colors"

// rampSample is the text the preview fades.
const rampSample = ">> Alice posted in General"

// rampHold is how long the preview shows the sample between its fades, and
// rampGap how long it leaves the row blank before playing again.
const (
	rampHold = time.Second
	rampGap  = 400 * time.Millisecond
)

// rampEditor is the dialog's state. field is the field being edited, steps
// the ramp as edited, sel the selected step, picking whether the colour
// picker is open for it, and frame the preview's place. gen tells this
// opening's preview ticks from an earlier one's.
type rampEditor struct {
	field   fieldDef
	steps   []int
	sel     int
	picking bool
	pick    colorpick.Picker
	frame   int
	gen     int
	step    time.Duration
	errMsg  string
}

// rampTickMsg advances the preview of the dialog opened as gen.
type rampTickMsg struct{ gen int }

// rampTick schedules the preview's next frame.
func rampTick(gen int, step time.Duration) tea.Cmd {
	return tea.Tick(step, func(time.Time) tea.Msg { return rampTickMsg{gen: gen} })
}

// startRampEdit opens the ramp editor for f, the current field.
func (m Model) startRampEdit(f fieldDef) (Model, tea.Cmd) {
	steps, ok := config.ParseToastFadeColors(f.Get())
	if !ok {
		steps, _ = config.ParseToastFadeColors(config.DefaultToastFadeColors)
	}
	m.ramp = rampEditor{
		field: f,
		steps: steps,
		gen:   m.ramp.gen + 1,
		step:  m.configs.Server.ToastFadeStep(),
	}
	m.mode = modeColorRamp
	return m, rampTick(m.ramp.gen, m.ramp.step)
}

// handleRampTick moves the preview on one frame, while the dialog that
// scheduled it is open.
func (m Model) handleRampTick(msg rampTickMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeColorRamp || msg.gen != m.ramp.gen {
		return m, nil
	}
	m.ramp.frame++
	return m, rampTick(m.ramp.gen, m.ramp.step)
}

// updateColorRamp handles keys in the ramp editor.
func (m Model) updateColorRamp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := &m.ramp
	r.errMsg = ""
	if r.picking {
		switch {
		case r.pick.Key(msg):
		case msg.Type == tea.KeyEnter:
			r.steps[r.sel] = r.pick.FG
			r.picking = false
		case msg.Type == tea.KeyEscape:
			r.picking = false
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyLeft:
		r.sel = max(0, r.sel-1)
	case tea.KeyRight:
		r.sel = min(len(r.steps)-1, r.sel+1)
	case tea.KeyCtrlLeft:
		if r.sel > 0 {
			r.steps[r.sel-1], r.steps[r.sel] = r.steps[r.sel], r.steps[r.sel-1]
			r.sel--
		}
	case tea.KeyCtrlRight:
		if r.sel < len(r.steps)-1 {
			r.steps[r.sel+1], r.steps[r.sel] = r.steps[r.sel], r.steps[r.sel+1]
			r.sel++
		}
	case tea.KeyEnter:
		r.pick = colorpick.New(r.steps[r.sel], true)
		r.picking = true
	case tea.KeyInsert:
		if len(r.steps) >= config.MaxToastFadeSteps {
			r.errMsg = fmt.Sprintf("At most %d steps", config.MaxToastFadeSteps)
			break
		}
		r.steps = append(r.steps[:r.sel+1], append([]int{r.steps[r.sel]}, r.steps[r.sel+1:]...)...)
		r.sel++
	case tea.KeyDelete:
		if len(r.steps) <= 1 {
			r.errMsg = "A ramp needs at least one step"
			break
		}
		r.steps = append(r.steps[:r.sel], r.steps[r.sel+1:]...)
		r.sel = min(r.sel, len(r.steps)-1)
	case tea.KeyEscape:
		m.mode = modeSysConfigEdit
	case tea.KeyRunes:
		switch strings.ToLower(string(msg.Runes)) {
		case "d":
			r.steps, _ = config.ParseToastFadeColors(config.DefaultToastFadeColors)
			r.sel = 0
		case "s":
			if err := r.field.Set(config.FormatToastFadeColors(r.steps)); err != nil {
				r.errMsg = err.Error()
				break
			}
			m.dirty = true
			m.message = ""
			m.mode = modeSysConfigEdit
		}
	}
	return m, nil
}

// rampFrame is the colour the preview shows the sample in at frame, and
// whether it shows it at all: the ramp in, the sample's own colour (white)
// for rampHold, the ramp out, then blank for rampGap.
func rampFrame(steps []int, frame int, step time.Duration) (color int, shown bool) {
	hold := max(1, int(rampHold/step))
	gap := max(1, int(rampGap/step))
	n := len(steps)
	cycle := 2*n + hold + gap
	f := frame % cycle
	switch {
	case f < n:
		return steps[f], true
	case f < n+hold:
		return 15, true
	case f < 2*n+hold:
		return steps[2*n+hold-1-f], true
	default:
		return 0, false
	}
}

// rampSwatches draws steps as swatches, sel bracketed when mark is set.
func rampSwatches(steps []int, sel int, mark bool, st colorpick.Styles) string {
	var b strings.Builder
	for i, c := range steps {
		b.WriteString(colorpick.Swatch(fmt.Sprintf("%02d", c), colorpick.FGStyle(c), mark && i == sel, st))
	}
	return b.String()
}

// rampStyles are this editor's dialog colours, for the shared picker.
func rampStyles() colorpick.Styles {
	return colorpick.Styles{Text: dialogTextStyle, Button: dialogTextStyle, Active: buttonActiveStyle}
}

// viewColorRamp draws the ramp editor over the edit screen.
func (m Model) viewColorRamp() string {
	lines := strings.Split(m.viewSysConfigEdit(), "\n")
	r := m.ramp
	st := rampStyles()
	w := colorpick.Width
	inner := w - 2
	side := dialogBorderStyle.Render("│")
	row := func(content string) string {
		if pad := inner - lipgloss.Width(content); pad > 0 {
			content += dialogTextStyle.Render(strings.Repeat(" ", pad))
		}
		return side + content + side
	}
	text := func(s string) string { return row(dialogTextStyle.Render(s)) }

	dialog := []string{
		dialogBorderStyle.Render("┌" + strings.Repeat("─", inner) + "┐"),
		side + dialogTitleStyle.Render(centerText(rampTitle, inner)) + side,
		text(""),
		row(dialogTextStyle.Render(" Steps:  ") + rampSwatches(r.steps, r.sel, !r.picking, st)),
		text(""),
	}
	if r.picking {
		dialog = append(dialog,
			text(fmt.Sprintf(" Colour for step %d:", r.sel+1)),
			row(r.pick.FGRow(st)),
			text(""),
		)
	}
	preview := dialogTextStyle.Render(strings.Repeat(" ", len(rampSample)))
	if c, shown := rampFrame(r.steps, r.frame, r.step); shown {
		preview = colorpick.FGStyle(c).Render(rampSample)
	}
	dialog = append(dialog, row(dialogTextStyle.Render(" Preview: ")+preview), text(""))
	help := []string{" ←→ step   Enter colour   Ins add   Del remove   Ctrl+←→ move", " D default   S save   Esc cancel"}
	if r.picking {
		help = []string{" ←→ colour   Enter choose   Esc back", ""}
	}
	for _, h := range help {
		dialog = append(dialog, text(h))
	}
	if r.errMsg != "" {
		dialog = append(dialog, row(flashMessageStyle.Render(" "+r.errMsg)))
	}
	dialog = append(dialog, dialogBorderStyle.Render("└"+strings.Repeat("─", inner)+"┘"))

	startRow := max(0, (m.height-len(dialog))/2)
	startCol := max(0, (m.width-w)/2)
	for i, dl := range dialog {
		at := startRow + i
		if at < len(lines) {
			lines[at] = padToCol(lines[at], startCol) + dl + skipToCol(lines[at], startCol+w)
		}
	}
	return strings.Join(lines, "\n")
}
