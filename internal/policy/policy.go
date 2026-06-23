// Package policy is a client-side guardrail: it decides which operations coji
// will perform in which spaces, based on a user-maintained policy file. It is
// NOT a security boundary — the credentials' real permissions still apply
// server-side — but it prevents accidental writes to spaces you mean to treat
// as read-only.
//
// The policy file is JSON, keyed by space key, e.g.:
//
//	{
//	  "default":          "read-write",
//	  "personal-default": "none",
//	  "spaces": {
//	    "ENG":     "read-write",
//	    "ARCHIVE": "read-only",
//	    "SECRET":  "none"
//	  }
//	}
//
// A space entry is either a preset string ("none", "read-only", "read-write")
// or an explicit list of operations, e.g. ["read","create","edit"].
//
// Personal spaces (Confluence keys prefixed with "~") are treated separately:
// when one has no explicit entry it falls back to "personal-default" rather
// than "default". An omitted "personal-default" means "none", so personal
// spaces are inaccessible unless explicitly opted into.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Op is an operation coji can perform against a space.
type Op string

const (
	OpRead   Op = "read"
	OpCreate Op = "create"
	OpEdit   Op = "edit"
	OpDelete Op = "delete"
)

func validOp(o Op) bool {
	switch o {
	case OpRead, OpCreate, OpEdit, OpDelete:
		return true
	}
	return false
}

// Set is the set of operations allowed for a space.
type Set map[Op]bool

// presetSet expands a named access level to a Set.
func presetSet(name string) (Set, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "none", "deny", "off":
		return Set{}, true
	case "read", "read-only", "readonly", "ro":
		return Set{OpRead: true}, true
	case "write", "read-write", "readwrite", "rw":
		return Set{OpRead: true, OpCreate: true, OpEdit: true, OpDelete: true}, true
	}
	return nil, false
}

// UnmarshalJSON accepts either a preset string or an explicit array of ops.
func (s *Set) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		ps, ok := presetSet(str)
		if !ok {
			return fmt.Errorf("unknown access level %q (want none, read-only, or read-write)", str)
		}
		*s = ps
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		set := Set{}
		for _, o := range arr {
			op := Op(strings.ToLower(strings.TrimSpace(o)))
			if !validOp(op) {
				return fmt.Errorf("unknown operation %q (want read, create, edit, or delete)", o)
			}
			set[op] = true
		}
		*s = set
		return nil
	}
	return fmt.Errorf("policy entry must be an access-level string or an array of operations")
}

// String renders a Set as a sorted, comma-separated list (or "none").
func (s Set) String() string {
	if len(s) == 0 {
		return "none"
	}
	ops := make([]string, 0, len(s))
	for op, ok := range s {
		if ok {
			ops = append(ops, string(op))
		}
	}
	sort.Strings(ops)
	return strings.Join(ops, ",")
}

// PersonalPrefix is the leading character of a Confluence personal-space key.
const PersonalPrefix = "~"

// IsPersonal reports whether a space key names a personal space.
func IsPersonal(spaceKey string) bool {
	return strings.HasPrefix(spaceKey, PersonalPrefix)
}

// Policy maps space keys to allowed operation sets, with a default for spaces
// not listed. Personal spaces (see IsPersonal) fall back to PersonalDefault
// instead of Default when they have no explicit entry.
type Policy struct {
	Default         Set            `json:"default"`
	PersonalDefault Set            `json:"personal-default"`
	Spaces          map[string]Set `json:"spaces"`
}

// Load reads a policy file. A missing file returns (nil, nil): with no policy,
// all operations are allowed. When the file omits "default", it defaults to
// read-write so the file only restricts the spaces it explicitly lists.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading policy: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing policy %s: %w", path, err)
	}
	if p.Default == nil {
		p.Default, _ = presetSet("read-write")
	}
	if p.PersonalDefault == nil {
		// Personal spaces are off unless explicitly opted into.
		p.PersonalDefault, _ = presetSet("none")
	}
	if p.Spaces == nil {
		p.Spaces = map[string]Set{}
	}
	return &p, nil
}

// Allow reports whether op is permitted in the space with the given key. A nil
// Policy (no policy file) allows everything. An unknown/empty key falls back to
// the default.
func (p *Policy) Allow(spaceKey string, op Op) bool {
	if p == nil {
		return true
	}
	if s, ok := p.Spaces[spaceKey]; ok {
		return s[op]
	}
	if IsPersonal(spaceKey) {
		return p.PersonalDefault[op]
	}
	return p.Default[op]
}

// Check returns a *Error when op is not allowed in spaceKey, else nil.
func (p *Policy) Check(spaceKey string, op Op) error {
	if p.Allow(spaceKey, op) {
		return nil
	}
	return &Error{SpaceKey: spaceKey, Op: op}
}

// Error is returned when the policy disallows an operation.
type Error struct {
	SpaceKey string
	Op       Op
}

func (e *Error) Error() string {
	where := "this space"
	if e.SpaceKey != "" {
		where = fmt.Sprintf("space %q", e.SpaceKey)
	}
	return fmt.Sprintf("%q is not allowed in %s by your coji policy", e.Op, where)
}
