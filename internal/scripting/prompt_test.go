package scripting

import "testing"

func TestPromptAnswer(t *testing.T) {
	t.Run("kind is the configured prompt", func(t *testing.T) {
		h := newHarness(t, harnessOpts{prompt: PromptYesNo})
		if got := h.eval("v3.prompt.kind").String(); got != "yesno" {
			t.Errorf("kind = %q", got)
		}
	})

	t.Run("kind is empty outside a prompt", func(t *testing.T) {
		h := newHarness(t, harnessOpts{})
		if got := h.eval("v3.prompt.kind").String(); got != "" {
			t.Errorf("kind = %q", got)
		}
		if _, set := h.eng.Answer(); set {
			t.Error("answer set before the script answered")
		}
	})

	t.Run("boolean answer", func(t *testing.T) {
		h := newHarness(t, harnessOpts{prompt: PromptYesNo})
		h.mustRun(`v3.prompt.answer(true)`)
		v, set := h.eng.Answer()
		if !set || v != true {
			t.Errorf("Answer() = %#v, %v", v, set)
		}
	})

	t.Run("string answer", func(t *testing.T) {
		h := newHarness(t, harnessOpts{prompt: PromptYesNo})
		h.mustRun(`v3.prompt.answer("No")`)
		v, set := h.eng.Answer()
		if !set || v != "No" {
			t.Errorf("Answer() = %#v, %v", v, set)
		}
	})

	t.Run("later answer replaces the earlier", func(t *testing.T) {
		h := newHarness(t, harnessOpts{prompt: PromptYesNo})
		h.mustRun(`v3.prompt.answer(true); v3.prompt.answer(false)`)
		if v, set := h.eng.Answer(); !set || v != false {
			t.Errorf("Answer() = %#v, %v", v, set)
		}
	})

	t.Run("answer with no value", func(t *testing.T) {
		h := newHarness(t, harnessOpts{prompt: PromptPause})
		h.mustRun(`v3.prompt.answer()`)
		if v, set := h.eng.Answer(); !set || v != nil {
			t.Errorf("Answer() = %#v, %v", v, set)
		}
	})

	t.Run("answer survives exit", func(t *testing.T) {
		h := newHarness(t, harnessOpts{prompt: PromptYesNo})
		h.mustRun(`v3.prompt.answer(true); exit(); v3.prompt.answer(false)`)
		if v, set := h.eng.Answer(); !set || v != true {
			t.Errorf("Answer() = %#v, %v", v, set)
		}
	})

	t.Run("answer outside a prompt is recorded but unused", func(t *testing.T) {
		h := newHarness(t, harnessOpts{})
		h.mustRun(`v3.prompt.answer(1)`)
		if _, set := h.eng.Answer(); !set {
			t.Error("answer not recorded")
		}
	})
}
