package menu

import (
	"strings"
	"unicode/utf8"
)

func truncateRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func isPipeCodeStartChar(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func pipeCodeLenAt(value string, index int) int {
	if index < 0 || index >= len(value) || value[index] != '|' || index+1 >= len(value) {
		return 0
	}

	// 3-char forms: |00..|15, |CR, |DE, |CL, |PP, |23
	if index+2 < len(value) {
		two := value[index+1 : index+3]
		if len(two) == 2 {
			if (two[0] >= '0' && two[0] <= '9') && (two[1] >= '0' && two[1] <= '9') {
				return 3
			}
			u := strings.ToUpper(two)
			if u == "CR" || u == "DE" || u == "CL" || u == "PP" {
				return 3
			}
		}
	}

	// Background forms: |B0..|B9 (3 chars), |B10..|B15 (4 chars)
	if index+2 < len(value) {
		if (value[index+1] == 'B' || value[index+1] == 'b') && (value[index+2] >= '0' && value[index+2] <= '9') {
			if index+3 < len(value) && (value[index+3] >= '0' && value[index+3] <= '9') {
				return 4 // |B10..|B15 (validated loosely)
			}
			return 3 // |B0..|B9
		}
	}

	// 2-char form: |P
	if index+1 < len(value) && (value[index+1] == 'P' || value[index+1] == 'p') {
		return 2
	}

	if index+1 < len(value) && isPipeCodeStartChar(value[index+1]) {
		return 0
	}

	return 0
}

func truncateOnelinerPreservePipeCodes(value string, maxVisible int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxVisible <= 0 {
		return ""
	}

	var out strings.Builder
	visible := 0
	i := 0
	for i < len(value) {
		if value[i] == '|' {
			codeLen := pipeCodeLenAt(value, i)
			if codeLen > 0 && i+codeLen <= len(value) {
				out.WriteString(value[i : i+codeLen])
				i += codeLen
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		if visible >= maxVisible {
			break
		}
		out.WriteString(value[i : i+size])
		visible++
		i += size
	}

	return strings.TrimSpace(out.String())
}

func truncatePipeCodedText(value string, maxVisible int) string {
	if value == "" || maxVisible <= 0 {
		return ""
	}

	var out strings.Builder
	visible := 0
	i := 0
	for i < len(value) {
		if value[i] == '|' && i+1 < len(value) && value[i+1] == '|' {
			if visible >= maxVisible {
				break
			}
			out.WriteString("||")
			visible++
			i += 2
			continue
		}

		if value[i] == '|' {
			codeLen := pipeCodeLenAt(value, i)
			if codeLen > 0 && i+codeLen <= len(value) {
				out.WriteString(value[i : i+codeLen])
				i += codeLen
				continue
			}
		}

		_, size := utf8.DecodeRuneInString(value[i:])
		if size <= 0 {
			size = 1
		}
		if visible >= maxVisible {
			break
		}
		out.WriteString(value[i : i+size])
		visible++
		i += size
	}

	return out.String()
}

func containsDisallowedOnelinerColorCode(value string) bool {
	i := 0
	for i < len(value) {
		if value[i] == '|' && i+1 < len(value) && value[i+1] == '|' {
			i += 2
			continue
		}

		if value[i] == '|' {
			codeLen := pipeCodeLenAt(value, i)
			if codeLen > 0 && i+codeLen <= len(value) {
				// Only standard foreground colors |01..|15 are allowed.
				if codeLen != 3 {
					return true
				}

				colorCode := value[i+1 : i+3]
				if colorCode < "01" || colorCode > "15" {
					return true
				}

				i += codeLen
				continue
			}
		}

		_, size := utf8.DecodeRuneInString(value[i:])
		if size <= 0 {
			size = 1
		}
		i += size
	}

	return false
}

func formatOnelinerDisplayName(name string) string {
	formatted := truncateRunes(name, oneLinerNameWidth)
	if formatted == "" {
		formatted = "Unknown"
	}
	padding := oneLinerNameWidth - utf8.RuneCountInString(formatted)
	if padding > 0 {
		formatted = strings.Repeat(" ", padding) + formatted
	}
	return formatted
}

func onelinerVisibleName(record onelinerRecord, anonymousName string) string {
	if strings.TrimSpace(anonymousName) == "" {
		anonymousName = "Anonymous"
	}
	if record.Anonymous {
		return anonymousName
	}
	if strings.TrimSpace(record.PostedByHandle) != "" {
		return record.PostedByHandle
	}
	if strings.TrimSpace(record.PostedByUsername) != "" {
		return record.PostedByUsername
	}
	return "Unknown"
}

const (
	oneLinerMaxStored  = 20
	oneLinerMaxDisplay = 10
	oneLinerMaxLength  = 51
	oneLinerNameWidth  = 20
)
