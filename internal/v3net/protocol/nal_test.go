package protocol

import (
	"strings"
	"testing"
)

func TestValidateAreaTag_Valid(t *testing.T) {
	for _, tag := range []string{"fel.general", "fn.bbs-talk", "a.b", "12345678.a23456789012345678901234"} {
		if err := ValidateAreaTag(tag); err != nil {
			t.Errorf("ValidateAreaTag(%q) = %v, want nil", tag, err)
		}
	}
}

func TestValidateAreaTag_MissingPeriodExplainsFormat(t *testing.T) {
	err := ValidateAreaTag("felstuff")
	if err == nil {
		t.Fatal("expected error for tag without period")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing a period") {
		t.Errorf("error should say the period is missing, got %q", msg)
	}
	if !strings.Contains(msg, `"fel.general"`) {
		t.Errorf("error should include an example tag, got %q", msg)
	}
}

func TestValidateAreaTag_InvalidFormatDescribesRulesInWords(t *testing.T) {
	for _, tag := range []string{"FEL.stuff", "toolongprefix.general", "fel.has spaces", "fel."} {
		err := ValidateAreaTag(tag)
		if err == nil {
			t.Fatalf("expected error for %q", tag)
		}
		msg := err.Error()
		if !strings.Contains(msg, `"fel.general"`) {
			t.Errorf("error for %q should include an example tag, got %q", tag, msg)
		}
		if strings.Contains(msg, "^[a-z0-9]") {
			t.Errorf("error for %q should not expose the raw regexp, got %q", tag, msg)
		}
	}
}
