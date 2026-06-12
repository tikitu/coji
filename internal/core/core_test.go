package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgilbir/coji/internal/confluence"
)

func newSvc(srv *httptest.Server) *Service {
	return New(confluence.New(srv.Client(), srv.URL))
}

func TestGetPageMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("body-format"); got != "storage" {
			t.Errorf("body-format = %q, want storage", got)
		}
		io.WriteString(w, `{"id":"7","title":"T","version":{"number":3},
			"body":{"storage":{"representation":"storage","value":"<p>hi <strong>there</strong></p>"}}}`)
	}))
	defer srv.Close()

	p, err := newSvc(srv).GetPage(context.Background(), "7", FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 3 {
		t.Errorf("version = %d, want 3", p.Version)
	}
	if strings.TrimSpace(p.Body) != "hi **there**" {
		t.Errorf("body = %q, want %q", p.Body, "hi **there**")
	}
}

func TestGetPageStorageRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"7","body":{"storage":{"value":"<p>raw</p>"}}}`)
	}))
	defer srv.Close()

	p, err := newSvc(srv).GetPage(context.Background(), "7", FormatStorage)
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "<p>raw</p>" {
		t.Errorf("body = %q, want raw storage passthrough", p.Body)
	}
}

func TestCreatePageConvertsMarkdown(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &body)
		io.WriteString(w, `{"id":"100","title":"New","version":{"number":1}}`)
	}))
	defer srv.Close()

	p, err := newSvc(srv).CreatePage(context.Background(), CreateInput{
		SpaceID: "42", Title: "New", Format: FormatMarkdown, Content: "# Hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "100" {
		t.Errorf("id = %q", p.ID)
	}
	storage := body["body"].(map[string]any)["storage"].(map[string]any)
	if !strings.Contains(storage["value"].(string), "<h1>Hello</h1>") {
		t.Errorf("storage value = %q, want converted heading", storage["value"])
	}
}

func TestEditPageBumpsVersion(t *testing.T) {
	var put map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Current state: version 5, existing title.
			io.WriteString(w, `{"id":"9","title":"Existing","version":{"number":5}}`)
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			json.Unmarshal(data, &put)
			io.WriteString(w, `{"id":"9","title":"Existing","version":{"number":6}}`)
		}
	}))
	defer srv.Close()

	p, err := newSvc(srv).EditPage(context.Background(), EditInput{
		ID: "9", Format: FormatMarkdown, Content: "updated **body**", VersionMsg: "tweak",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 6 {
		t.Errorf("returned version = %d, want 6", p.Version)
	}
	// Title preserved from current page.
	if put["title"] != "Existing" {
		t.Errorf("title = %v, want Existing (preserved)", put["title"])
	}
	// Version bumped to current+1.
	ver := put["version"].(map[string]any)
	if ver["number"].(float64) != 6 {
		t.Errorf("put version = %v, want 6", ver["number"])
	}
	storage := put["body"].(map[string]any)["storage"].(map[string]any)
	if !strings.Contains(storage["value"].(string), "<strong>body</strong>") {
		t.Errorf("storage value = %q, want converted markdown", storage["value"])
	}
}

func TestParseFormat(t *testing.T) {
	for _, ok := range []string{"markdown", "storage", "adf"} {
		if _, err := ParseFormat(ok); err != nil {
			t.Errorf("ParseFormat(%q) errored: %v", ok, err)
		}
	}
	if _, err := ParseFormat("html"); err == nil {
		t.Error("ParseFormat(html) should error")
	}
}
