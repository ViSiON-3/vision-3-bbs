// Package types holds small types shared by the session, menu and
// cmd/vision3 packages, kept here so none of them has to import another just
// for a type definition.
package types

// AutoRunTracker keeps track of which run-once ('//') commands have executed in a session.
// Key format: "menuName:commandString"
type AutoRunTracker map[string]bool
