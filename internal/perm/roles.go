package perm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// FileName is the roles file's name in the board's configs directory.
const FileName = "roles.json"

// maxRoleName is the longest role name: long enough to say what a role is
// for, short enough for the user editors' columns.
const maxRoleName = 24

// Role is one role as configs/roles.json writes it.
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`
}

// file is configs/roles.json.
type file struct {
	Roles []Role `json:"roles"`
}

// Diagnostic is one problem in configs/roles.json. None stops the file
// loading: the role or permission it names is left out, so a mistake can
// only ever grant less.
type Diagnostic struct {
	// Index and Role are the role's position in the file (1-based) and its
	// name as written.
	Index int
	Role  string
	// Message says what is wrong and what was done about it.
	Message string
}

// String is the diagnostic as the log and doctor print it:
// `role 2 ("Validator"): ...`.
func (d Diagnostic) String() string {
	return fmt.Sprintf("role %d (%q): %s", d.Index, d.Role, d.Message)
}

// Roles is the set of roles a board defines. The zero value and nil are
// both the empty set: no roles, so nothing is granted below sysop level.
type Roles struct {
	list   []Role // as loaded, names normalized, invalid entries left out
	byName map[string]map[Permission]bool
}

// Len is the number of roles defined.
func (r *Roles) Len() int {
	if r == nil {
		return 0
	}
	return len(r.list)
}

// List is the roles defined, in file order.
func (r *Roles) List() []Role {
	if r == nil {
		return nil
	}
	out := make([]Role, len(r.list))
	for i, role := range r.list {
		role.Permissions = append([]string(nil), role.Permissions...)
		out[i] = role
	}
	return out
}

// Names is the defined roles' names, in file order.
func (r *Roles) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, len(r.list))
	for i, role := range r.list {
		names[i] = role.Name
	}
	return names
}

// Defined reports whether a role named name exists, ignoring case.
func (r *Roles) Defined(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.byName[NormalizeRoleName(name)]
	return ok
}

// grants reports whether the role named name (already normalized) exists
// and grants p.
func (r *Roles) grants(name string, p Permission) bool {
	if r == nil {
		return false
	}
	return r.byName[name][p]
}

// Parse reads the contents of configs/roles.json. It fails only for JSON
// that can't be read; problems inside it come back as diagnostics, with the
// entry they concern left out.
func Parse(data []byte) (*Roles, []Diagnostic, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := &Roles{byName: map[string]map[Permission]bool{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return r, nil, nil
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil, err
	}
	var diags []Diagnostic
	for i, role := range f.Roles {
		bad := func(format string, a ...any) {
			diags = append(diags, Diagnostic{Index: i + 1, Role: role.Name, Message: fmt.Sprintf(format, a...)})
		}
		name := NormalizeRoleName(role.Name)
		if err := CheckRoleName(name); err != nil {
			bad("%v; the role is ignored", err)
			continue
		}
		if _, dup := r.byName[name]; dup {
			bad("another role is already named %s; this one is ignored", name)
			continue
		}
		grants := map[Permission]bool{}
		var kept []string
		for _, ps := range role.Permissions {
			p, ok := Lookup(ps)
			if !ok {
				bad("%q is not a permission (known: %s); it grants nothing", ps, KnownNames())
				continue
			}
			if !grants[p] {
				grants[p] = true
				kept = append(kept, string(p))
			}
		}
		if len(kept) == 0 {
			bad("grants no permissions, so its members can do nothing a role gives")
		}
		r.byName[name] = grants
		r.list = append(r.list, Role{Name: name, Description: strings.TrimSpace(role.Description), Permissions: kept})
	}
	return r, diags, nil
}

// Path is configs/roles.json under configDir.
func Path(configDir string) string { return filepath.Join(configDir, FileName) }

// Load reads configDir's roles.json. A missing file is the empty set, the
// board's state until the sysop defines a role.
func Load(configDir string) (*Roles, []Diagnostic, error) {
	data, err := os.ReadFile(Path(configDir))
	if errors.Is(err, fs.ErrNotExist) {
		return &Roles{byName: map[string]map[Permission]bool{}}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return Parse(data)
}

// current is the BBS's roles, as last loaded.
var current atomic.Pointer[Roles]

// Current is the roles the BBS last loaded, never nil: the empty set before
// SetCurrent.
func Current() *Roles {
	if r := current.Load(); r != nil {
		return r
	}
	return &Roles{}
}

// SetCurrent makes r the roles every later check reads. The BBS calls it at
// startup and each time configs/roles.json is reloaded.
func SetCurrent(r *Roles) { current.Store(r) }

// NormalizeRoleName is name as roles are compared: trimmed, lower case.
func NormalizeRoleName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// CheckRoleName reports what is wrong with a role name, nil for one that can
// be used: a letter, then letters, digits, _ and -, up to 24 in all. The same
// characters are what the ACS term ROLE:name reads.
func CheckRoleName(name string) error {
	if name == "" {
		return errors.New("a role needs a name")
	}
	if len(name) > maxRoleName {
		return fmt.Errorf("role name %q is longer than %d characters", name, maxRoleName)
	}
	for i, c := range name {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		switch {
		case i == 0 && !letter:
			return fmt.Errorf("role name %q must start with a letter", name)
		case !letter && (c < '0' || c > '9') && c != '_' && c != '-':
			return fmt.Errorf("role name %q has %q in it; use letters, digits, _ and -", name, c)
		}
	}
	return nil
}

// NormalizeRoles is a user record's role list as compared: each name
// normalized, blanks and repeats dropped, order kept.
func NormalizeRoles(names []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		n = NormalizeRoleName(n)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// ParseRoleList reads a list of role names typed into a user editor,
// separated by commas or spaces, and checks each against roles: a name that
// isn't a valid role name, or that roles doesn't define, is an error, so a
// sysop can't assign a role that would grant nothing without noticing. A
// blank list is no roles.
func ParseRoleList(s string, roles *Roles) ([]string, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	names := NormalizeRoles(fields)
	for _, n := range names {
		if err := CheckRoleName(n); err != nil {
			return nil, err
		}
		if !roles.Defined(n) {
			if roles.Len() == 0 {
				return nil, fmt.Errorf("no role named %s: configs/roles.json defines no roles", n)
			}
			return nil, fmt.Errorf("no role named %s in configs/roles.json (defined: %s)", n, strings.Join(roles.Names(), ", "))
		}
	}
	return names, nil
}

// FormatRoleList is a role list as the user editors show it: "a, b", or ""
// for none.
func FormatRoleList(names []string) string { return strings.Join(NormalizeRoles(names), ", ") }

// Undefined is the names on a record that roles doesn't define, normalized.
func Undefined(names []string, roles *Roles) []string {
	var out []string
	for _, n := range NormalizeRoles(names) {
		if !roles.Defined(n) {
			out = append(out, n)
		}
	}
	return out
}
