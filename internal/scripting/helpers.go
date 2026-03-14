package scripting

import (
	"fmt"
	"strconv"

	"github.com/dop251/goja"
)

// jsString reads a string property from a JS object with a default value.
func jsString(obj *goja.Object, key, defaultVal string) string {
	v := obj.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return defaultVal
	}
	return v.String()
}

// errMissingArgs returns an error for missing function arguments.
func errMissingArgs(fn, expected string) error {
	return fmt.Errorf("%s requires arguments: %s", fn, expected)
}

// itoa is a convenience alias for strconv.Itoa, used across binding files.
var itoa = strconv.Itoa
