package wfcui

import "github.com/charmbracelet/lipgloss"

// colorSet holds the resolved lipgloss colors used in the WFC console.
// When opts.NoColor is true, all colors are the zero value (no styling).
type colorSet struct {
	header     lipgloss.Style
	selected   lipgloss.Style
	dimmed     lipgloss.Style
	statusOn   lipgloss.Style
	statusOff  lipgloss.Style
	eventTime  lipgloss.Style
	border     lipgloss.Style
	cmdBar     lipgloss.Style
	errorStyle lipgloss.Style
}

// newStyles builds a colorSet from Options.
func newStyles(opts Options) colorSet {
	if opts.NoColor {
		return colorSet{
			header:     lipgloss.NewStyle(),
			selected:   lipgloss.NewStyle(),
			dimmed:     lipgloss.NewStyle(),
			statusOn:   lipgloss.NewStyle(),
			statusOff:  lipgloss.NewStyle(),
			eventTime:  lipgloss.NewStyle(),
			border:     lipgloss.NewStyle(),
			cmdBar:     lipgloss.NewStyle(),
			errorStyle: lipgloss.NewStyle(),
		}
	}
	return colorSet{
		header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		selected:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4")),
		dimmed:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		statusOn:   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		statusOff:  lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		eventTime:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		border:     lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		cmdBar:     lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Background(lipgloss.Color("0")),
		errorStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")),
	}
}

// borderSet returns a lipgloss.Border based on ASCII mode.
func borderSet(ascii bool) lipgloss.Border {
	if ascii {
		return lipgloss.ASCIIBorder()
	}
	return lipgloss.NormalBorder()
}
