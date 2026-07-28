package menu

import (
	"bytes"
	"os"
	"regexp"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func normalizePipeCodeDelimiters(input []byte) []byte {
	if len(input) == 0 {
		return input
	}

	// Only normalize likely pipe-code delimiters (e.g. ¦CR, ¦08, │DE).
	// Do NOT blanket-convert ANSI line-art bytes (such as CP437 0xB3), which
	// can corrupt imported art templates.
	normalized := make([]byte, 0, len(input))

	isPipeCodeLead := func(b byte) bool {
		return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
	}

	for i := 0; i < len(input); {
		replaced := false

		// UTF-8 broken bar (U+00A6 => 0xC2 0xA6)
		if i+1 < len(input) && input[i] == 0xC2 && input[i+1] == 0xA6 {
			if i+2 < len(input) && isPipeCodeLead(input[i+2]) {
				normalized = append(normalized, '|')
				i += 2
				replaced = true
			}
		}

		if !replaced {
			// UTF-8 box drawing light vertical (U+2502 => 0xE2 0x94 0x82)
			if i+2 < len(input) && input[i] == 0xE2 && input[i+1] == 0x94 && input[i+2] == 0x82 {
				if i+3 < len(input) && isPipeCodeLead(input[i+3]) {
					normalized = append(normalized, '|')
					i += 3
					replaced = true
				}
			}
		}

		if !replaced {
			// Raw single-byte broken bar (0xA6)
			if input[i] == 0xA6 {
				if i+1 < len(input) && isPipeCodeLead(input[i+1]) {
					normalized = append(normalized, '|')
					i++
					replaced = true
				}
			}
		}

		if !replaced {
			normalized = append(normalized, input[i])
			i++
		}
	}

	return normalized
}

// readTemplateFile reads a template file at path, trying the base path first,
// then path+".ANS", then path+".ans", so files saved by ANSI editors (which
// typically append the uppercase .ANS extension) are recognised automatically.
// SAUCE metadata is stripped from the returned content.
func readTemplateFile(path string) ([]byte, error) {
	var (
		data []byte
		err  error
	)
	for _, candidate := range []string{path, path + ".ANS", path + ".ans"} {
		data, err = os.ReadFile(candidate)
		if err == nil {
			return stripSauceMetadata(data), nil
		}
		if !os.IsNotExist(err) {
			// Real I/O error (permissions, etc.) — stop trying.
			return nil, err
		}
	}
	return nil, err
}

func stripSauceMetadata(input []byte) []byte {
	if len(input) < 7 {
		return input
	}

	idx := bytes.LastIndex(input, []byte("SAUCE00"))
	if idx < 0 {
		return input
	}

	// SAUCE record should be near EOF; ignore stray in-body text matches.
	if idx < len(input)-512 {
		return input
	}

	cut := idx

	// If full SAUCE record is present, remove optional COMNT block too.
	if idx+128 <= len(input) {
		comments := int(input[idx+104])
		if comments > 0 {
			commentLen := 5 + (comments * 64)
			commentStart := idx - commentLen
			if commentStart >= 0 && bytes.Equal(input[commentStart:commentStart+5], []byte("COMNT")) {
				cut = commentStart
			}
		}
	}

	// Remove CP/M EOF marker if present before metadata.
	if cut > 0 && input[cut-1] == 0x1A {
		cut--
	}

	return input[:cut]
}

// replaceMenuATCode replaces @CODE@, @CODE:N@, @CODE##…@, and modifier forms
// @CODE|L:N@, @CODE|R:N@, @CODE|C:N@ (plus ## visual-width variants) in raw
// ANSI content with the supplied value, applying width/padding when specified.
//
// Alignment modifiers (between | and width):
//
//	L = left-align (default), R = right-align, C = center
//
// Examples: @RR@, @RR:60@, @RR|C:60@, @RR|C##########@, @RR|R8@
func replaceMenuATCode(content []byte, code string, value string) []byte {
	pat := regexp.MustCompile(`@` + regexp.QuoteMeta(code) + `(?:\|([LRC])(\d+)?)?(?::(\d+)|(#+))?@`)
	return pat.ReplaceAllFunc(content, func(match []byte) []byte {
		parts := pat.FindSubmatch(match)
		// parts[1] = alignment modifier (L/R/C)
		// parts[2] = digits after modifier (e.g. @RR|C60@)
		// parts[3] = :N explicit width
		// parts[4] = ## visual width
		alignMode := ansi.AlignLeft
		if len(parts) > 1 && len(parts[1]) > 0 {
			alignMode = ansi.ParseAlignment(string(parts[1]))
		}

		width := 0
		if len(parts) > 2 && len(parts[2]) > 0 {
			// digits after modifier (e.g. @RR|R8@)
			width, _ = strconv.Atoi(string(parts[2]))
		} else if len(parts) > 3 && len(parts[3]) > 0 {
			// :N explicit width
			width, _ = strconv.Atoi(string(parts[3]))
		} else if len(parts) > 4 && len(parts[4]) > 0 {
			// ## visual width — total placeholder length
			width = len(match)
		}

		result := value
		if width > 0 {
			result = ansi.ApplyWidthConstraintAligned(value, width, alignMode)
		}
		return []byte(result)
	})
}
