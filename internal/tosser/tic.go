package tosser

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/google/uuid"
)

// Inbound file echoes. A file distributed through an FTN file echo arrives as
// two files in the inbound directory: the file itself and a .TIC control file
// describing it (which echo, its CRC, its description, the password agreed
// with the sending link). processTICs matches each TIC to the file area linked
// to that echo for this network, checks it, and moves the file into the area
// with a file record. A TIC that cannot be delivered is moved aside, with its
// file, to the temp path's bad TIC directory.

// BadTICDirName is the subdirectory of the temp path that undeliverable TICs
// and their files are moved to. They are moved, never deleted: a missing file
// area or a password mismatch is a config fix away from being deliverable.
const BadTICDirName = "badtic"

// ticWaitFor is how long a TIC waits for its file before it is moved aside.
// binkd only exposes a file once it is fully received, but a session that
// drops between the TIC and its file leaves the file for the next one.
const ticWaitFor = quarantineAfter

// FileAreaStore is what TIC processing needs of the file areas.
// *file.FileManager implements it.
type FileAreaStore interface {
	ListAreas() []file.FileArea
	GetAreaUploadPath(areaID int) (string, error)
	GetFilesForArea(areaID int) []file.FileRecord
	AddFileRecord(record file.FileRecord) error
	UpdateFileRecord(fileID uuid.UUID, updateFunc func(*file.FileRecord)) error
}

// SetFileAreas gives the tosser the file areas, so ProcessInbound also
// delivers inbound file echoes (.TIC files) into the areas linked to them.
// Without it (the default), TICs are left in the inbound directory untouched.
func (t *Tosser) SetFileAreas(fa FileAreaStore) {
	t.fileAreas = fa
}

// isTICName reports whether an inbound file name is a TIC control file.
func isTICName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".tic")
}

// ticReferencedFiles returns the lower-cased names of the files the TICs in an
// inbound directory describe. Such a file is part of a file echo delivery,
// not mail: a file echo's .zip passes every check a ZIP mail bundle does, and
// tossing it as one would delete it. This holds whether or not this run
// processes TICs, and for TICs another network will take.
func ticReferencedFiles(dir string, entries []os.DirEntry) map[string]bool {
	refs := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !isTICName(e.Name()) {
			continue
		}
		// A TIC that does not parse still protects the file it names.
		tic, _ := ftn.ReadTIC(filepath.Join(dir, e.Name()))
		if tic == nil {
			continue
		}
		for _, n := range []string{tic.File, tic.LongName} {
			if n != "" {
				refs[strings.ToLower(n)] = true
			}
		}
	}
	return refs
}

// processTICs delivers the TICs in one inbound directory. It runs before the
// packets in the same directory are tossed, so a delivered file is gone from
// the inbound before anything else considers it.
func (t *Tosser) processTICs(dir string, result *TossResult) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			result.Errors = append(result.Errors, fmt.Sprintf("read inbound dir %s: %v", dir, err))
		}
		return
	}

	var tics []string
	for _, e := range entries {
		if !e.IsDir() && isTICName(e.Name()) {
			tics = append(tics, e.Name())
		}
	}
	if len(tics) == 0 {
		return
	}

	// File echo tag -> area, for this network's areas only: the same tag on
	// another network is a different echo.
	areas := make(map[string]file.FileArea)
	for _, a := range t.fileAreas.ListAreas() {
		if a.IsFileEcho() && strings.EqualFold(a.Network, t.networkName) {
			areas[strings.ToUpper(a.FileEcho)] = a
		}
	}

	// Only the secure inbound is written by sessions that passed binkd's
	// password check. A TIC anywhere else is trusted only on its own Pw line.
	secure := t.paths.SecureInboundPath != "" && dir == t.paths.SecureInboundPath
	for _, name := range tics {
		t.processTIC(dir, name, secure, areas, result)
	}
}

// processTIC delivers a single TIC. secure reports whether dir is the secure
// inbound.
func (t *Tosser) processTIC(dir, ticName string, secure bool, areas map[string]file.FileArea, result *TossResult) {
	ticPath := filepath.Join(dir, ticName)
	tic, err := ftn.ReadTIC(ticPath)
	if err != nil {
		// What did parse is enough to find the file, which goes with it.
		var dataPath string
		if tic != nil {
			_, dataPath, _ = findTICFile(dir, tic)
		}
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("unusable TIC: %v", err), result)
		return
	}

	from, err := tic.FromAddress()
	if err != nil {
		_, dataPath, _ := findTICFile(dir, tic)
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("TIC has no usable From address (%q)", tic.From), result)
		return
	}
	link, ok := t.linkFor(from)
	if !ok {
		// Not from one of our links; another network's tosser may take it.
		result.noteSkipped(ticPath, fmt.Sprintf("%d:%d/%d", from.Zone, from.Net, from.Node))
		slog.Debug("skipping TIC from unknown link", "network", t.networkName, "tic", ticName, "from", tic.From)
		return
	}

	// Located before the remaining checks so that a TIC rejected for any
	// reason takes its file with it into the bad TIC directory.
	dataName, dataPath, found := findTICFile(dir, tic)

	// From is whatever the TIC says. Without a password to prove it, only a
	// TIC that came in over an authenticated session is believed: anyone
	// can put a file in the unsecured inbound.
	if link.TICPassword == "" && !secure {
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("TIC claiming to be from %s arrived in the unsecured inbound, "+
			"and the link has no tic_password to prove it", link.Address), result)
		return
	}
	if link.TICPassword != "" && !strings.EqualFold(tic.Password, link.TICPassword) {
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("TIC password from %s does not match the link's tic_password", link.Address), result)
		return
	}
	area, ok := areas[strings.ToUpper(tic.Area)]
	if !ok {
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("no file area is linked to file echo %s on network %s", tic.Area, t.networkName), result)
		return
	}

	if !found {
		if info, err := os.Stat(ticPath); err == nil && time.Since(info.ModTime()) > ticWaitFor {
			t.rejectTIC(ticPath, "", fmt.Sprintf("file %s never arrived", tic.File), result)
			return
		}
		// Claimed and waiting for its file: not unclaimed mail.
		result.WaitingTICs = append(result.WaitingTICs, ticPath)
		slog.Debug("TIC waiting for its file", "network", t.networkName, "tic", ticName, "file", tic.File)
		return
	}

	info, err := os.Stat(dataPath)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("stat %s: %v", dataPath, err))
		return
	}
	if tic.Size >= 0 && info.Size() != tic.Size {
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("%s is %d bytes, the TIC says %d", dataName, info.Size(), tic.Size), result)
		return
	}
	crc, err := ftn.FileCRC32(dataPath)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("read %s: %v", dataPath, err))
		return
	}
	if crc != tic.CRC {
		t.rejectTIC(ticPath, dataPath, fmt.Sprintf("%s has CRC %s, the TIC says %s",
			dataName, ftn.FormatCRC32(crc), ftn.FormatCRC32(tic.CRC)), result)
		return
	}

	outcome, err := t.deliverTICFile(area, dataName, dataPath, ftn.FormatCRC32(crc), info.Size(), tic)
	if err != nil {
		// Left in place: a failure here is local (disk, permissions), not
		// the TIC's fault, and the next toss retries it.
		result.Errors = append(result.Errors, fmt.Sprintf("deliver %s to file area %s: %v", dataName, area.Tag, err))
		return
	}
	if err := os.Remove(ticPath); err != nil {
		slog.Warn("failed to remove processed TIC", "path", ticPath, "error", err)
	}

	switch outcome {
	case ticDupe:
		result.FilesDuped++
		slog.Info("dropped duplicate file echo file", "network", t.networkName, "echo", tic.Area, "file", dataName, "area", area.Tag)
	default:
		result.FilesImported++
		slog.Info("received file echo file", "network", t.networkName, "echo", tic.Area, "file", dataName,
			"area", area.Tag, "size", info.Size(), "replaced", outcome == ticReplaced)
	}
}

type ticOutcome int

const (
	ticAdded ticOutcome = iota
	ticReplaced
	ticDupe
)

// deliverTICFile moves a checked file into its area and records it. A file
// the area already holds under the same name with the same CRC is a dupe and
// is dropped; with a different CRC it is a new version and replaces the old
// one, keeping its record (and download count).
//
// If the record cannot be written, everything is put back: the received file
// returns to the inbound, so the TIC still finds it on the next toss, and a
// file it replaced is restored.
func (t *Tosser) deliverTICFile(area file.FileArea, name, srcPath, crc string, size int64, tic *ftn.TIC) (ticOutcome, error) {
	areaDir, err := t.fileAreas.GetAreaUploadPath(area.ID)
	if err != nil {
		return 0, err
	}
	dstPath := filepath.Join(areaDir, name)

	var existing *file.FileRecord
	for _, r := range t.fileAreas.GetFilesForArea(area.ID) {
		if strings.EqualFold(r.Filename, name) {
			r := r
			existing = &r
			break
		}
	}

	if existing != nil {
		existingCRC := existing.CRC32
		if existingCRC == "" {
			// A file the area got some other way: compare its content.
			if c, err := ftn.FileCRC32(filepath.Join(areaDir, existing.Filename)); err == nil {
				existingCRC = ftn.FormatCRC32(c)
			}
		}
		if strings.EqualFold(existingCRC, crc) {
			if err := os.Remove(srcPath); err != nil {
				return 0, fmt.Errorf("remove duplicate: %w", err)
			}
			return ticDupe, nil
		}
		// The record keeps its own name (which may differ in case).
		dstPath = filepath.Join(areaDir, existing.Filename)
	}

	// Whatever is at the destination now (the old version, or a stray file
	// with no record) is set aside rather than overwritten, until the record
	// is safely written.
	backup, err := setAside(dstPath)
	if err != nil {
		return 0, err
	}
	restore := func() {
		if backup != "" {
			if err := os.Rename(backup, dstPath); err != nil {
				slog.Error("failed to restore the file a file echo delivery replaced", "path", dstPath, "backup", backup, "error", err)
			}
		}
	}
	if err := moveIntoArea(srcPath, dstPath); err != nil {
		restore()
		return 0, err
	}
	undo := func() {
		if err := moveFile(dstPath, srcPath); err != nil {
			slog.Error("failed to return a file echo file to the inbound after its record could not be written",
				"path", dstPath, "inbound", srcPath, "error", err)
			return // keep the backup out of the way rather than lose the new file
		}
		restore()
	}
	commit := func() {
		if backup != "" {
			if err := os.Remove(backup); err != nil {
				slog.Warn("failed to remove the previous version of a file echo file", "path", backup, "error", err)
			}
		}
	}

	uploader := tic.Origin
	if uploader == "" {
		uploader = tic.From
	}
	now := time.Now()
	if existing != nil {
		err := t.fileAreas.UpdateFileRecord(existing.ID, func(r *file.FileRecord) {
			r.Description = tic.Description()
			r.Size = size
			r.CRC32 = crc
			r.UploadedAt = now
			r.UploadedBy = uploader
			r.Reviewed = true // network-delivered content, as for a new record
		})
		if err != nil {
			undo()
			return 0, err
		}
		commit()
		return ticReplaced, nil
	}
	err = t.fileAreas.AddFileRecord(file.FileRecord{
		ID:          uuid.New(),
		AreaID:      area.ID,
		Filename:    name,
		Description: tic.Description(),
		Size:        size,
		UploadedAt:  now,
		UploadedBy:  uploader,
		// The sysop chose to carry the echo; its files need no review.
		Reviewed: true,
		CRC32:    crc,
	})
	if err != nil {
		undo()
		return 0, err
	}
	commit()
	return ticAdded, nil
}

// setAside renames an existing file at path to a hidden name beside it and
// returns that name, or "" when nothing is there.
func setAside(path string) (string, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return "", nil
	}
	backup := filepath.Join(filepath.Dir(path), ".tic-old-"+uuid.NewString())
	if err := os.Rename(path, backup); err != nil {
		return "", fmt.Errorf("set aside %s: %w", filepath.Base(path), err)
	}
	return backup, nil
}

// findTICFile finds the file a TIC describes in its directory, matching names
// case-insensitively: a TIC's File line is traditionally uppercase 8.3, and
// the file may arrive under its long name instead. A name that is not a plain
// file name is never matched, so a TIC cannot reach outside the inbound.
func findTICFile(dir string, tic *ftn.TIC) (name, path string, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", false
	}
	for _, want := range []string{tic.LongName, tic.File} {
		// Nor the TIC itself, nor a name that would land on the area's own
		// metadata.
		if want == "" || file.CheckFilename(want) != nil || isTICName(want) ||
			file.IsMetadataFile(want) {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && e.Type().IsRegular() && strings.EqualFold(e.Name(), want) {
				return e.Name(), filepath.Join(dir, e.Name()), true
			}
		}
	}
	return "", "", false
}

// rejectTIC moves an undeliverable TIC, and its file when it was found, to
// the bad TIC directory, and reports why.
func (t *Tosser) rejectTIC(ticPath, dataPath, reason string, result *TossResult) {
	dest := filepath.Join(t.paths.TempPath, BadTICDirName)
	if t.paths.TempPath == "" {
		result.Errors = append(result.Errors, fmt.Sprintf("%s: %s (left in place: no temp_path to move it to)", filepath.Base(ticPath), reason))
		return
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s: %s (left in place: %v)", filepath.Base(ticPath), reason, err))
		return
	}
	for _, p := range []string{dataPath, ticPath} {
		if p == "" {
			continue
		}
		if _, err := moveAside(p, dest); err != nil {
			slog.Warn("failed to move undeliverable TIC aside", "path", p, "error", err)
		}
	}
	result.FilesBad++
	result.Errors = append(result.Errors, fmt.Sprintf("%s: %s — moved to %s", filepath.Base(ticPath), reason, dest))
}

// linkFor returns this network's link with the given address, comparing zone,
// net and node as packet origins are.
func (t *Tosser) linkFor(addr ftn.Address) (linkConfig, bool) {
	for _, link := range t.config.Links {
		la, err := ftn.ParseAddress(link.Address)
		if err != nil {
			continue
		}
		if la.Zone == addr.Zone && la.Net == addr.Net && la.Node == addr.Node {
			return link, true
		}
	}
	return linkConfig{}, false
}

// moveAside moves path into dir, adding a numeric suffix rather than
// overwriting a file already there.
func moveAside(path, dir string) (string, error) {
	base := filepath.Base(path)
	target := filepath.Join(dir, base)
	for i := 1; ; i++ {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(dir, fmt.Sprintf("%s.%d", base, i))
	}
	return target, moveFile(path, target)
}

// moveIntoArea moves a received file to its place in a file area, replacing
// an older version there in one step.
func moveIntoArea(src, dst string) error {
	if err := atomicfile.Replace(src, dst); err == nil {
		return nil
	}
	// Most likely the inbound and the file areas are on different
	// filesystems: copy beside the destination, then replace.
	tmp, err := copyToTemp(src, filepath.Dir(dst))
	if err != nil {
		return err
	}
	if err := atomicfile.Replace(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

// moveFile renames src to dst, copying when they are on different filesystems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	tmp, err := copyToTemp(src, filepath.Dir(dst))
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

// copyToTemp copies src to a new temp file in dir and returns its path.
func copyToTemp(src, dir string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }() // read-only

	out, err := os.CreateTemp(dir, ".tic-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(out.Name())
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(out.Name())
		return "", err
	}
	if err := os.Chmod(out.Name(), 0644); err != nil {
		_ = os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}
