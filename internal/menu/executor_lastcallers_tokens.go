package menu

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

var lastCallerATTokenRegex = regexp.MustCompile(`@([A-Za-z]{2,12})(?::(-?\d+))?@`)

func renderLastCallerATTokens(template string, record user.CallRecord, totalUsers int, userNote string, timeLoc *time.Location) string {
	return lastCallerATTokenRegex.ReplaceAllStringFunc(template, func(match string) string {
		parts := lastCallerATTokenRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		code := strings.ToUpper(parts[1])
		value, ok := lastCallerATTokenValue(code, record, totalUsers, userNote, timeLoc)
		if !ok {
			return match
		}

		if len(parts) > 2 && parts[2] != "" {
			if width, err := strconv.Atoi(parts[2]); err == nil {
				if isLastCallerATCenterAligned(code) {
					value = formatLastCallerATWidthCentered(value, width)
				} else {
					value = formatLastCallerATWidth(value, width, isLastCallerATNumeric(code))
				}
			}
		}

		return value
	})
}

func renderLastCallerGlobalATTokens(template string, totalUsers int) string {
	return lastCallerATTokenRegex.ReplaceAllStringFunc(template, func(match string) string {
		parts := lastCallerATTokenRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		code := strings.ToUpper(parts[1])
		if code != "UC" && code != "USERCT" {
			return match
		}

		value := strconv.Itoa(totalUsers)
		if len(parts) > 2 && parts[2] != "" {
			if width, err := strconv.Atoi(parts[2]); err == nil {
				value = formatLastCallerATWidth(value, width, true)
			}
		}

		return value
	})
}

func lastCallerATTokenValue(code string, record user.CallRecord, totalUsers int, userNote string, timeLoc *time.Location) (string, bool) {
	switch code {
	case "UC", "USERCT":
		return strconv.Itoa(totalUsers), true
	case "NOTE", "NT":
		return userNote, true
	case "CA":
		return strconv.FormatUint(record.CallNumber, 10), true
	case "UN":
		return record.Handle, true
	case "LC":
		return record.GroupLocation, true
	case "ND":
		return strconv.Itoa(record.NodeID), true
	case "LO":
		if record.ConnectTime.IsZero() {
			return "", true
		}
		return formatLastCallerShortLocalTime(record.ConnectTime, timeLoc), true
	case "LT":
		if !record.DisconnectTime.IsZero() {
			return formatLastCallerShortLocalTime(record.DisconnectTime, timeLoc), true
		}
		if !record.ConnectTime.IsZero() {
			return formatLastCallerShortLocalTime(record.ConnectTime.Add(record.Duration), timeLoc), true
		}
		return "", true
	case "NU":
		if record.CallNumber <= 1 {
			return "*", true
		}
		return " ", true
	case "TO":
		return strconv.Itoa(int(record.Duration.Minutes())), true
	case "MP", "MR", "DL", "UL", "ES", "FS":
		return "0", true
	case "DK":
		return strconv.Itoa(int(record.DownloadedMB * 1024.0)), true
	case "UK":
		return strconv.Itoa(int(record.UploadedMB * 1024.0)), true
	default:
		return "", false
	}
}

func isLastCallerATNumeric(code string) bool {
	switch code {
	case "CA", "ND", "TO", "MP", "MR", "DL", "DK", "UL", "UK", "ES", "FS":
		return true
	default:
		return false
	}
}

func isLastCallerATCenterAligned(code string) bool {
	switch code {
	case "ND", "CA", "TO":
		return true
	default:
		return false
	}
}

func formatLastCallerATWidth(value string, width int, alignRight bool) string {
	if width == 0 {
		return value
	}

	if width < 0 {
		width = -width
		alignRight = true
	}

	runes := []rune(value)
	if len(runes) > width {
		runes = runes[:width]
	}
	value = string(runes)

	padding := width - utf8.RuneCountInString(value)
	if padding <= 0 {
		return value
	}

	pad := strings.Repeat(" ", padding)
	if alignRight {
		return pad + value
	}
	return value + pad
}

func formatLastCallerATWidthCentered(value string, width int) string {
	if width == 0 {
		return value
	}

	if width < 0 {
		width = -width
	}

	runes := []rune(value)
	if len(runes) > width {
		runes = runes[:width]
	}
	value = string(runes)

	padding := width - utf8.RuneCountInString(value)
	if padding <= 0 {
		return value
	}

	left := padding / 2
	right := padding - left
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
}

func formatLastCallerShortLocalTime(t time.Time, timeLoc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	if timeLoc == nil {
		timeLoc = time.Local
	}
	return t.In(timeLoc).Format("03:04pm")
}

func getLastCallerTimeLocation(configTZ string) *time.Location {
	return config.LoadTimezone(configTZ)
}
