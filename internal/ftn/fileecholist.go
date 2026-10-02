package ftn

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// ParseFileEchoList parses a file echo list, the file-echo counterpart of an
// echomail .na list. Networks publish it in one of two shapes, and both are
// accepted, line by line:
//
//	Area TQW_NODE    0   !   Weekly Nodelists   (FILEBONE.NA: tag, level, flags, description)
//	TQW_NODE         Weekly Nodelists            (plain: tag, description)
//
// Lines starting with ';', '#' or '%' are comments. A tag listed twice is an
// error, as it is in an echomail list: it means the file is not in either
// shape.
func ParseFileEchoList(r io.Reader) ([]EchoArea, error) {
	var areas []EchoArea
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "%") {
			continue
		}
		fields := strings.Fields(line)
		var tag string
		var rest []string
		if strings.EqualFold(fields[0], "area") && len(fields) >= 2 {
			tag, rest = fields[1], fields[2:]
			// The access level and the flags column, when present.
			if len(rest) > 0 && isDigits(rest[0]) {
				rest = rest[1:]
			}
			if len(rest) > 0 && !hasAlnum(rest[0]) {
				rest = rest[1:]
			}
		} else {
			tag, rest = fields[0], fields[1:]
		}
		if !isFileEchoTag(tag) {
			continue
		}
		desc := strings.TrimSpace(strings.TrimPrefix(strings.Join(rest, " "), "-"))

		key := strings.ToUpper(tag)
		if seen[key] {
			return nil, fmt.Errorf("file echo %s is listed twice (line %q) — this does not look like a file echo list", tag, line)
		}
		seen[key] = true
		areas = append(areas, EchoArea{Tag: tag, Description: desc})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(areas) == 0 {
		return nil, fmt.Errorf("no file echoes found")
	}
	return areas, nil
}

// isFileEchoTag reports whether s can be a file echo tag: letters, digits and
// _ - . only, at most 50 characters (the echomail importer's rule).
func isFileEchoTag(s string) bool {
	if s == "" || len(s) > 50 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
