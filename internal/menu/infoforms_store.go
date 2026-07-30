package menu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InfoFormConfig holds sysop-configured infoform settings.
// Maps to V2's Cfg.InfoformStr[1..5], Cfg.Infoformlvl[1..5], Cfg.RequiredForms.
type InfoFormConfig struct {
	Descriptions  [5]string `json:"descriptions"`   // Form descriptions (V2: InfoformStr)
	MinLevels     [5]int    `json:"min_levels"`     // Minimum access level per form (V2: Infoformlvl)
	RequiredForms string    `json:"required_forms"` // Which forms are required, e.g. "15" = forms 1 and 5
}

// InfoFormResponse holds a user's completed answers for a specific form.
// Maps to V2's FORMS.TXT/FORMS.MAP entries.
type InfoFormResponse struct {
	UserID      int       `json:"user_id"`
	Handle      string    `json:"handle"`
	FormNum     int       `json:"form_num"`
	FilledOutAt time.Time `json:"filled_out_at"`
	Answers     []string  `json:"answers"` // One entry per * field in the template
}

var infoformsMu sync.Mutex

// infoformsDataDir returns the path to the infoforms data directory.
func infoformsDataDir(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "infoforms")
}

// infoformsConfigPath returns the path to the infoforms config file.
func infoformsConfigPath(rootConfigPath string) string {
	return filepath.Join(infoformsDataDir(rootConfigPath), "config.json")
}

// infoformsTemplatePath returns the path to a form template file.
func infoformsTemplatePath(rootConfigPath string, formNum int) string {
	return filepath.Join(infoformsDataDir(rootConfigPath), "templates", fmt.Sprintf("form_%d.txt", formNum))
}

// infoformsResponsePath returns the path to a user's response file.
func infoformsResponsePath(rootConfigPath string, userID int, formNum int) string {
	return filepath.Join(infoformsDataDir(rootConfigPath), "responses", fmt.Sprintf("%d_%d.json", userID, formNum))
}

// loadInfoFormConfig loads the infoforms configuration.
func loadInfoFormConfig(rootConfigPath string) (*InfoFormConfig, error) {
	data, err := os.ReadFile(infoformsConfigPath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults matching V2 (CONFIG.PAS:73-78)
			return &InfoFormConfig{
				Descriptions:  [5]string{"New User Application", "", "", "", ""},
				MinLevels:     [5]int{0, 0, 0, 0, 0},
				RequiredForms: "",
			}, nil
		}
		return nil, fmt.Errorf("read infoforms config: %w", err)
	}
	var cfg InfoFormConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse infoforms config: %w", err)
	}
	return &cfg, nil
}

// loadInfoFormResponse loads a user's response for a specific form.
func loadInfoFormResponse(rootConfigPath string, userID int, formNum int) (*InfoFormResponse, error) {
	data, err := os.ReadFile(infoformsResponsePath(rootConfigPath, userID, formNum))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No response yet
		}
		return nil, fmt.Errorf("read infoform response: %w", err)
	}
	var resp InfoFormResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse infoform response: %w", err)
	}
	return &resp, nil
}

// saveInfoFormResponse saves a user's response for a specific form.
// Uses temp file + rename to prevent torn reads by concurrent sessions.
// Note: os.Rename is atomic on Unix/POSIX but not guaranteed atomic on Windows.
// On Windows, concurrent readers may briefly see a missing file during the rename.
func saveInfoFormResponse(rootConfigPath string, resp *InfoFormResponse) error {
	data, err := json.MarshalIndent(resp, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal infoform response: %w", err)
	}
	fp := infoformsResponsePath(rootConfigPath, resp.UserID, resp.FormNum)
	dir := filepath.Dir(fp)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create infoforms response directory: %w", err)
	}
	// Write to temp file then rename for atomic update
	tmp, err := os.CreateTemp(dir, ".infoform-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()        // cleanup on error path
		_ = os.Remove(tmpName) // cleanup on error path
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()        // cleanup on error path
		_ = os.Remove(tmpName) // cleanup on error path
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName) // cleanup on error path
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, fp); err != nil {
		_ = os.Remove(tmpName) // cleanup on error path
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// deleteInfoFormResponse deletes a user's response for a specific form.
func deleteInfoFormResponse(rootConfigPath string, userID int, formNum int) error {
	fp := infoformsResponsePath(rootConfigPath, userID, formNum)
	err := os.Remove(fp)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete infoform response: %w", err)
	}
	return nil
}

// hasCompletedForm checks if a user has completed a specific form.
// Uses file existence check — no User struct field needed.
func hasCompletedForm(rootConfigPath string, userID int, formNum int) bool {
	_, err := os.Stat(infoformsResponsePath(rootConfigPath, userID, formNum))
	return err == nil
}

// templateExists checks if a form template file exists.
func templateExists(rootConfigPath string, formNum int) bool {
	_, err := os.Stat(infoformsTemplatePath(rootConfigPath, formNum))
	return err == nil
}

// isFormRequired checks if a form number is in the required forms string.
// V2 pattern: RequiredForms is a string like "15" meaning forms 1 and 5 required.
func isFormRequired(cfg *InfoFormConfig, formNum int) bool {
	return strings.Contains(cfg.RequiredForms, strconv.Itoa(formNum))
}
