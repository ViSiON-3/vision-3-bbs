package menu

// --- Record Structs (Modified for JSON Parsing) ---

// MenuRecord adapted for JSON parsing of .MNU files.
// Assumes JSON keys match the tags.
type MenuRecord struct {
	Title string `json:"TITLE"`
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
	// Script names a v3_script door that draws this menu and answers its
	// prompt, in place of the menu's screen and prompt (see menu_script.go):
	// the door's code, then any words to pass it as arguments ("SUBMENU
	// blue"). Empty takes the menu set's menuScript from theme.json; "NONE"
	// turns that default off for this menu.
	Script string `json:"SCRIPT,omitempty"`
}

// GetClrScrBefore reports whether the screen is cleared before the menu is
// shown; either the CLR or the CLS key in the menu file turns it on.
func (mr *MenuRecord) GetClrScrBefore() bool { return mr.ClrScrBefore || mr.ClsScrBefore }

// GetUsePrompt reports whether the menu displays its prompt (Prompt1/Prompt2)
// before reading a command; it reflects the USEPROMPT key in the menu file.
func (mr *MenuRecord) GetUsePrompt() bool { return mr.UsePrompt }
