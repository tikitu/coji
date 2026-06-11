package policy

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSetUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    Set
		wantErr bool
	}{
		{"preset read-only", `"read-only"`, Set{OpRead: true}, false},
		{"preset none", `"none"`, Set{}, false},
		{"preset rw", `"rw"`, Set{OpRead: true, OpCreate: true, OpEdit: true, OpDelete: true}, false},
		{"explicit ops", `["read","create"]`, Set{OpRead: true, OpCreate: true}, false},
		{"unknown preset", `"sideways"`, nil, true},
		{"unknown op", `["read","fly"]`, nil, true},
		{"wrong type", `42`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Set
			err := json.Unmarshal([]byte(tt.json), &s)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", s)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(s) != len(tt.want) {
				t.Fatalf("got %v, want %v", s, tt.want)
			}
			for op := range tt.want {
				if !s[op] {
					t.Errorf("missing op %q in %v", op, s)
				}
			}
		})
	}
}

func TestPolicyLoadAndAllow(t *testing.T) {
	var p Policy
	const doc = `{"default":"read-only","spaces":{"ENG":"read-write","SECRET":"none"}}`
	if err := json.Unmarshal([]byte(doc), &p); err != nil {
		t.Fatal(err)
	}
	if p.Default == nil { // Load() sets this; here we emulate the default branch
		p.Default, _ = presetSet("read-write")
	}

	cases := []struct {
		space string
		op    Op
		want  bool
	}{
		{"ENG", OpEdit, true},     // read-write
		{"ENG", OpDelete, true},   // read-write
		{"SECRET", OpRead, false}, // none
		{"DOCS", OpRead, true},    // default read-only
		{"DOCS", OpCreate, false}, // default read-only
	}
	for _, c := range cases {
		if got := p.Allow(c.space, c.op); got != c.want {
			t.Errorf("Allow(%q,%q) = %v, want %v", c.space, c.op, got, c.want)
		}
	}
}

func TestNilPolicyAllowsAll(t *testing.T) {
	var p *Policy
	if !p.Allow("anything", OpDelete) {
		t.Error("nil policy should allow everything")
	}
	if err := p.Check("anything", OpDelete); err != nil {
		t.Errorf("nil policy Check should be nil, got %v", err)
	}
}

func TestCheckReturnsError(t *testing.T) {
	p := &Policy{Default: Set{OpRead: true}, Spaces: map[string]Set{}}
	err := p.Check("DOCS", OpCreate)
	var perr *Error
	if !errors.As(err, &perr) {
		t.Fatalf("expected *policy.Error, got %T", err)
	}
	if perr.SpaceKey != "DOCS" || perr.Op != OpCreate {
		t.Errorf("error = %+v", perr)
	}
}
