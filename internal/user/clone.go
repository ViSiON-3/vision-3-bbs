package user

import (
	"maps"
	"slices"
	"time"
)

// Clone is a copy of u that shares nothing with it: every map, slice and
// pointer field is copied, so a change to the copy, in place or not, never
// reaches u. A nil field stays nil. The manager hands out and takes in
// records only as clones, so no caller can change a stored record except
// through a save.
//
// A reference field added to User must be copied here; TestCloneCopiesEveryReference
// fails until it is.
func (u *User) Clone() *User {
	if u == nil {
		return nil
	}
	c := *u
	c.TwitList = slices.Clone(u.TwitList)
	c.PublicKeys = slices.Clone(u.PublicKeys)
	c.Roles = slices.Clone(u.Roles)
	c.ExpiringRoles = maps.Clone(u.ExpiringRoles)
	c.Groups = slices.Clone(u.Groups)
	c.NamedFlags = maps.Clone(u.NamedFlags)
	c.PreviousHandles = slices.Clone(u.PreviousHandles)
	c.FileNewscanSince = cloneTime(u.FileNewscanSince)
	c.LastReadMessageIDs = maps.Clone(u.LastReadMessageIDs)
	c.TaggedFileIDs = slices.Clone(u.TaggedFileIDs)
	c.TaggedMessageAreaTags = slices.Clone(u.TaggedMessageAreaTags)
	c.TaggedFileAreaIDs = slices.Clone(u.TaggedFileAreaIDs)
	c.TaggedFileAreaTags = slices.Clone(u.TaggedFileAreaTags)
	c.SeenNewscanNetworks = slices.Clone(u.SeenNewscanNetworks)
	c.SeenNewscanAreaTags = slices.Clone(u.SeenNewscanAreaTags)
	c.SeenNewsIDs = slices.Clone(u.SeenNewsIDs)
	if u.MorePrompts != nil {
		v := *u.MorePrompts
		c.MorePrompts = &v
	}
	c.ColorCodes = slices.Clone(u.ColorCodes)
	c.CentralLastCallsSince = cloneTime(u.CentralLastCallsSince)
	c.DeletedAt = cloneTime(u.DeletedAt)
	c.APIDevices = slices.Clone(u.APIDevices)
	return &c
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}
