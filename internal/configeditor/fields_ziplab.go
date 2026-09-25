package configeditor

import (
	"os/exec"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// Field definitions for the ZipLab Upload Processing inner menu, which edits
// ziplab.json. Archive formats are not here: ZipLab takes them from
// archivers.json, edited under Archivers.

// zipLabMenuItems returns the ZipLab inner menu.
func zipLabMenuItems() []sysConfigMenuItem {
	return []sysConfigMenuItem{
		{Label: "General", Build: func(m *Model) []fieldDef { return zipLabFieldsGeneral(&m.configs.ZipLab) }},
		{Label: "Pipeline Steps", Build: func(m *Model) []fieldDef { return zipLabFieldsSteps(&m.configs.ZipLab) }},
		{Label: "Virus Scan", Build: func(m *Model) []fieldDef { return zipLabFieldsVirusScan(&m.configs.ZipLab) }},
	}
}

// zipLabGeneralNote explains a General setting that will not do what it
// appears to, or "" when there is nothing to say.
func zipLabGeneralNote(cfg *ziplab.Config) string {
	switch {
	case !cfg.Enabled || !cfg.RunOnUpload:
		return "Uploads are not processed"
	case cfg.ScanFailBehavior == "quarantine" && cfg.QuarantinePath == "":
		return "No quarantine path: infected uploads are deleted"
	case !cfg.Steps.VirusScan.Enabled:
		return "Virus scan is off, so nothing is quarantined"
	}
	return ""
}

// zipLabStepsNote explains a step that is enabled but cannot run, or "".
func zipLabStepsNote(cfg *ziplab.Config) string {
	if cfg.Steps.ExtractToTemp.Enabled {
		return ""
	}
	switch {
	case cfg.Steps.VirusScan.Enabled && cfg.Steps.RemoveAds.Enabled:
		return "Extract is off: virus scan and DIZ/ads are skipped"
	case cfg.Steps.VirusScan.Enabled:
		return "Extract is off: virus scan is skipped"
	case cfg.Steps.RemoveAds.Enabled:
		return "Extract is off: DIZ/ads step is skipped"
	}
	return ""
}

// zipLabScanNote warns when the virus scan is on but its scanner cannot be
// found. The pipeline treats a scanner that fails to run as a detection, so
// every upload would be deleted or quarantined.
func zipLabScanNote(vs *ziplab.VirusScanConfig) string {
	if !vs.Enabled {
		return ""
	}
	if vs.Command == "" {
		return "No command set: every upload will fail the scan"
	}
	if _, err := exec.LookPath(vs.Command); err != nil {
		return "Scanner not found here: uploads will fail the scan"
	}
	return ""
}

// zipLabFieldsGeneral returns fields for the General sub-screen.
func zipLabFieldsGeneral(cfg *ziplab.Config) []fieldDef {
	return []fieldDef{
		{
			Label: "Enabled", Help: "Master switch for ZipLab archive processing", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.Enabled) },
			Set: func(val string) error { cfg.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Run on Upload", Help: "Process archives as users upload them", Type: ftYesNo, Col: 3, Row: 2, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.RunOnUpload) },
			Set: func(val string) error { cfg.RunOnUpload = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Scan Failure", Help: "What happens to an upload that fails the virus scan", Type: ftLookup, Col: 3, Row: 4, Width: 10,
			Get: func() string {
				if cfg.ScanFailBehavior == "quarantine" {
					return "quarantine"
				}
				return "delete" // the pipeline deletes on anything else
			},
			Set: func(val string) error { cfg.ScanFailBehavior = val; return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "delete", Display: "delete - remove the upload"},
					{Value: "quarantine", Display: "quarantine - move it to the quarantine path"},
				}
			},
		},
		{
			Label: "Quarantine Path", Help: "Where failed uploads go (absolute, or relative to BBS root)", Type: ftString, Col: 3, Row: 5, Width: 40,
			Get: func() string { return cfg.QuarantinePath },
			Set: func(val string) error { cfg.QuarantinePath = val; return nil },
		},
		{
			Label: "Note", Help: "Settings that will not take effect as they stand", Type: ftDisplay, Col: 3, Row: 7, Width: 48,
			Get: func() string { return zipLabGeneralNote(cfg) },
		},
	}
}

// zipLabFieldsSteps returns fields for the Pipeline Steps sub-screen. The
// steps are listed in the order the pipeline runs them.
func zipLabFieldsSteps(cfg *ziplab.Config) []fieldDef {
	st := &cfg.Steps
	return []fieldDef{
		{
			Label: "Test Integrity", Help: "Test the archive; a corrupt upload is rejected", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(st.TestIntegrity.Enabled) },
			Set: func(val string) error { st.TestIntegrity.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Extract", Help: "Extract to a temp dir (virus scan and DIZ/ads need it)", Type: ftYesNo, Col: 3, Row: 2, Width: 1,
			Get: func() string { return uitext.BoolToYN(st.ExtractToTemp.Enabled) },
			Set: func(val string) error { st.ExtractToTemp.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Virus Scan", Help: "Run the scanner set on the Virus Scan screen", Type: ftYesNo, Col: 3, Row: 3, Width: 1,
			Get: func() string { return uitext.BoolToYN(st.VirusScan.Enabled) },
			Set: func(val string) error { st.VirusScan.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "DIZ / Remove Ads", Help: "Use FILE_ID.DIZ as description; strip listed ad files", Type: ftYesNo, Col: 3, Row: 4, Width: 1,
			Get: func() string { return uitext.BoolToYN(st.RemoveAds.Enabled) },
			Set: func(val string) error { st.RemoveAds.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Patterns File", Help: "Ad filenames to strip, one per line (in ziplab dir)", Type: ftString, Col: 3, Row: 5, Width: 40,
			Get: func() string { return st.RemoveAds.PatternsFile },
			Set: func(val string) error { st.RemoveAds.PatternsFile = val; return nil },
		},
		{
			Label: "Add Comment", Help: "Set the archive comment from the comment file", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return uitext.BoolToYN(st.AddComment.Enabled) },
			Set: func(val string) error { st.AddComment.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Comment File", Help: "Archive comment text (in ziplab dir)", Type: ftString, Col: 3, Row: 7, Width: 40,
			Get: func() string { return st.AddComment.CommentFile },
			Set: func(val string) error { st.AddComment.CommentFile = val; return nil },
		},
		{
			Label: "Include File", Help: "Add a file, such as a BBS ad, to every archive", Type: ftYesNo, Col: 3, Row: 8, Width: 1,
			Get: func() string { return uitext.BoolToYN(st.IncludeFile.Enabled) },
			Set: func(val string) error { st.IncludeFile.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "File to Include", Help: "File added to each archive (in ziplab dir)", Type: ftString, Col: 3, Row: 9, Width: 40,
			Get: func() string { return st.IncludeFile.FilePath },
			Set: func(val string) error { st.IncludeFile.FilePath = val; return nil },
		},
		{
			Label: "Note", Help: "Steps that are enabled but will not run", Type: ftDisplay, Col: 3, Row: 11, Width: 48,
			Get: func() string { return zipLabStepsNote(cfg) },
		},
	}
}

// zipLabFieldsVirusScan returns fields for the Virus Scan sub-screen.
func zipLabFieldsVirusScan(cfg *ziplab.Config) []fieldDef {
	vs := &cfg.Steps.VirusScan
	return []fieldDef{
		{
			Label: "Enabled", Help: "Scan extracted files; a detection fails the upload", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(vs.Enabled) },
			Set: func(val string) error { vs.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Command", Help: "Scanner program; a non-zero exit fails the upload", Type: ftString, Col: 3, Row: 2, Width: 40,
			Get: func() string { return vs.Command },
			Set: func(val string) error { vs.Command = val; return nil },
		},
		{
			Label: "Args", Help: "JSON array or words; {WORKDIR}=extracted, {FILE}=archive", Type: ftString, Col: 3, Row: 3, Width: 48,
			Get: func() string { return joinArgs(vs.Args) },
			Set: func(val string) error {
				args, err := splitArgs(val)
				if err != nil {
					return err
				}
				vs.Args = args
				return nil
			},
		},
		{
			Label: "Timeout Secs", Help: "Seconds before the scan counts as failed (0 = 60)", Type: ftInteger, Col: 3, Row: 4, Width: 4, Min: 0, Max: 3600,
			Get: func() string { return strconv.Itoa(vs.Timeout) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				vs.Timeout = n
				return nil
			},
		},
		{
			Label: "Note", Help: "Problems that would make every scan fail", Type: ftDisplay, Col: 3, Row: 6, Width: 48,
			Get: func() string { return zipLabScanNote(vs) },
		},
	}
}
