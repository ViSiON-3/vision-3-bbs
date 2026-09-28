package menu

// --- Record Structs (Modified for JSON Parsing) ---

// MenuRecord adapted for JSON parsing of .MNU files.
// Assumes JSON keys match the tags.
type MenuRecord struct {
	// Fields expected from JSON .MNU
	ClrScrBefore bool   `json:"CLR"`
	ClsScrBefore bool   `json:"CLS"`
	UsePrompt    bool   `json:"USEPROMPT"`
	Prompt1      string `json:"PROMPT1"`
	Prompt2      string `json:"PROMPT2"`
	Fallback     string `json:"FALLBACK"`
	ACS          string `json:"ACS"`
	Password     string `json:"PASS"`
	// ForceHotKey makes the menu take single-key input for every caller,
	// whatever their Hot Keys setting.
	ForceHotKey bool `json:"FORCEHOTKEY"`
}

// GetClrScrBefore reports whether the screen is cleared before the menu is
// shown; either the CLR or the CLS key in the menu file turns it on.
func (mr *MenuRecord) GetClrScrBefore() bool { return mr.ClrScrBefore || mr.ClsScrBefore }

// GetUsePrompt reports whether the menu displays its prompt (Prompt1/Prompt2)
// before reading a command; it reflects the USEPROMPT key in the menu file.
func (mr *MenuRecord) GetUsePrompt() bool { return mr.UsePrompt }
