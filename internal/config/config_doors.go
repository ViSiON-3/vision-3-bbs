package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DoorConfig defines the configuration for a single external door program.
type DoorConfig struct {
	Code                string            `json:"code"`                            // Unique internal code used in DOOR:CODE commands (uppercase slug)
	Name                string            `json:"name"`                            // Display label shown to users (free-form, case preserved)
	WorkingDirectory    string            `json:"working_directory,omitempty"`     // Directory to run the command in (optional)
	Commands            []string          `json:"commands,omitempty"`              // Commands to execute (native: [0]=executable, [1:]=args; DOS: batch lines)
	DropfileType        string            `json:"dropfile_type,omitempty"`         // Type of dropfile ("DOOR.SYS", "CHAIN.TXT", "NONE") (optional, defaults to NONE)
	DropfileLocation    string            `json:"dropfile_location,omitempty"`     // Where to write dropfile: "startup" (working dir, default) or "node" (per-node temp dir)
	DropfileCase        string            `json:"dropfile_case,omitempty"`         // Dropfile filename case: "upper" (default) or "lower"
	IOMode              string            `json:"io_mode,omitempty"`               // I/O handling ("STDIO", "SOCKET") (optional, defaults to STDIO)
	RequiresRawTerminal bool              `json:"requires_raw_terminal,omitempty"` // Whether the BBS should attempt to put the terminal in raw mode (optional, defaults to false)
	UseShell            bool              `json:"use_shell,omitempty"`             // Wrap command in /bin/sh -c (Linux) or cmd /c (Windows)
	SingleInstance      bool              `json:"single_instance,omitempty"`       // Only allow one node to run this door at a time
	MinAccessLevel      int               `json:"min_access_level,omitempty"`      // Minimum user access level required (0 = no restriction)
	CleanupCommand      string            `json:"cleanup_command,omitempty"`       // Command to run after door exits (optional)
	CleanupArgs         []string          `json:"cleanup_args,omitempty"`          // Arguments for cleanup command (supports placeholders)
	EnvironmentVars     map[string]string `json:"environment_variables,omitempty"` // Additional environment variables (optional)
	// Script door fields
	Type         string   `json:"type,omitempty"`          // "synchronet_js", "v3_script", or empty (legacy native/DOS)
	Script       string   `json:"script,omitempty"`        // Main JS file to execute (relative to working_directory)
	LibraryPaths []string `json:"library_paths,omitempty"` // Search paths for load()/require()
	Args         []string `json:"args,omitempty"`          // Script arguments (available as argv in JS)
	ExecDir      string   `json:"exec_dir,omitempty"`      // Synchronet exec directory (system.exec_dir)
	// DOS door fields
	IsDOS        bool   `json:"is_dos,omitempty"`        // true = DOS door launched via a DOS emulator
	DriveCPath   string `json:"drive_c_path,omitempty"`  // Path to drive_c directory (default: ~/.dosemu/drive_c)
	DOSEmulator  string `json:"dos_emulator,omitempty"`  // Emulator to use: "auto" (default) or "dosemu"
	FossilDriver string `json:"fossil_driver,omitempty"` // DOS FOSSIL driver command (e.g. "C:\\UTILS\\X00.EXE eliminate")
	// dosemu2-specific fields (Linux x86 only)
	DosemuConfig string `json:"dosemu_config,omitempty"` // Path to custom .dosemurc (optional)
}

// doorCodeRE validates a door code after uppercasing: the code keys the door
// registry and appears in DOOR:CODE menu commands, so it must be a short slug
// with no spaces or punctuation.
var doorCodeRE = regexp.MustCompile(`^[A-Z0-9_-]{1,16}$`)

// NormalizeDoorCode trims and uppercases a door code and validates it against
// the required format. Both the loader and the config editor use this, so the
// contract is enforced identically for hand-edited and TUI-edited configs.
func NormalizeDoorCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !doorCodeRE.MatchString(code) {
		return "", fmt.Errorf("door code must be 1-16 chars: A-Z, 0-9, _ or -")
	}
	return code, nil
}

// LoadDoors loads the door configuration from the specified file path.
func LoadDoors(filePath string) (map[string]DoorConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// If the file doesn't exist, return an empty map and no error, as doors are optional.
		if os.IsNotExist(err) {
			return make(map[string]DoorConfig), nil
		}
		return nil, fmt.Errorf("failed to read doors file %s: %w", filePath, err)
	}

	var doors []DoorConfig
	err = json.Unmarshal(data, &doors)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal doors JSON from %s: %w", filePath, err)
	}

	// Key by uppercased Code: menu lookups uppercase the door code before
	// consulting the registry, so codes must normalize or the door is
	// unreachable. Name is a display label and is left untouched.
	doorMap := make(map[string]DoorConfig)
	for _, door := range doors {
		code, err := NormalizeDoorCode(door.Code)
		if err != nil {
			return nil, fmt.Errorf("door %q in %s: %w", door.Name, filePath, err)
		}
		if _, exists := doorMap[code]; exists {
			return nil, fmt.Errorf("duplicate door code found in %s: %s", filePath, door.Code)
		}
		door.Code = code
		doorMap[code] = door
	}

	return doorMap, nil
}
