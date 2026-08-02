package protocol

import (
	"strings"
	"testing"
)

// maxTagErrorLen is the narrowest status row that displays these errors:
// the hub area insert dialog is 60 columns wide with 59 usable characters.
const maxTagErrorLen = 59

func TestValidateAreaTag_Valid(t *testing.T) {
	for _, tag := range []string{"fel.general", "fn.bbs-talk", "a.b", "12345678.a23456789012345678901234"} {
		if err := ValidateAreaTag(tag); err != nil {
			t.Errorf("ValidateAreaTag(%q) = %v, want nil", tag, err)
		}
	}
}

func TestValidateAreaTag_MissingPeriod(t *testing.T) {
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

func TestValidateAreaTag_PrefixTooLong(t *testing.T) {
	err := ValidateAreaTag("toolongprefix.general")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "prefix too long") || !strings.Contains(err.Error(), "8") {
		t.Errorf("error should say the prefix is too long (max 8), got %q", err.Error())
	}
}

func TestValidateAreaTag_NameTooLong(t *testing.T) {
	err := ValidateAreaTag("fel.this-name-is-far-too-long-to-be-valid")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "name too long") || !strings.Contains(err.Error(), "24") {
		t.Errorf("error should say the name is too long (max 24), got %q", err.Error())
	}
}

func TestValidateAreaTag_OtherwiseDescribesFormat(t *testing.T) {
	for _, tag := range []string{"FEL.stuff", "fel.has spaces", "fel.", ".general", "fel.stuff.extra"} {
		err := ValidateAreaTag(tag)
		if err == nil {
			t.Fatalf("expected error for %q", tag)
		}
		msg := err.Error()
		if !strings.Contains(msg, "lowercase") {
			t.Errorf("error for %q should mention lowercase, got %q", tag, msg)
		}
		if !strings.Contains(msg, `"fel.general"`) {
			t.Errorf("error for %q should include an example tag, got %q", tag, msg)
		}
	}
}

func TestValidateAreaTag_ErrorsFitStatusRowAndHideRegexp(t *testing.T) {
	for _, tag := range []string{
		"felstuff", "toolongprefix.general", "fel.this-name-is-far-too-long-to-be-valid",
		"FEL.stuff", "fel.has spaces", "fel.", ".general",
	} {
		err := ValidateAreaTag(tag)
		if err == nil {
			t.Fatalf("expected error for %q", tag)
		}
		msg := err.Error()
		if n := len([]rune(msg)); n > maxTagErrorLen {
			t.Errorf("error for %q is %d chars, must fit %d-char status row: %q", tag, n, maxTagErrorLen, msg)
		}
		if strings.Contains(msg, "^[a-z0-9]") {
			t.Errorf("error for %q should not expose the raw regexp, got %q", tag, msg)
		}
	}
}
