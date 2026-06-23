package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgilbir/coji/internal/confluence"
	"gopkg.in/yaml.v3"
)

func newSvc(srv *httptest.Server) *Service {
	return New(confluence.New(srv.Client(), srv.URL))
}

func TestGetPageMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("body-format"); got != "storage" {
			t.Errorf("body-format = %q, want storage", got)
		}
		io.WriteString(w, `{"id":"7","title":"T","createdAt":"2019-01-02T03:04:05.000Z",
			"version":{"number":3,"createdAt":"2024-05-06T07:08:09.000Z"},
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
	if p.Created != "2019-01-02T03:04:05.000Z" {
		t.Errorf("created = %q, want the page creation date", p.Created)
	}
	if p.Updated != "2024-05-06T07:08:09.000Z" {
		t.Errorf("updated = %q, want the current version's creation date", p.Updated)
	}
	if fm := p.Frontmatter(); !strings.Contains(fm, `created: "2019-01-02T03:04:05.000Z"`) {
		t.Errorf("frontmatter missing created line:\n%s", fm)
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

// TestFrontmatterIsValidYAML checks the output is a genuine YAML frontmatter
// block — fenced by --- delimiters and parseable as YAML into exactly the
// expected key/value mapping — rather than merely containing the right
// substrings somewhere. A title with quotes and a colon exercises the quoting.
func TestFrontmatterIsValidYAML(t *testing.T) {
	p := &Page{
		ID:       "123",
		Title:    `Tricky: title with "quotes" & a colon`,
		SpaceID:  "900",
		SpaceKey: "ENG",
		Version:  4,
		Created:  "2019-01-02T03:04:05.000Z",
		Updated:  "2024-05-06T07:08:09.000Z",
		WebURL:   "https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Tricky",
	}
	fm := p.Frontmatter()

	// Must be fenced: opens with a --- line and the metadata is closed by a
	// --- line, with nothing but the trailing blank line after it.
	rest, ok := strings.CutPrefix(fm, "---\n")
	if !ok {
		t.Fatalf("frontmatter must open with a --- delimiter line:\n%s", fm)
	}
	yamlBlock, after, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatalf("frontmatter must close with a --- delimiter line:\n%s", fm)
	}
	if strings.TrimSpace(after) != "" {
		t.Errorf("nothing but blank space should follow the closing delimiter, got %q", after)
	}

	// The fenced block must parse as YAML into exactly the expected mapping.
	got := map[string]any{}
	if err := yaml.Unmarshal([]byte(yamlBlock), &got); err != nil {
		t.Fatalf("frontmatter block is not valid YAML: %v\nblock:\n%s", err, yamlBlock)
	}
	want := map[string]any{
		"title":   p.Title,
		"id":      p.ID,
		"version": p.Version,
		"created": p.Created,
		"updated": p.Updated,
		"space":   p.SpaceKey,
		"spaceId": p.SpaceID,
		"source":  p.WebURL,
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys (%v), want %d", len(got), got, len(want))
	}
	for k, wv := range want {
		gv, present := got[k]
		if !present {
			t.Errorf("frontmatter missing key %q", k)
			continue
		}
		// version decodes to an int; the rest to strings. Compare by value.
		if fmt.Sprint(gv) != fmt.Sprint(wv) {
			t.Errorf("frontmatter[%q] = %#v, want %#v", k, gv, wv)
		}
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
