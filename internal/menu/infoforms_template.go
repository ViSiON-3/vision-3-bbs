package menu

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// expandInfoformCodes replaces infoform-specific placeholders in template text.
// |VN is replaced with the BBS version number.
func expandInfoformCodes(s string) string {
	return strings.ReplaceAll(s, "|VN", version.Number)
}

// prepareSegment normalizes newlines and expands infoform codes for terminal display.
func prepareSegment(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return expandInfoformCodes(s)
}

// parseTemplate reads a form template and returns the text segments and field metadata.
// Template format (from V2):
//   - Plain text displayed to user character by character
//   - * = pause for user input (one answer field, optional)
//   - *! = pause for user input (required — user cannot leave blank)
//   - |B<n>; = set max input buffer length to n characters for next field
type templateField struct {
	MaxLen   int  // Max input length (0 = default/unlimited)
	Required bool // If true, user cannot leave this field blank
}

type parsedTemplate struct {
	Segments []string        // Text segments between input fields
	Fields   []templateField // Field metadata (one per * marker)
}

// parseTemplateFile reads and parses a form template file.
func parseTemplateFile(rootConfigPath string, formNum int) (*parsedTemplate, error) {
	data, err := os.ReadFile(infoformsTemplatePath(rootConfigPath, formNum))
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}

	tmpl := &parsedTemplate{}
	var currentSegment strings.Builder
	currentMaxLen := 0

	i := 0
	for i < len(data) {
		ch := data[i]

		if ch == '*' {
			// Input field marker: * = optional, *! = required
			required := false
			if i+1 < len(data) && data[i+1] == '!' {
				required = true
				i++ // consume the '!'
			}
			tmpl.Segments = append(tmpl.Segments, currentSegment.String())
			currentSegment.Reset()
			tmpl.Fields = append(tmpl.Fields, templateField{MaxLen: currentMaxLen, Required: required})
			currentMaxLen = 0 // Reset for next field
			i++
			continue
		}

		if ch == '|' && i+1 < len(data) && (data[i+1] == 'B' || data[i+1] == 'b') {
			// |B<n>; — buffer length control code
			j := i + 2
			var numStr strings.Builder
			for j < len(data) && data[j] != ';' {
				numStr.WriteByte(data[j])
				j++
			}
			if j < len(data) && data[j] == ';' {
				if n, err := strconv.Atoi(numStr.String()); err == nil && n >= 1 && n <= 255 {
					currentMaxLen = n
				}
				i = j + 1
				continue
			}
			// Not a valid |B code, write literally
			currentSegment.WriteByte(ch)
			i++
			continue
		}

		currentSegment.WriteByte(ch)
		i++
	}

	// Add trailing text segment
	tmpl.Segments = append(tmpl.Segments, currentSegment.String())

	return tmpl, nil
}
