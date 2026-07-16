// Package jsutil provides small helpers for building goja JavaScript
// runtimes. It centralizes error handling for property registration so
// call sites stay readable while errors are still surfaced via slog.
package jsutil

import (
	"log/slog"

	"github.com/dop251/goja"
)

// Setter is satisfied by both *goja.Runtime and *goja.Object.
type Setter interface {
	Set(name string, value any) error
}

// Set assigns a property on a JS object or a runtime global. goja's Set
// only fails on frozen/non-extensible targets, which never applies to the
// runtime objects built by this codebase, so a failure indicates a
// programming error and is logged rather than propagated.
func Set(target Setter, name string, value any) {
	if err := target.Set(name, value); err != nil {
		slog.Error("jsutil: failed to set JS property", "name", name, "error", err)
	}
}

// DefineAccessor defines a getter/setter property on a JS object. As with
// Set, failure indicates a programming error (e.g. redefining a
// non-configurable property) and is logged rather than propagated.
func DefineAccessor(obj *goja.Object, name string, getter, setter goja.Value, configurable, enumerable goja.Flag) {
	if err := obj.DefineAccessorProperty(name, getter, setter, configurable, enumerable); err != nil {
		slog.Error("jsutil: failed to define JS accessor", "name", name, "error", err)
	}
}
