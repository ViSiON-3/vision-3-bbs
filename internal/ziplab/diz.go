package ziplab

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// stripSauceMetadata removes the SAUCE metadata block (and optional COMNT block)
// from the end of ANSI art file bytes, along with the CP/M EOF marker (0x1A).
func stripSauceMetadata(input []byte) []byte {
	if len(input) < 7 {
		return input
	}
	idx := bytes.LastIndex(input, []byte("SAUCE00"))
	if idx < 0 {
		return input
	}
	if idx < len(input)-512 {
		return input
	}
	cut := idx
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
	if cut > 0 && input[cut-1] == 0x1A {
		cut--
	}
	return input[:cut]
}

// cleanDIZ strips trailing whitespace and DOS-era control characters
// (Ctrl-Z / 0x1A CP/M EOF marker) from FILE_ID.DIZ content, and
// converts any CP437 high bytes to their UTF-8 equivalents so the
// result is valid UTF-8 that survives JSON serialisation.
func cleanDIZ(raw string) string {
	s := strings.TrimRight(raw, " \t\r\n\x1a")
	s = strings.ReplaceAll(s, "\x1a", "")
	return cp437BytesToUTF8(s)
}

// utf8BOM is the UTF-8 byte order mark some editors write at the start of
// a file.
const utf8BOM = "\xef\xbb\xbf"

// cp437BytesToUTF8 converts FILE_ID.DIZ text to valid UTF-8. The encoding
// is decided for the whole text, not per character: many CP437 box-drawing
// pairs are also valid two-byte UTF-8 (CD BB, "═╗", decodes as U+037B), so
// keeping every valid UTF-8 sequence would corrupt CP437 art. Text is kept
// as UTF-8 when it starts with a byte order mark, or when it is valid UTF-8
// and holds at least one multi-byte character that is not a look-alike of
// CP437 drawing characters. Anything else is CP437 throughout.
func cp437BytesToUTF8(s string) string {
	if bom, ok := strings.CutPrefix(s, utf8BOM); ok && utf8.ValidString(bom) {
		return bom
	}
	if isLikelyUTF8(s) {
		return s
	}

	var out strings.Builder
	out.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 {
			out.WriteByte(c)
			continue
		}
		// Every high byte has a CP437 mapping.
		out.WriteRune(ansi.Cp437ToUnicode[c])
	}
	return out.String()
}

// isLikelyUTF8 reports whether s should be read as UTF-8 rather than CP437:
// it must be valid UTF-8 and contain a multi-byte character that
// cp437LookAlike does not claim. Pure ASCII is trivially UTF-8.
func isLikelyUTF8(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	multiByte := false
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if size > 1 {
			multiByte = true
			if !cp437LookAlike(s[i : i+size]) {
				return true
			}
		}
		i += size
	}
	return !multiByte
}

// cp437LookAlike reports whether seq, one valid multi-byte UTF-8 sequence,
// is more plausibly a run of CP437 characters. CP437 continuation bytes
// 0xB0-0xBF are the shade and box-drawing characters (░▒▓│┤╡╢╖╕╣║╗╝╜╛┐),
// and every lead byte is a line, block or Greek/math character, so a
// sequence whose continuation bytes all fall in that range reads as drawing
// characters. The exceptions are lead bytes of common letters and symbols,
// whose sequences are real text: Latin-1 symbols (C2, except C1 control
// codes) and letters (C3), Latin Extended-A (C5), Greek (CE, CF), Cyrillic
// (D0, D1) and Arabic (D8).
func cp437LookAlike(seq string) bool {
	switch seq[0] {
	case 0xC2:
		// U+0080-U+009F are C1 control codes, never real DIZ text.
		return seq[1] < 0xA0
	case 0xC3, 0xC5, 0xCE, 0xCF, 0xD0, 0xD1, 0xD8:
		return false
	}
	for i := 1; i < len(seq); i++ {
		if seq[i] < 0xB0 {
			return false
		}
	}
	return true
}

// dizRank ranks a candidate description file named name, found depth
// directories below the archive root (0 for the root itself). Lower ranks
// win: FILE_ID.ANS beats FILE_ID.DIZ, and at each a file at the root beats one
// in a subdirectory. Only the root and one level of subdirectories count; a
// description deeper down belongs to something bundled inside the archive.
// ok is false for any other file or depth.
func dizRank(name string, depth int) (rank int, ok bool) {
	if depth < 0 || depth > 1 {
		return 0, false
	}
	switch {
	case strings.EqualFold(name, "FILE_ID.ANS"):
		return depth, true
	case strings.EqualFold(name, "FILE_ID.DIZ"):
		return 2 + depth, true
	}
	return 0, false
}

// ExtractDIZFromZip opens a ZIP archive and reads the file description,
// choosing among FILE_ID.ANS and FILE_ID.DIZ as dizRank does. Returns empty
// string if neither is found.
func ExtractDIZFromZip(archivePath string) (string, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("failed to open zip %s: %w", archivePath, err)
	}
	defer func() { _ = r.Close() }() // read-only zip reader

	var dizFile *zip.File
	bestRank := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// ZIP entry names always use "/", whatever system made the archive.
		name := strings.TrimPrefix(path.Clean(f.Name), "/")
		rank, ok := dizRank(path.Base(name), strings.Count(name, "/"))
		if ok && (dizFile == nil || rank < bestRank) {
			dizFile, bestRank = f, rank
		}
	}

	if dizFile == nil {
		return "", nil
	}

	rc, err := dizFile.Open()
	if err != nil {
		return "", fmt.Errorf("failed to read %s from %s: %w", dizFile.Name, archivePath, err)
	}
	defer func() { _ = rc.Close() }() // read-only

	data, readErr := io.ReadAll(io.LimitReader(rc, 10*1024))
	if readErr != nil {
		return "", fmt.Errorf("failed to read %s from %s: %w", dizFile.Name, archivePath, readErr)
	}
	return cleanDIZ(string(stripSauceMetadata(data))), nil
}

// ExtractDIZFromArchive extracts FILE_ID.DIZ from a supported archive.
// For native ZIP files, it reads directly from the archive without extraction.
// For external formats, it extracts to a temp directory, searches for the DIZ,
// and cleans up. Returns empty string if no DIZ is found.
func ExtractDIZFromArchive(archivePath, configPath string) (string, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		cfg = DefaultConfig()
	}

	at, ok := cfg.GetArchiveType(archivePath)
	if !ok {
		return "", nil
	}

	if at.Native {
		return ExtractDIZFromZip(archivePath)
	}

	if at.ExtractCommand == "" {
		return "", fmt.Errorf("no extract command configured for %s", filepath.Ext(archivePath))
	}

	p := NewProcessor(cfg, filepath.Dir(archivePath))
	workDir, err := p.StepExtract(archivePath)
	if err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}
	if workDir == "" {
		return "", nil
	}
	defer func() { _ = os.RemoveAll(workDir) }() // best-effort temp cleanup

	return p.findAndReadDIZ(workDir), nil
}
