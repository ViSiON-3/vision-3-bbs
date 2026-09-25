package ziplab

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
)

// StepConfig holds the setting every ZipLab pipeline step has: whether it runs.
type StepConfig struct {
	Enabled bool `json:"enabled"`
}

// VirusScanConfig extends StepConfig with the external scanner to run. It is
// the only step that runs a command of the sysop's choosing; the others are
// built in, or use the archiver commands from archivers.json.
type VirusScanConfig struct {
	StepConfig
	Command string   `json:"command,omitempty"`        // Scanner executable
	Args    []string `json:"args,omitempty"`           // Arguments ({WORKDIR} = extracted files, {FILE} = archive)
	Timeout int      `json:"timeoutSeconds,omitempty"` // Timeout in seconds (0 = default 60s)
}

// RemoveAdsConfig extends StepConfig with a patterns file.
type RemoveAdsConfig struct {
	StepConfig
	PatternsFile string `json:"patternsFile,omitempty"` // Path to REMOVE.TXT
}

// AddCommentConfig extends StepConfig with a comment file.
type AddCommentConfig struct {
	StepConfig
	CommentFile string `json:"commentFile,omitempty"` // Path to ZCOMMENT.TXT
}

// IncludeFileConfig extends StepConfig with the file to include.
type IncludeFileConfig struct {
	StepConfig
	FilePath string `json:"filePath,omitempty"` // Path to BBS.AD or similar
}

// StepsConfig holds all pipeline step configurations.
type StepsConfig struct {
	TestIntegrity StepConfig        `json:"testIntegrity"`
	ExtractToTemp StepConfig        `json:"extractToTemp"`
	VirusScan     VirusScanConfig   `json:"virusScan"`
	RemoveAds     RemoveAdsConfig   `json:"removeAds"`
	AddComment    AddCommentConfig  `json:"addComment"`
	IncludeFile   IncludeFileConfig `json:"includeFile"`
}

// ArchiveType defines how to handle a specific archive format.
type ArchiveType struct {
	Extension      string   `json:"extension"`                // e.g., ".zip", ".rar"
	Native         bool     `json:"native"`                   // true = handled by Go stdlib
	ExtractCommand string   `json:"extractCommand,omitempty"` // External extract command
	ExtractArgs    []string `json:"extractArgs,omitempty"`    // Extract arguments
	TestCommand    string   `json:"testCommand,omitempty"`    // Integrity test command
	TestArgs       []string `json:"testArgs,omitempty"`       // Test arguments
	AddCommand     string   `json:"addCommand,omitempty"`     // Add file command
	AddArgs        []string `json:"addArgs,omitempty"`        // Add arguments
	CommentCommand string   `json:"commentCommand,omitempty"` // Add comment command
	CommentArgs    []string `json:"commentArgs,omitempty"`    // Comment arguments
}

// Config holds the complete ZipLab configuration.
type Config struct {
	Enabled          bool        `json:"enabled"`
	RunOnUpload      bool        `json:"runOnUpload"`
	ScanFailBehavior string      `json:"scanFailBehavior"` // "delete" or "quarantine"
	QuarantinePath   string      `json:"quarantinePath,omitempty"`
	Steps            StepsConfig `json:"steps"`
	// ArchiveTypes is built from archivers.json when the config is loaded,
	// and is never read from or written to ziplab.json.
	ArchiveTypes []ArchiveType `json:"-"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:          true,
		RunOnUpload:      true,
		ScanFailBehavior: "delete",
		Steps: StepsConfig{
			TestIntegrity: StepConfig{Enabled: true},
			ExtractToTemp: StepConfig{Enabled: true},
			VirusScan:     VirusScanConfig{StepConfig: StepConfig{Enabled: false}, Command: "clamscan", Args: []string{"--stdout", "--no-summary", "{WORKDIR}"}, Timeout: 120},
			RemoveAds:     RemoveAdsConfig{StepConfig: StepConfig{Enabled: true}, PatternsFile: "REMOVE.TXT"},
			AddComment:    AddCommentConfig{StepConfig: StepConfig{Enabled: true}, CommentFile: "ZCOMMENT.TXT"},
			IncludeFile:   IncludeFileConfig{StepConfig: StepConfig{Enabled: true}, FilePath: "BBS.AD"},
		},
		ArchiveTypes: []ArchiveType{
			{Extension: ".zip", Native: true},
		},
	}
}

// LoadConfig loads ZipLab configuration from ziplab.json in the given config directory.
// It also loads the central archivers.json and merges enabled archiver definitions
// into the ZipLab ArchiveTypes list, so the pipeline knows how to handle each format.
// Returns defaults if the file doesn't exist.
func LoadConfig(configPath string) (Config, error) {
	cfg, err := ReadConfig(configPath)
	if err != nil {
		return cfg, err
	}

	// Merge archive types from the central archivers.json config.
	// This ensures all enabled archivers are available to the ZipLab pipeline.
	arcCfg, arcErr := archiver.LoadConfig(configPath)
	if arcErr != nil {
		slog.Warn("failed to load archivers.json, using ziplab defaults", "error", arcErr)
	} else {
		cfg.ArchiveTypes = archiverTypesFromConfig(arcCfg)
	}

	slog.Info("loaded ziplab config", "enabled", cfg.Enabled, "runOnUpload", cfg.RunOnUpload, "scanFailBehavior", cfg.ScanFailBehavior, "archiveTypes", len(cfg.ArchiveTypes))
	return cfg, nil
}

// ReadConfig reads ziplab.json from the given config directory over the
// defaults, without merging in archivers.json. It is what an editor loads, so
// that saving the result back writes only ZipLab's own settings. Returns
// defaults if the file doesn't exist.
func ReadConfig(configPath string) (Config, error) {
	filePath := filepath.Join(configPath, "ziplab.json")
	slog.Info("loading ziplab config", "path", filePath)

	cfg := DefaultConfig()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("ziplab.json not found, using defaults", "path", filePath)
			return cfg, nil
		}
		return cfg, fmt.Errorf("failed to read ziplab config %s: %w", filePath, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to parse ziplab config %s: %w", filePath, err)
	}
	return cfg, nil
}

// SaveConfig writes cfg to ziplab.json in the given config directory.
func SaveConfig(configPath string, cfg Config) error {
	filePath := filepath.Join(configPath, "ziplab.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling ziplab config: %w", err)
	}
	if err := atomicfile.WriteFile(filePath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}
	return nil
}

// archiverTypesFromConfig converts the central archiver.Config into ZipLab ArchiveType entries.
func archiverTypesFromConfig(cfg archiver.Config) []ArchiveType {
	var types []ArchiveType
	for _, a := range cfg.EnabledArchivers() {
		at := ArchiveType{
			Extension:      a.Extension,
			Native:         a.Native,
			ExtractCommand: a.Unpack.Command,
			ExtractArgs:    a.Unpack.Args,
			TestCommand:    a.Test.Command,
			TestArgs:       a.Test.Args,
			AddCommand:     a.AddFile.Command,
			AddArgs:        a.AddFile.Args,
			CommentCommand: a.Comment.Command,
			CommentArgs:    a.Comment.Args,
		}
		types = append(types, at)
		// Also add additional extensions (e.g., .lzh for lha archiver)
		for _, ext := range a.Extensions {
			if !strings.EqualFold(ext, a.Extension) {
				extra := at
				extra.Extension = ext
				types = append(types, extra)
			}
		}
	}
	if len(types) == 0 {
		// Fallback: always have native ZIP
		types = []ArchiveType{{Extension: ".zip", Native: true}}
	}
	return types
}

// IsArchiveSupported checks if a filename matches a configured archive type.
func (c *Config) IsArchiveSupported(filename string) bool {
	_, ok := c.GetArchiveType(filename)
	return ok
}

// GetArchiveType returns the ArchiveType config for a given filename.
func (c *Config) GetArchiveType(filename string) (ArchiveType, bool) {
	if filename == "" {
		return ArchiveType{}, false
	}
	lowerName := strings.ToLower(filename)
	for _, at := range c.ArchiveTypes {
		if strings.HasSuffix(lowerName, strings.ToLower(at.Extension)) {
			return at, true
		}
	}
	return ArchiveType{}, false
}
