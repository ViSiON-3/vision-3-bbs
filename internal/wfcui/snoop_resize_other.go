//go:build !unix

package wfcui

// watchResize is a no-op where there is no SIGWINCH.
func watchResize() (<-chan struct{}, func()) {
	return nil, func() {}
}
