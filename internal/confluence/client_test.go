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
