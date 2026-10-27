package configeditor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/configmerge"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// allConfigs holds all loaded configuration data.
type allConfigs struct {
	Server      config.ServerConfig
	Conferences []conference.Conference
	MsgAreas    []message.MessageArea
	// msgAreasBase is message_areas.json as last loaded or saved, the base
	// saveMsgAreas merges against so changes made on disk meanwhile (by the
	// BBS or helper) are kept. msgAreasBaseKnown is false for configs not
	// loaded from disk, which then save by plain overwrite.
	msgAreasBase      []message.MessageArea
	msgAreasBaseKnown bool
	// bases holds the other shared files as last loaded or saved, for the
	// same merge (saveMergedFile). Zero for configs not loaded from disk.
	bases     configBases
	FileAreas []file.FileArea
	Doors     map[string]config.DoorConfig
	Events    config.EventsConfig
	FTN       config.FTNConfig
	V3Net     config.V3NetConfig
	QWKNet    config.QWKNetConfig
	Protocols []transfer.ProtocolConfig
	Archivers archiver.Config
	LoginSeq  []config.LoginItem
	// ZipLab is ziplab.json alone. Its ArchiveTypes is left at the default:
	// ZipLab takes archive formats from archivers.json when the BBS loads it.
	ZipLab ziplab.Config
	// Theme is the menu set's theme.json, overlay first; themeSaved is it as
	// last loaded or saved, so an unedited theme is never written.
	Theme      config.ThemeConfig
	themeSaved config.ThemeConfig
}

// configBases are the snapshots saveMergedFile merges against: each shared
// config file as the editor last loaded or saved it.
type configBases struct {
	known       bool
	server      config.ServerConfig
	conferences []conference.Conference
	fileAreas   []file.FileArea
	ftn         config.FTNConfig
	v3net       config.V3NetConfig
}

// snapshotBases records ac's current values as the merge bases.
func (ac *allConfigs) snapshotBases() error {
	var err error
	b := configBases{known: true}
	if b.server, err = configmerge.Clone(ac.Server); err != nil {
		return err
	}
	if b.conferences, err = configmerge.Clone(ac.Conferences); err != nil {
		return err
	}
	if b.fileAreas, err = configmerge.Clone(ac.FileAreas); err != nil {
		return err
	}
	if b.ftn, err = configmerge.Clone(ac.FTN); err != nil {
		return err
	}
	if b.v3net, err = configmerge.Clone(ac.V3Net); err != nil {
		return err
	}
	ac.bases = b
	return nil
}

// loadAllConfigs loads all configuration files from the given directory.
// menuSetPath is the menu set the BBS runs, beside the configs directory.
func menuSetPath(configPath string) string {
	return filepath.Join(configPath, "..", "menus", "v3")
}

func loadAllConfigs(configPath string) (allConfigs, error) {
	var ac allConfigs
	var err error

	// Server config
	ac.Server, err = config.LoadServerConfig(configPath)
	if err != nil {
		return ac, fmt.Errorf("loading server config: %w", err)
	}

	// Conferences
	ac.Conferences, err = loadJSONSlice[conference.Conference](configPath, "conferences.json")
	if err != nil {
		return ac, fmt.Errorf("loading conferences: %w", err)
	}
	// Show conferences in the order the BBS uses, so the list matches what
	// callers see in the conference picker.
	normalizeConferenceOrder(ac.Conferences)

	// Message areas
	ac.MsgAreas, err = loadJSONSlice[message.MessageArea](configPath, "message_areas.json")
	if err != nil {
		return ac, fmt.Errorf("loading message areas: %w", err)
	}

	ac.msgAreasBase = message.CloneAreas(ac.MsgAreas)
	ac.msgAreasBaseKnown = true

	// Sort message areas by conference position, then by area position within
	// each conference so the TUI list groups areas under their conference.
	sortMsgAreasByConference(ac.MsgAreas, ac.Conferences)

	// File areas
	ac.FileAreas, err = loadJSONSlice[file.FileArea](configPath, "file_areas.json")
	if err != nil {
		return ac, fmt.Errorf("loading file areas: %w", err)
	}

	// Doors (LoadDoors returns an empty map, not an error, for a missing file).
	// A real load error must propagate: silently editing an empty list would
	// overwrite the existing doors.json on save.
	ac.Doors, err = config.LoadDoors(filepath.Join(configPath, "doors.json"))
	if err != nil {
		return ac, fmt.Errorf("loading doors: %w", err)
	}

	// Events
	ac.Events, err = config.LoadEventsConfig(configPath)
	if err != nil {
		return ac, fmt.Errorf("loading events: %w", err)
	}

	// FTN
	ac.FTN, err = config.LoadFTNConfig(configPath)
	if err != nil {
		return ac, fmt.Errorf("loading ftn: %w", err)
	}
	adoptBinkdIPFamilies(&ac.FTN, filepath.Join(configPath, "..", "data", "ftn", "binkd.conf"))

	// V3Net
	ac.V3Net, err = config.LoadV3NetConfig(configPath)
	if err != nil {
		return ac, fmt.Errorf("loading v3net: %w", err)
	}

	// QWK networks
	ac.QWKNet, err = config.LoadQWKNetConfig(configPath)
	if err != nil {
		return ac, fmt.Errorf("loading qwknet: %w", err)
	}

	// Protocols
	ac.Protocols, err = transfer.LoadProtocols(filepath.Join(configPath, "protocols.json"))
	if err != nil {
		// Protocols file may not exist
		ac.Protocols = nil
	}

	// Archivers
	ac.Archivers, err = archiver.LoadConfig(configPath)
	if err != nil {
		ac.Archivers = archiver.Config{}
	}

	// ZipLab. Read without the archivers.json merge, so a save writes back
	// only ZipLab's own settings. A parse error propagates for the same
	// reason as doors: editing defaults would overwrite the sysop's file.
	ac.ZipLab, err = ziplab.ReadConfig(configPath)
	if err != nil {
		return ac, fmt.Errorf("loading ziplab: %w", err)
	}

	// theme.json lives in the menu set, not in configs. A file that does not
	// parse stops the load for the same reason as doors.
	ac.Theme, err = config.LoadThemeConfig(menuSetPath(configPath))
	if err != nil {
		return ac, fmt.Errorf("loading theme: %w", err)
	}
	ac.themeSaved = ac.Theme

	// Login sequence
	ac.LoginSeq, err = loadJSONSlice[config.LoginItem](configPath, "login.json")
	if err != nil {
		ac.LoginSeq = nil
	}

	if err := ac.snapshotBases(); err != nil {
		return ac, fmt.Errorf("snapshotting configs: %w", err)
	}
	return ac, nil
}

// loadJSONSlice loads a JSON array from a file into a slice of T.
func loadJSONSlice[T any](configPath, filename string) ([]T, error) {
	filePath := filepath.Join(configPath, filename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}
	return result, nil
}

// saveMergedFile saves *cur to a config file other programs also write,
// merging in their changes made since *base was read (configmerge.SaveFile).
// On success *cur and *base become what is now on disk; on failure both are
// left alone, so a retry still sees the sysop's edits as edits. It returns a
// description of each merge conflict. Configs that were not loaded from
// disk (known false) are written as they are.
func saveMergedFile[T any](f configmerge.File[T], known bool, base, cur *T) ([]string, map[int]int, error) {
	if !known {
		lock, err := configmerge.Lock(f.Path)
		if err != nil {
			return nil, nil, err
		}
		defer lock.Release()
		data, err := f.Marshal(*cur)
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, atomicfile.WriteFile(f.Path, data, configmerge.FileMode(f.Path, 0o644))
	}
	merged, res, err := configmerge.SaveFile(f, *base, *cur)
	if err != nil {
		return nil, nil, err
	}
	newBase, err := configmerge.Clone(merged)
	if err != nil {
		return nil, nil, err
	}
	*cur, *base = merged, newBase
	var conflicts []string
	for _, c := range res.Conflicts {
		conflicts = append(conflicts, filepath.Base(f.Path)+": "+c.What)
	}
	return conflicts, res.Renumbered, nil
}

// remapConferenceIDs follows a conference save that renumbered conferences
// the editor added (another program took the ID first, or added the same
// conference under another ID): areas the editor put in such a conference
// move with it. An area counts as the editor's when its conference differs
// from its base copy, or it is new; areas already in that conference on
// disk belong to the other program's conference and stay.
func remapConferenceIDs(ac *allConfigs, renumbered map[int]int) {
	if len(renumbered) == 0 {
		return
	}
	msgBase := make(map[int]int, len(ac.msgAreasBase))
	for _, a := range ac.msgAreasBase {
		msgBase[a.ID] = a.ConferenceID
	}
	for i := range ac.MsgAreas {
		a := &ac.MsgAreas[i]
		if to, ok := renumbered[a.ConferenceID]; ok {
			if was, known := msgBase[a.ID]; !known || was != a.ConferenceID {
				a.ConferenceID = to
			}
		}
	}
	fileBase := make(map[int]int, len(ac.bases.fileAreas))
	for _, a := range ac.bases.fileAreas {
		fileBase[a.ID] = a.ConferenceID
	}
	for i := range ac.FileAreas {
		a := &ac.FileAreas[i]
		if to, ok := renumbered[a.ConferenceID]; ok {
			if was, known := fileBase[a.ID]; !known || was != a.ConferenceID {
				a.ConferenceID = to
			}
		}
	}
}

func marshalIndented[T any](v T) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// conferencesFile describes conferences.json, read the way loadAllConfigs
// reads it.
func conferencesFile(configPath string) configmerge.File[[]conference.Conference] {
	return configmerge.File[[]conference.Conference]{
		Path: filepath.Join(configPath, "conferences.json"),
		Parse: func(data []byte) ([]conference.Conference, error) {
			var confs []conference.Conference
			if err := json.Unmarshal(data, &confs); err != nil {
				return nil, err
			}
			normalizeConferenceOrder(confs)
			return confs, nil
		},
		Marshal: marshalIndented[[]conference.Conference],
	}
}

// fileAreasFile describes file_areas.json.
func fileAreasFile(configPath string) configmerge.File[[]file.FileArea] {
	return configmerge.File[[]file.FileArea]{
		Path: filepath.Join(configPath, "file_areas.json"),
		Parse: func(data []byte) ([]file.FileArea, error) {
			var areas []file.FileArea
			err := json.Unmarshal(data, &areas)
			return areas, err
		},
		Marshal: marshalIndented[[]file.FileArea],
		Fix:     fixRenumberedFileAreaPaths,
	}
}

// defaultFileAreaPath is the directory the editor gives a new file area.
func defaultFileAreaPath(id int) string {
	return fmt.Sprintf("files/area_%d", id)
}

// fixRenumberedFileAreaPaths moves a file area the merge renumbered off the
// generated directory named after its old ID, which the area that kept that
// ID also uses; sharing it would mix the two areas' files and metadata. A
// path the sysop set explicitly is left alone.
func fixRenumberedFileAreaPaths(areas []file.FileArea, res configmerge.Result) []file.FileArea {
	for from, to := range res.Renumbered {
		for i := range areas {
			if areas[i].ID == to && areas[i].Path == defaultFileAreaPath(from) {
				areas[i].Path = defaultFileAreaPath(to)
			}
		}
	}
	return areas
}

// ftnFile describes ftn.json, read the way loadAllConfigs reads it: link
// address families are adopted from binkd.conf, or the merge would read the
// editor's adopted values as edits.
func ftnFile(configPath string) configmerge.File[config.FTNConfig] {
	f := config.FTNConfigFile(configPath)
	parse := f.Parse
	f.Parse = func(data []byte) (config.FTNConfig, error) {
		cfg, err := parse(data)
		if err != nil {
			return cfg, err
		}
		adoptBinkdIPFamilies(&cfg, filepath.Join(configPath, "..", "data", "ftn", "binkd.conf"))
		return cfg, nil
	}
	return f
}

// saveJSONSlice writes a slice as a JSON array to a file.
func saveJSONSlice[T any](configPath, filename string, data []T) error {
	filePath := filepath.Join(configPath, filename)
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", filename, err)
	}
	if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}
	return nil
}

// saveDoors writes doors config back to disk as a JSON array (matching LoadDoors format).
func saveDoors(configPath string, doors map[string]config.DoorConfig) error {
	// LoadDoors reads a JSON array keyed by Code; preserve its original order.
	doorSlice := make([]config.DoorConfig, 0, len(doors))
	for _, d := range doors {
		doorSlice = append(doorSlice, d)
	}
	sort.Slice(doorSlice, func(i, j int) bool {
		if doorSlice[i].ConfigOrder != doorSlice[j].ConfigOrder {
			return doorSlice[i].ConfigOrder < doorSlice[j].ConfigOrder
		}
		return doorSlice[i].Code < doorSlice[j].Code
	})
	return saveJSONSlice(configPath, "doors.json", doorSlice)
}

// saveEventsConfig writes events config back to disk.
func saveEventsConfig(configPath string, cfg config.EventsConfig) error {
	filePath := filepath.Join(configPath, "events.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling events: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}
	return nil
}

// savedFTNOwnAddresses returns each network's own address as ftn.json holds
// it on disk, keyed by network name. It reads the file directly rather than
// through config.LoadFTNConfig, which logs every network and reassigns shared
// outbounds — neither belongs in a save. A missing or unreadable file gives an
// empty map: there is then no earlier address to compare with.
func savedFTNOwnAddresses(configPath string) map[string]string {
	addrs := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(configPath, "ftn.json"))
	if err != nil {
		return addrs
	}
	var saved config.FTNConfig
	if err := json.Unmarshal(data, &saved); err != nil {
		return addrs
	}
	for name, net := range saved.Networks {
		addrs[name] = net.OwnAddress
	}
	return addrs
}

// saveArchivers writes archivers config back to disk.
func saveArchivers(configPath string, cfg archiver.Config) error {
	filePath := filepath.Join(configPath, "archivers.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling archivers: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}
	return nil
}

// saveProtocols writes protocols config back to disk.
func saveProtocols(configPath string, protocols []transfer.ProtocolConfig) error {
	return saveJSONSlice(configPath, "protocols.json", protocols)
}

// saveMsgAreas writes message areas back to disk, returning a description of
// each merge conflict.
//
// The BBS and helper write message_areas.json too, and this editor rewrites
// it on every save, even one that changed nothing here. So the save holds the
// shared areas lock and merges what is on disk now with the editor's areas,
// against the list as the editor loaded it (message.MergeAreas): changes made
// elsewhere since are kept, and only a field changed in both places takes the
// editor's value. ac.MsgAreas and the merge base are updated to the result.
func saveMsgAreas(configPath string, ac *allConfigs) ([]string, error) {
	path := filepath.Join(configPath, "message_areas.json")
	if !ac.msgAreasBaseKnown {
		lock, err := message.LockAreasFile(path)
		if err != nil {
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		defer lock.Release()
		_, err = message.WriteAreasFile(path, ac.MsgAreas)
		return nil, err
	}

	var conflicts []string
	var merged []message.MessageArea
	err := message.UpdateAreasFile(path, func(disk []message.MessageArea) ([]message.MessageArea, bool, error) {
		res, err := message.MergeAreas(ac.msgAreasBase, ac.MsgAreas, disk)
		if err != nil {
			return nil, false, err
		}
		for _, c := range res.Conflicts {
			conflicts = append(conflicts, c.What)
		}
		merged = res.Areas
		// Nothing to add to what is on disk: leave the file, and a running
		// BBS, alone.
		diskData, err1 := message.MarshalAreas(disk)
		mergedData, err2 := message.MarshalAreas(merged)
		if err1 == nil && err2 == nil && bytes.Equal(diskData, mergedData) {
			return nil, false, nil
		}
		return merged, true, nil
	})
	if err != nil {
		// Leave the editor's areas and base as they were: recording the
		// unsaved merge as the base would make a retry read the sysop's edits
		// as unchanged and drop them in favour of the disk.
		return nil, err
	}
	ac.msgAreasBase = message.CloneAreas(merged)
	ac.MsgAreas = message.CloneAreas(merged)
	sortMsgAreasByConference(ac.MsgAreas, ac.Conferences)
	return conflicts, nil
}

// saveLoginSeq writes login sequence back to disk.
func saveLoginSeq(configPath string, items []config.LoginItem) error {
	return saveJSONSlice(configPath, "login.json", items)
}

// assignUnsetConferencePositions applies the BBS's load-time migration
// (conference.AssignUnsetPositions) to confs in place.
func assignUnsetConferencePositions(confs []conference.Conference) {
	ptrs := make([]*conference.Conference, len(confs))
	for i := range confs {
		ptrs[i] = &confs[i]
	}
	conference.AssignUnsetPositions(ptrs)
}

// normalizeConferenceOrder assigns positions to conferences that lack one,
// the same way the BBS does, then sorts by position (ties by ID) to match
// ConferenceManager.ListConferences.
func normalizeConferenceOrder(confs []conference.Conference) {
	assignUnsetConferencePositions(confs)
	sort.SliceStable(confs, func(i, j int) bool {
		if confs[i].Position != confs[j].Position {
			return confs[i].Position < confs[j].Position
		}
		return confs[i].ID < confs[j].ID
	})
}

// nextConferenceIDAndPosition returns the ID and position for a new
// conference appended to confs. Unset positions are assigned first, so the
// new conference lands after them rather than ahead of them.
func nextConferenceIDAndPosition(confs []conference.Conference) (id, pos int) {
	assignUnsetConferencePositions(confs)
	id = 1
	for _, c := range confs {
		if c.ID >= id {
			id = c.ID + 1
		}
		if c.Position > pos {
			pos = c.Position
		}
	}
	return id, pos + 1
}

// sortMsgAreasByConference sorts message areas by conference display position,
// then by area position within each conference. Areas whose conference is not
// found sort to the end.
func sortMsgAreasByConference(areas []message.MessageArea, confs []conference.Conference) {
	// Build lookup: conference ID → conference Position.
	confPos := make(map[int]int, len(confs))
	for _, c := range confs {
		confPos[c.ID] = c.Position
	}
	sort.SliceStable(areas, func(i, j int) bool {
		ci := confPos[areas[i].ConferenceID]
		cj := confPos[areas[j].ConferenceID]
		if ci == 0 {
			ci = math.MaxInt32
		}
		if cj == 0 {
			cj = math.MaxInt32
		}
		if ci != cj {
			return ci < cj
		}
		return areas[i].Position < areas[j].Position
	})
}

// confTagByID returns the conference Tag for the given conference ID,
// or "?" if not found.
func confTagByID(confs []conference.Conference, confID int) string {
	for _, c := range confs {
		if c.ID == confID {
			return c.Tag
		}
	}
	return "?"
}

// adoptBinkdIPFamilies reads into each link with no address family set the
// -4 / -6 its binkd.conf node line carries. Before the setting existed the
// only way to pin a hub to IPv4 was to add -4 to binkd.conf by hand, and the
// editor's save treats an unset family as auto and clears the flag; reading
// it in first shows the sysop what binkd is really doing and keeps the fix.
// Best-effort: an unreadable binkd.conf leaves the links as they are, and the
// save's sync, which reads the same file, will not touch it either.
func adoptBinkdIPFamilies(ftnCfg *config.FTNConfig, binkdPath string) {
	fams, err := ftn.ReadBinkdIPFamilies(binkdPath)
	if err != nil {
		slog.Warn("could not read binkd.conf address families", "path", binkdPath, "error", err)
		return
	}
	for netKey, nc := range ftnCfg.Networks {
		for i := range nc.Links {
			lnk := &nc.Links[i]
			if lnk.IPFamily != config.IPFamilyAuto {
				continue
			}
			if fam, ok := fams[strings.ToLower(lnk.Address+"@"+netKey)]; ok {
				lnk.IPFamily = fam
			}
		}
	}
}
