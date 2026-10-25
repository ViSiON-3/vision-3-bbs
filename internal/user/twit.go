package user

import (
	"errors"
	"strings"
	"unicode"
)

var (
	// ErrTwitSelf rejects adding the list owner.
	ErrTwitSelf = errors.New("cannot add yourself")
	// ErrTwitExempt rejects adding a protected local account.
	ErrTwitExempt = errors.New("user is exempt")
	// ErrTwitFull reports that the configured entry limit has been reached.
	ErrTwitFull = errors.New("twit list full")
	// ErrTwitDuplicate reports an author already on the list.
	ErrTwitDuplicate = errors.New("already on twit list")
	// ErrTwitInvalid rejects an empty or unsafe author name.
	ErrTwitInvalid = errors.New("invalid twit name")
)

// TwitPolicy supplies defaults even for boards upgraded without the new settings.
type TwitPolicy struct{ MaxEntries, ExemptLevel, SysOpLevel, CoSysOpLevel int }

// Limit returns the configured cap or the default of 50 entries.
func (p TwitPolicy) Limit() int {
	if p.MaxEntries > 0 {
		return p.MaxEntries
	}
	return 50
}

// Exempt reports whether local account messages must remain visible. Sysops
// and co-sysops always are; ExemptLevel, when set, protects lower levels too.
func (p TwitPolicy) Exempt(u *User) bool {
	staff := p.SysOpLevel
	if staff <= 0 {
		staff = 255
	}
	if p.CoSysOpLevel > 0 {
		staff = min(staff, p.CoSysOpLevel)
	}
	level := p.ExemptLevel
	if level <= 0 {
		level = staff
	}
	return u != nil && (u.AccessLevel >= staff || u.AccessLevel >= level)
}

// TwitDirectory resolves local authors by handle and stable account ID.
type TwitDirectory interface {
	GetUser(string) (*User, bool)
	GetUserByID(int) (*User, bool)
}

// NewTwitEntry resolves complete local handles before interpreting network name@address syntax.
// This preserves handles containing @ while qualified remote names remain network entries.
func NewTwitEntry(owner *User, directory TwitDirectory, text string, policy TwitPolicy) (TwitEntry, error) {
	entry := TwitEntry{Name: strings.TrimSpace(text)}
	if owner == nil || entry.Name == "" || len(text) > 200 || strings.ContainsFunc(text, unicode.IsControl) {
		return TwitEntry{}, ErrTwitInvalid
	}
	if directory != nil {
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
	if entry.UserID == 0 {
		if i := strings.LastIndex(entry.Name, "@"); i >= 0 {
			entry.Name, entry.Addr = strings.TrimSpace(entry.Name[:i]), strings.TrimSpace(entry.Name[i+1:])
			if entry.Addr == "" {
				return TwitEntry{}, ErrTwitInvalid
			}
		}
	}
	if entry.Name == "" {
		return TwitEntry{}, ErrTwitInvalid
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

// BuildTwitMatcher snapshots author names and current exemptions for a reader or export.
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

// Hidden matches a displayed author and origin, always preserving anonymous messages.
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
