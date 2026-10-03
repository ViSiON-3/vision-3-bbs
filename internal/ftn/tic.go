package ftn

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// TIC is a parsed .TIC control file: the description that travels with a
// file distributed through an FTN file echo. Each line is a keyword and its
// value; keywords are case-insensitive, and several may repeat.
//
// Keywords the inbound processor has no use for (Magic, Date, Created, ...)
// are ignored.
type TIC struct {
	Area     string   // file echo tag, e.g. "TQW_LINUXFILES"
	AreaDesc string   // Areadesc: the echo's description
	Origin   string   // address of the system that hatched the file
	From     string   // address of the system that sent us this TIC
	To       string   // address the TIC is for (often absent)
	File     string   // file name, traditionally 8.3
	LongName string   // Lfile or Fullname: the long file name, when it differs
	Desc     []string // one-line description(s)
	LDesc    []string // long description, one entry per line
	Path     []string // systems the file has passed through
	SeenBy   []string // systems that have the file
	Password string   // Pw: the password agreed with the sending link
	Replaces []string // files in the area this one supersedes; may hold * and ? wildcards

	Size   int64 // -1 when the TIC has no Size line
	CRC    uint32
	HasCRC bool
}

// ParseTIC reads a TIC control file. It fails when the Area, File or Crc line
// is missing, or a Crc or Size value does not parse, since such a file cannot
// be delivered safely: the CRC is the only check that the file is the one the
// TIC describes. On such a failure it still returns what it read, so the
// caller can find the file the TIC names and set it aside with it; the TIC is
// nil only when reading failed.
func ParseTIC(r io.Reader) (*TIC, error) {
	t := &TIC{Size: -1}
	var invalid error
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\x1a")
		// A line ends at its first NUL. Some tossers write a value from a
		// fixed-size field with its padding (Mystic has been seen to send
		// "Pw SECRET" followed by NULs and stray bytes); C-based readers stop
		// at the NUL, and no TIC value can contain one.
		if i := strings.IndexByte(line, 0); i >= 0 {
			line = line[:i]
		}
		key, value := splitTICLine(line)
		if key == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "area":
			t.Area = value
		case "areadesc":
			t.AreaDesc = value
		case "origin":
			t.Origin = value
		case "from":
			t.From = value
		case "to":
			t.To = value
		case "file":
			t.File = value
		case "lfile", "fullname":
			t.LongName = value
		case "desc":
			t.Desc = append(t.Desc, value)
		case "ldesc":
			t.LDesc = append(t.LDesc, value)
		case "path":
			t.Path = append(t.Path, value)
		case "seenby":
			t.SeenBy = append(t.SeenBy, value)
		case "pw":
			t.Password = value
		case "replaces":
			if value != "" {
				t.Replaces = append(t.Replaces, value)
			}
		case "size":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				invalid = fmt.Errorf("invalid Size %q", value)
				continue
			}
			t.Size = n
		case "crc":
			n, err := strconv.ParseUint(value, 16, 32)
			if err != nil {
				invalid = fmt.Errorf("invalid Crc %q", value)
				continue
			}
			t.CRC = uint32(n)
			t.HasCRC = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	switch {
	case invalid != nil:
		return t, invalid
	case t.Area == "":
		return t, fmt.Errorf("no Area line")
	case t.File == "":
		return t, fmt.Errorf("no File line")
	case !t.HasCRC:
		return t, fmt.Errorf("no Crc line")
	}
	return t, nil
}

// ReadTIC parses the TIC control file at path. See ParseTIC for what it
// returns on failure.
func ReadTIC(path string) (*TIC, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only
	return ParseTIC(f)
}

// splitTICLine splits a TIC line into its keyword and value. The value is
// everything after the first run of whitespace, trimmed; a description can
// itself contain runs of spaces, so only the separator is collapsed.
func splitTICLine(line string) (key, value string) {
	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return "", ""
	}
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i:])
}

// Description returns the file's description: the long description when the
// TIC has one, otherwise the one-line description(s), one per line.
func (t *TIC) Description() string {
	if len(t.LDesc) > 0 {
		return strings.Join(t.LDesc, "\n")
	}
	return strings.Join(t.Desc, "\n")
}

// ReplacesFile reports whether name is a file one of the TIC's Replaces lines
// names, by MatchFileName.
func (t *TIC) ReplacesFile(name string) bool {
	for _, pattern := range t.Replaces {
		if MatchFileName(pattern, name) {
			return true
		}
	}
	return false
}

// MatchFileName reports whether a file name matches a pattern in which * (any
// run of characters) and ? (any one character) are the only wildcards. Other
// characters match themselves, ignoring case the way strings.EqualFold does,
// so this agrees with the case-insensitive name comparisons elsewhere.
func MatchFileName(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	pi, ni := 0, 0
	star, resume := -1, 0 // last * seen, and where in name it is matching from
	for ni < len(n) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, resume = pi, ni
			pi++
		case pi < len(p) && (p[pi] == '?' || equalFoldRune(p[pi], n[ni])):
			pi++
			ni++
		case star >= 0:
			// Let the last * take one more character and try again.
			resume++
			pi, ni = star+1, resume
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// equalFoldRune reports whether a and b are equal under Unicode simple case
// folding.
func equalFoldRune(a, b rune) bool {
	if a == b {
		return true
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

// FromAddress parses the From line, ignoring any "@domain" suffix.
func (t *TIC) FromAddress() (Address, error) {
	return parseTICAddress(t.From)
}

// parseTICAddress parses an address as it appears in a TIC, which may carry
// a "@domain" suffix and, on Path lines, trailing text after the address.
func parseTICAddress(s string) (Address, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return Address{}, fmt.Errorf("empty address")
	}
	addr := fields[0]
	if i := strings.IndexByte(addr, '@'); i >= 0 {
		addr = addr[:i]
	}
	return ParseAddress(addr)
}

// FileCRC32 returns the CRC-32 (IEEE, as TIC files use) of the file at path.
func FileCRC32(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }() // read-only
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return 0, err
	}
	return h.Sum32(), nil
}

// FormatCRC32 renders a CRC-32 the way TIC files and file records carry it:
// eight uppercase hex digits.
func FormatCRC32(crc uint32) string {
	return fmt.Sprintf("%08X", crc)
}
