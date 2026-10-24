package user

import (
	"errors"
	"strings"
	"unicode"
)

var (
	ErrTwitSelf      = errors.New("cannot add yourself")
	ErrTwitExempt    = errors.New("user is exempt")
	ErrTwitFull      = errors.New("twit list full")
	ErrTwitDuplicate = errors.New("already on twit list")
	ErrTwitInvalid   = errors.New("invalid twit name")
)

// TwitPolicy supplies defaults even for boards upgraded without the new settings.
type TwitPolicy struct{ MaxEntries, ExemptLevel, SysOpLevel int }

func (p TwitPolicy) Limit() int {
	if p.MaxEntries > 0 {
		return p.MaxEntries
	}
	return 50
}
func (p TwitPolicy) Exempt(u *User) bool {
	sysop := p.SysOpLevel
	if sysop <= 0 {
		sysop = 255
	}
	level := p.ExemptLevel
	if level <= 0 {
		level = sysop
	}
	return u != nil && (u.AccessLevel >= sysop || u.AccessLevel >= level)
}

type TwitDirectory interface {
	GetUser(string) (*User, bool)
	GetUserByID(int) (*User, bool)
}

// NewTwitEntry resolves unqualified local handles to stable account IDs.
// Qualified names deliberately remain network entries, even if a handle matches.
func NewTwitEntry(owner *User, directory TwitDirectory, text string, policy TwitPolicy) (TwitEntry, error) {
	entry := TwitEntry{Name: strings.TrimSpace(text)}
	if i := strings.LastIndex(entry.Name, "@"); i >= 0 {
		entry.Name, entry.Addr = strings.TrimSpace(entry.Name[:i]), strings.TrimSpace(entry.Name[i+1:])
		if entry.Addr == "" {
			return TwitEntry{}, ErrTwitInvalid
		}
	}
	if owner == nil || entry.Name == "" || len(text) > 200 || strings.ContainsFunc(text, unicode.IsControl) {
		return TwitEntry{}, ErrTwitInvalid
	}
	if entry.Addr == "" && directory != nil {
		if target, ok := directory.GetUser(entry.Name); ok {
			if target.ID == owner.ID {
				return TwitEntry{}, ErrTwitSelf
			}
			if policy.Exempt(target) {
				return TwitEntry{}, ErrTwitExempt
			}
			entry.UserID, entry.Name = target.ID, target.Handle
		}
	}
	if entry.Addr == "" && (strings.EqualFold(entry.Name, owner.Handle) || strings.EqualFold(entry.Name, owner.RealName)) {
		return TwitEntry{}, ErrTwitSelf
	}
	for _, old := range owner.TwitList {
		if (entry.UserID != 0 && old.UserID == entry.UserID) || (strings.EqualFold(strings.TrimSpace(old.Name), entry.Name) && strings.EqualFold(strings.TrimSpace(old.Addr), entry.Addr)) {
			return TwitEntry{}, ErrTwitDuplicate
		}
	}
	if len(owner.TwitList) >= policy.Limit() {
		return TwitEntry{}, ErrTwitFull
	}
	return entry, nil
}

type twitMatch struct {
	names []string
	addr  string
}

// TwitMatcher resolves local names and exemptions once, when entering a reader
// or building a QWK packet. It never consults hidden author metadata.
type TwitMatcher struct {
	entries       []twitMatch
	anonymousName string
}

func BuildTwitMatcher(entries []TwitEntry, directory TwitDirectory, policy TwitPolicy, anonymousName string) *TwitMatcher {
	matcher := &TwitMatcher{anonymousName: strings.TrimSpace(anonymousName)}
	for _, entry := range entries {
		match := twitMatch{names: []string{strings.TrimSpace(entry.Name)}, addr: strings.TrimSpace(entry.Addr)}
		if entry.UserID != 0 {
			match.addr = ""
			if directory != nil {
				if target, ok := directory.GetUserByID(entry.UserID); ok {
					if policy.Exempt(target) {
						continue
					}
					match.names = append(match.names, strings.TrimSpace(target.Handle), strings.TrimSpace(target.RealName))
				}
			}
		}
		matcher.entries = append(matcher.entries, match)
	}
	return matcher
}
func (m *TwitMatcher) Hidden(from, addr string) bool {
	if m == nil {
		return false
	}
	from, addr = strings.TrimSpace(from), strings.TrimSpace(addr)
	if from == "" || strings.EqualFold(from, "Anonymous") || (m.anonymousName != "" && strings.EqualFold(from, m.anonymousName)) {
		return false
	}
	for _, entry := range m.entries {
		if entry.addr != "" && !strings.EqualFold(entry.addr, addr) {
			continue
		}
		for _, name := range entry.names {
			if name != "" && strings.EqualFold(name, from) {
				return true
			}
		}
	}
	return false
}
