package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgilbir/coji/internal/confluence"
	"github.com/mgilbir/coji/internal/policy"
)

// readWrite is the full op set; readOnly allows only reads.
var (
	readWrite = policy.Set{policy.OpRead: true, policy.OpCreate: true, policy.OpEdit: true, policy.OpDelete: true}
	readOnly  = policy.Set{policy.OpRead: true}
)

func svcWithPolicy(srv *httptest.Server, p *policy.Policy) *Service {
	return New(confluence.New(srv.Client(), srv.URL), WithPolicy(p))
}

func TestPolicyBlocksCreate(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted = true
		}
		io.WriteString(w, `{"id":"1"}`)
	}))
	defer srv.Close()

	// Default read-only; creating in an unlisted space must be blocked.
	p := &policy.Policy{Default: readOnly, Spaces: map[string]policy.Set{}}
	_, err := svcWithPolicy(srv, p).CreatePage(context.Background(), CreateInput{
		SpaceKey: "DOCS", Title: "x", Format: FormatMarkdown, Content: "hi",
	})
	var perr *policy.Error
	if !errors.As(err, &perr) {
		t.Fatalf("expected *policy.Error, got %v", err)
	}
	if posted {
		t.Error("a blocked create must not POST to the API")
	}
}

func TestPolicyAllowsCreateInListedSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create with SpaceKey resolves the id first.
		if r.URL.Path == "/spaces" {
			io.WriteString(w, `{"results":[{"id":"500","key":"ENG"}]}`)
			return
		}
		io.WriteString(w, `{"id":"9","title":"x","version":{"number":1}}`)
	}))
	defer srv.Close()

	p := &policy.Policy{Default: readOnly, Spaces: map[string]policy.Set{"ENG": readWrite}}
	page, err := svcWithPolicy(srv, p).CreatePage(context.Background(), CreateInput{
		SpaceKey: "ENG", Title: "x", Format: FormatMarkdown, Content: "hi",
	})
	if err != nil {
		t.Fatalf("create in read-write space should succeed: %v", err)
	}
	if page.ID != "9" {
		t.Errorf("id = %q", page.ID)
	}
}

func TestPolicyBlocksEditViaIDResolution(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pages/77":
			io.WriteString(w, `{"id":"77","title":"T","spaceId":"100","version":{"number":2}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/spaces/100":
			io.WriteString(w, `{"id":"100","key":"ARCHIVE"}`)
		case r.Method == http.MethodPut:
			putCalled = true
			io.WriteString(w, `{"id":"77","version":{"number":3}}`)
		}
	}))
	defer srv.Close()

	// ARCHIVE is read-only -> edit must be blocked after resolving its key.
	p := &policy.Policy{Default: readWrite, Spaces: map[string]policy.Set{"ARCHIVE": readOnly}}
	_, err := svcWithPolicy(srv, p).EditPage(context.Background(), EditInput{
		ID: "77", Format: FormatMarkdown, Content: "new",
	})
	var perr *policy.Error
	if !errors.As(err, &perr) {
		t.Fatalf("expected *policy.Error, got %v", err)
	}
	if perr.SpaceKey != "ARCHIVE" {
		t.Errorf("error space = %q, want ARCHIVE", perr.SpaceKey)
	}
	if putCalled {
		t.Error("a blocked edit must not PUT to the API")
	}
}

func TestNoPolicyAllowsEverything(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/spaces" {
			io.WriteString(w, `{"results":[{"id":"1","key":"ANY"}]}`)
			return
		}
		io.WriteString(w, `{"id":"2","version":{"number":1}}`)
	}))
	defer srv.Close()

	// No policy (nil) -> create allowed, and no SpaceByID call needed.
	_, err := New(confluence.New(srv.Client(), srv.URL)).CreatePage(context.Background(), CreateInput{
		SpaceKey: "ANY", Title: "x", Format: FormatMarkdown, Content: "hi",
	})
	if err != nil {
		t.Fatalf("create with no policy should succeed: %v", err)
	}
}
