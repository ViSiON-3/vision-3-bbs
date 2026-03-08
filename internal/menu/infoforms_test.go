package menu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTemplateFile(t *testing.T) {
	// Create a temp directory with a template file
	dir := t.TempDir()
	templatesDir := filepath.Join(dir, "data", "infoforms", "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Template with 2 fields, one with buffer length control
	template := "Name: *\r\n|B30;City: *\r\nThanks!"
	if err := os.WriteFile(filepath.Join(templatesDir, "form_1.txt"), []byte(template), 0644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	configPath := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// parseTemplateFile expects rootConfigPath where ../data/infoforms is the data dir
	tmpl, err := parseTemplateFile(configPath, 1)
	if err != nil {
		t.Fatalf("parseTemplateFile: %v", err)
	}

	// Should have 2 fields
	if len(tmpl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(tmpl.Fields))
	}

	// Should have 3 segments (before field 1, between fields, after field 2)
	if len(tmpl.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(tmpl.Segments))
	}

	// First field has no buffer limit
	if tmpl.Fields[0].MaxLen != 0 {
		t.Errorf("field 0 MaxLen = %d, want 0", tmpl.Fields[0].MaxLen)
	}

	// Second field has |B30; buffer limit
	if tmpl.Fields[1].MaxLen != 30 {
		t.Errorf("field 1 MaxLen = %d, want 30", tmpl.Fields[1].MaxLen)
	}

	// First segment should be "Name: "
	if tmpl.Segments[0] != "Name: " {
		t.Errorf("segment 0 = %q, want %q", tmpl.Segments[0], "Name: ")
	}

	// Trailing segment should be "\r\nThanks!"
	if tmpl.Segments[2] != "\r\nThanks!" {
		t.Errorf("segment 2 = %q, want %q", tmpl.Segments[2], "\r\nThanks!")
	}
}

func TestIsFormRequired(t *testing.T) {
	cfg := &InfoFormConfig{
		RequiredForms: "15",
	}

	if !isFormRequired(cfg, 1) {
		t.Error("form 1 should be required")
	}
	if isFormRequired(cfg, 2) {
		t.Error("form 2 should not be required")
	}
	if isFormRequired(cfg, 3) {
		t.Error("form 3 should not be required")
	}
	if isFormRequired(cfg, 4) {
		t.Error("form 4 should not be required")
	}
	if !isFormRequired(cfg, 5) {
		t.Error("form 5 should be required")
	}
}

func TestIsFormRequired_Empty(t *testing.T) {
	cfg := &InfoFormConfig{
		RequiredForms: "",
	}

	for i := 1; i <= 5; i++ {
		if isFormRequired(cfg, i) {
			t.Errorf("form %d should not be required with empty RequiredForms", i)
		}
	}
}

func TestTemplateExists(t *testing.T) {
	dir := t.TempDir()
	templatesDir := filepath.Join(dir, "data", "infoforms", "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	configPath := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create only form_1.txt
	if err := os.WriteFile(filepath.Join(templatesDir, "form_1.txt"), []byte("test*"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !templateExists(configPath, 1) {
		t.Error("form 1 template should exist")
	}
	if templateExists(configPath, 2) {
		t.Error("form 2 template should not exist")
	}
}

func TestHasCompletedForm(t *testing.T) {
	dir := t.TempDir()
	responsesDir := filepath.Join(dir, "data", "infoforms", "responses")
	if err := os.MkdirAll(responsesDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	configPath := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a response for user 42, form 1
	if err := os.WriteFile(filepath.Join(responsesDir, "42_1.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !hasCompletedForm(configPath, 42, 1) {
		t.Error("user 42 should have completed form 1")
	}
	if hasCompletedForm(configPath, 42, 2) {
		t.Error("user 42 should not have completed form 2")
	}
	if hasCompletedForm(configPath, 99, 1) {
		t.Error("user 99 should not have completed form 1")
	}
}

func TestLoadInfoFormConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No data/infoforms dir — should return defaults
	cfg, err := loadInfoFormConfig(configPath)
	if err != nil {
		t.Fatalf("loadInfoFormConfig: %v", err)
	}
	if cfg.Descriptions[0] != "New User Application" {
		t.Errorf("default desc[0] = %q, want %q", cfg.Descriptions[0], "New User Application")
	}
	if cfg.RequiredForms != "" {
		t.Errorf("default RequiredForms = %q, want empty", cfg.RequiredForms)
	}
}

func TestSaveAndLoadInfoFormResponse(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	resp := &InfoFormResponse{
		UserID:   1,
		Username: "testuser",
		Handle:   "TestHandle",
		FormNum:  2,
		Answers:  []string{"Answer1", "Answer2"},
	}

	if err := saveInfoFormResponse(configPath, resp); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadInfoFormResponse(configPath, 1, 2)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil response")
	}
	if loaded.Handle != "TestHandle" {
		t.Errorf("Handle = %q, want %q", loaded.Handle, "TestHandle")
	}
	if len(loaded.Answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(loaded.Answers))
	}
	if loaded.Answers[0] != "Answer1" {
		t.Errorf("answer[0] = %q, want %q", loaded.Answers[0], "Answer1")
	}
}

func TestExpandInfoformCodes(t *testing.T) {
	result := expandInfoformCodes("Welcome to BBS v|VN!")
	if result == "Welcome to BBS v|VN!" {
		t.Error("expected |VN to be replaced with version number")
	}
	// Should not contain the literal |VN anymore
	if result == "" {
		t.Error("result should not be empty")
	}
}
