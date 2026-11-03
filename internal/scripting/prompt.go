package scripting

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// Prompt kinds a script can answer, set in ScriptConfig.Prompt by the BBS
// when a strings.json value runs the script in place of a prompt (#646).
// The script reads the kind from v3.prompt.kind.
const (
	// PromptYesNo is a Yes/No question. The answer is a boolean, or one of
	// the strings "y", "yes", "n" or "no" in any case.
	PromptYesNo = "yesno"
	// PromptPause is a "press Enter" pause. Any answer, or none, ends it.
	PromptPause = "pause"
	// PromptMenu is a menu's command prompt. The answer is a string:
	// "GOTO:NAME" goes to that menu, "LOGOFF" ends the call, and anything
	// else is taken as the command line the caller would have typed.
	PromptMenu = "menu"
)

// registerPrompt creates the v3.prompt object: the kind of prompt the script
// stands in for, and answer(value), which hands an answer back to the BBS so
// it skips reading one from the caller.
func registerPrompt(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	jsutil.Set(obj, "kind", eng.cfg.Prompt)

	// answer(value) — set the prompt's answer. With no argument the prompt
	// is answered with nothing, which ends a pause. Calling it again
	// replaces the earlier answer.
	jsutil.Set(obj, "answer", func(call goja.FunctionCall) goja.Value {
		var value any
		if len(call.Arguments) > 0 {
			value = call.Arguments[0].Export()
		}
		if eng.cfg.Prompt == "" {
			slog.Debug("V3 script answered outside a prompt; the answer is ignored",
				"node", eng.session.NodeNumber, "value", value)
		}
		eng.answer, eng.answered = value, true
		return goja.Undefined()
	})

	jsutil.Set(v3, "prompt", obj)
}

// Answer returns the last value the script passed to v3.prompt.answer, and
// whether it called it at all. A call with no argument reports a nil value
// and true. The value is the exported JavaScript value: bool, string,
// float64 or int64 for numbers, nil for undefined or null, and maps or
// slices for objects and arrays. Call it after Run has returned.
func (eng *Engine) Answer() (value any, set bool) {
	return eng.answer, eng.answered
}
