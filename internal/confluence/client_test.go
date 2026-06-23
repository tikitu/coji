package confluence

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a Client at a test server.
func newTestClient(srv *httptest.Server) *Client {
	return New(srv.Client(), srv.URL)
}

func TestGetPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/pages/123" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("body-format"); got != "storage" {
			t.Errorf("body-format = %q, want storage", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"123","title":"Hi","status":"current",
			"version":{"number":7},
			"body":{"storage":{"representation":"storage","value":"<p>hi</p>"}}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	p, err := c.GetPage(context.Background(), "123", Storage)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Hi" || p.Version.Number != 7 {
		t.Errorf("unexpected page: %+v", p)
	}
	if b := p.Body.Get(Storage); b == nil || b.Value != "<p>hi</p>" {
		t.Errorf("storage body = %+v", b)
	}
	if got := NextVersion(p); got != 8 {
		t.Errorf("NextVersion = %d, want 8", got)
	}
}

func TestCreatePageSendsNestedBody(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pages" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &captured)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"id":"999","title":"New"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	p, err := c.CreatePage(context.Background(), CreatePageInput{
		SpaceID: "42", Title: "New", Rep: Storage, Value: "<p>body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "999" {
		t.Errorf("id = %q, want 999", p.ID)
	}
	if captured["spaceId"] != "42" || captured["status"] != "current" {
		t.Errorf("captured = %v", captured)
	}
	body, _ := captured["body"].(map[string]any)
	storage, _ := body["storage"].(map[string]any)
	if storage["value"] != "<p>body</p>" || storage["representation"] != "storage" {
		t.Errorf("nested storage body = %v", body)
	}
}

func TestCreatePagePrivate(t *testing.T) {
	var gotPrivate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrivate = r.URL.Query().Get("private")
		io.WriteString(w, `{"id":"1"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if _, err := c.CreatePage(context.Background(), CreatePageInput{
		SpaceID: "42", Title: "secret", Rep: Storage, Value: "<p>x</p>", Private: true,
	}); err != nil {
		t.Fatal(err)
	}
	if gotPrivate != "true" {
		t.Errorf("private query = %q, want \"true\"", gotPrivate)
	}

	// Without Private, the query param should be absent.
	if _, err := c.CreatePage(context.Background(), CreatePageInput{
		SpaceID: "42", Title: "public", Rep: Storage, Value: "<p>x</p>",
	}); err != nil {
		t.Fatal(err)
	}
	if gotPrivate != "" {
		t.Errorf("private query = %q, want empty", gotPrivate)
	}
}

func TestUpdatePageVersion(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/pages/123" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &captured)
		io.WriteString(w, `{"id":"123","version":{"number":8}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.UpdatePage(context.Background(), UpdatePageInput{
		ID: "123", Title: "T", Rep: Storage, Value: "<p>x</p>", VersionNumber: 8, VersionMsg: "edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	ver, _ := captured["version"].(map[string]any)
	if ver["number"].(float64) != 8 || ver["message"] != "edit" {
		t.Errorf("version = %v", ver)
	}
}

func TestListSpacesPaginates(t *testing.T) {
	var cursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		cursors = append(cursors, cursor)
		switch cursor {
		case "":
			io.WriteString(w, `{"results":[{"id":"1","key":"A"}],"_links":{"next":"/wiki/api/v2/spaces?limit=250&cursor=NEXT"}}`)
		case "NEXT":
			io.WriteString(w, `{"results":[{"id":"2","key":"B"}],"_links":{}}`)
		default:
			t.Errorf("unexpected cursor %q", cursor)
		}
	}))
	defer srv.Close()

	spaces, err := newTestClient(srv).ListSpaces(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 2 || spaces[0].Key != "A" || spaces[1].Key != "B" {
		t.Errorf("spaces = %+v, want A then B", spaces)
	}
	if len(cursors) != 2 || cursors[0] != "" || cursors[1] != "NEXT" {
		t.Errorf("cursors = %v, want [\"\" \"NEXT\"]", cursors)
	}
}

func TestDirectChildrenUsesFolderPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/folders/42/direct-children" {
			t.Errorf("path = %q, want folder path", r.URL.Path)
		}
		io.WriteString(w, `{"results":[{"id":"9","type":"page","title":"Kid"}],"_links":{}}`)
	}))
	defer srv.Close()

	kids, err := newTestClient(srv).DirectChildren(context.Background(), TypeFolder, "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].Type != TypePage || kids[0].ID != "9" {
		t.Errorf("children = %+v", kids)
	}
}

func TestCursorFromNext(t *testing.T) {
	tests := map[string]string{
		"":                                 "",
		"/wiki/api/v2/pages?cursor=abc123": "abc123",
		"/wiki/api/v2/pages?limit=25":      "",
	}
	for in, want := range tests {
		if got := cursorFromNext(in); got != want {
			t.Errorf("cursorFromNext(%q) = %q, want %q", in, got, want)
		}
	}
}

// search fixtures: shape captured from a real Confluence Cloud v1 /search
// response, with all contentful values (titles, excerpts, space keys, ids,
// urls) replaced by invented ones. The envelope structure, field names,
// nesting, embedded "\n" in excerpts, the "&#39;" entity in a top-level title,
// and the cursor-bearing _links.next are preserved verbatim from the real API.
const searchPage1 = `{
  "results": [
    {
      "content": {
        "id": "111111", "type": "page", "status": "current",
        "title": "Widget handling policy",
        "space": {"id": 900001, "key": "DOCS", "name": "Documentation", "type": "global"}
      },
      "title": "Widget handling policy",
      "excerpt": "This page describes how widgets are handled.\nIt covers intake and disposal.",
      "url": "/spaces/DOCS/pages/111111/Widget+handling+policy",
      "lastModified": "2025-02-03T11:22:33.000Z",
      "score": 42.5
    },
    {
      "content": {
        "id": "222222", "type": "page", "status": "current",
        "title": "Gadget onboarding checklist",
        "space": {"id": 900002, "key": "OPS", "name": "Operations", "type": "global"}
      },
      "title": "Gadget&#39;s onboarding checklist",
      "excerpt": "Steps for onboarding a new gadget.",
      "url": "/spaces/OPS/pages/222222/Gadget+onboarding+checklist",
      "lastModified": "2025-04-10T08:00:00.000Z",
      "score": 17.0
    }
  ],
  "start": 0, "limit": 2, "size": 2, "totalSize": 3,
  "_links": {
    "base": "https://example.atlassian.net/wiki",
    "next": "/rest/api/search?next=true&cursor=CURSOR2&limit=2&start=2&cql=type%3Dpage"
  }
}`

const searchPage2 = `{
  "results": [
    {
      "content": {
        "id": "333333", "type": "page", "status": "current",
        "title": "Sprocket maintenance guide",
        "space": {"id": 900001, "key": "DOCS", "name": "Documentation", "type": "global"}
      },
      "title": "Sprocket maintenance guide",
      "excerpt": "Routine maintenance for sprockets.",
      "url": "/spaces/DOCS/pages/333333/Sprocket+maintenance+guide",
      "lastModified": "2025-05-01T09:30:00.000Z",
      "score": 9.1
    }
  ],
  "start": 2, "limit": 2, "size": 1, "totalSize": 3,
  "_links": {"base": "https://example.atlassian.net/wiki"}
}`

func TestSearchParsesAndPaginates(t *testing.T) {
	var paths, cursors, cqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		cursors = append(cursors, r.URL.Query().Get("cursor"))
		cqls = append(cqls, r.URL.Query().Get("cql"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "CURSOR2" {
			io.WriteString(w, searchPage2)
		} else {
			io.WriteString(w, searchPage1)
		}
	}))
	defer srv.Close()

	// Build the client with a v2-style base so v1BaseURL's suffix swap is exercised.
	c := New(srv.Client(), srv.URL+"/api/v2")
	hits, err := c.Search(context.Background(), `type=page AND text ~ "widget"`, 25)
	if err != nil {
		t.Fatal(err)
	}

	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3", len(hits))
	}
	// Search must hit the v1 /search path (not the v2 base).
	for _, p := range paths {
		if p != "/rest/api/search" {
			t.Errorf("request path = %q, want /rest/api/search", p)
		}
	}
	if len(cursors) != 2 || cursors[0] != "" || cursors[1] != "CURSOR2" {
		t.Errorf("cursors = %v, want [\"\" \"CURSOR2\"]", cursors)
	}
	if cqls[0] != `type=page AND text ~ "widget"` {
		t.Errorf("cql = %q", cqls[0])
	}

	got := hits[0]
	if got.ID != "111111" || got.SpaceKey != "DOCS" || got.Type != "page" {
		t.Errorf("hit[0] id/space/type = %+v", got)
	}
	if got.Title != "Widget handling policy" {
		t.Errorf("hit[0] title = %q", got.Title)
	}
	if !strings.Contains(got.Excerpt, "\n") {
		t.Errorf("client should pass the raw excerpt through (cleaning is core's job): %q", got.Excerpt)
	}
	if got.URL != "/spaces/DOCS/pages/111111/Widget+handling+policy" {
		t.Errorf("hit[0] url = %q", got.URL)
	}
	if got.Updated != "2025-02-03T11:22:33.000Z" {
		t.Errorf("hit[0] updated = %q", got.Updated)
	}
	if hits[2].ID != "333333" {
		t.Errorf("hit[2] id = %q, want 333333 (second page)", hits[2].ID)
	}
}

func TestSearchHonorsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, searchPage1) // 2 results, with a next cursor
	}))
	defer srv.Close()

	// limit=1 must stop after the first result and not chase the next cursor.
	hits, err := New(srv.Client(), srv.URL+"/api/v2").Search(context.Background(), "cql", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "111111" {
		t.Errorf("hits = %+v, want exactly the first result", hits)
	}
}

func TestV1BaseURLSuffixSwap(t *testing.T) {
	tests := map[string]string{
		"https://api.atlassian.com/ex/confluence/CID/api/v2": "https://api.atlassian.com/ex/confluence/CID/rest/api",
		"https://site.atlassian.net/wiki/api/v2":             "https://site.atlassian.net/wiki/rest/api",
	}
	for base, want := range tests {
		if got := New(nil, base).v1BaseURL(); got != want {
			t.Errorf("v1BaseURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"errors":[{"title":"Not Found","detail":"no such page"}]}`)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.GetPage(context.Background(), "missing", Storage)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("status code = %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Detail, "no such page") {
		t.Errorf("detail = %q", apiErr.Detail)
	}
}
