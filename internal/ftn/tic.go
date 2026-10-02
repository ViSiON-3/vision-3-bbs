package ftn

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strconv"
	"strings"
)

// TIC is a parsed .TIC control file: the description that travels with a
// file distributed through an FTN file echo. Each line is a keyword and its
// value; keywords are case-insensitive, and several may repeat.
//
// Keywords the inbound processor has no use for (Magic, Replaces, Date,
// Created, ...) are ignored.
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

	Size   int64 // -1 when the TIC has no Size line
	CRC    uint32
	HasCRC bool
}

// ParseTIC reads a TIC control file. It fails when the Area or File line is
// missing, or a Crc or Size value does not parse, since such a file cannot be
// processed safely.
func ParseTIC(r io.Reader) (*TIC, error) {
	t := &TIC{Size: -1}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\x1a")
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
		case "size":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Size %q", value)
			}
			t.Size = n
		case "crc":
			n, err := strconv.ParseUint(value, 16, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid Crc %q", value)
			}
			t.CRC = uint32(n)
			t.HasCRC = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if t.Area == "" {
		return nil, fmt.Errorf("no Area line")
	}
	if t.File == "" {
		return nil, fmt.Errorf("no File line")
	}
	return t, nil
}

// ReadTIC parses the TIC control file at path.
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
