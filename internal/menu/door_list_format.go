package menu

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// formatDoorListLine fills a DOORLIST.MID template line for one door.
// Placeholders: ^ID = display index, ^CO = internal code (what the user
// types at the door prompt), ^NA = display name, ^TY = door type.
func formatDoorListLine(template string, displayIdx int, code string, d config.DoorConfig) string {
	doorType := "Native"
	switch {
	case d.Type == "v3_script":
		doorType = "VPL"
	case d.Type == "synchronet_js":
		doorType = "Synchronet JS"
	case d.IsDOS:
		doorType = "DOS"
	}

	// Single-pass replacement: substituted values (e.g. a Name containing a
	// literal "^TY") must not be re-processed by later placeholders.
	return strings.NewReplacer(
		"^ID", fmt.Sprintf("%-3d", displayIdx),
		"^CO", fmt.Sprintf("%-16s", code),
		"^NA", fmt.Sprintf("%-30s", d.Name),
		"^TY", doorType,
	).Replace(template)
}
