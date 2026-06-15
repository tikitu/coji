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
	"github.com/mgilbir/coji/internal/policy"
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

func TestBuildCQL(t *testing.T) {
	if got := buildCQL("nda template", ""); got != `type=page AND text ~ "nda template"` {
		t.Errorf("buildCQL = %q", got)
	}
	if got := buildCQL("nda", "HR"); got != `type=page AND text ~ "nda" AND space="HR"` {
		t.Errorf("buildCQL with space = %q", got)
	}
	// Quotes and backslashes in the query must be escaped, not break the CQL.
	if got := buildCQL(`a"b\c`, ""); got != `type=page AND text ~ "a\"b\\c"` {
		t.Errorf("buildCQL escaping = %q", got)
	}
}

func TestCleanExcerpt(t *testing.T) {
	// Markers stripped, HTML entities decoded, whitespace/newlines collapsed.
	in := "the @@@hl@@@policy@@@endhl@@@ covers\nintake &amp; disposal &#39;rules&#39;"
	want := "the policy covers intake & disposal 'rules'"
	if got := cleanExcerpt(in); got != want {
		t.Errorf("cleanExcerpt = %q, want %q", got, want)
	}
}

func TestParseSpaceAndID(t *testing.T) {
	key, id := parseSpaceAndID("/spaces/DOCS/pages/111111/Some+Title")
	if key != "DOCS" || id != "111111" {
		t.Errorf("parseSpaceAndID = %q,%q want DOCS,111111", key, id)
	}
	if k, i := parseSpaceAndID(""); k != "" || i != "" {
		t.Errorf("parseSpaceAndID(\"\") = %q,%q want empty", k, i)
	}
}

// newSearchSvc points a Service at a test server, using a v2-style base so the
// client's v1 suffix-swap is exercised. siteURL lets the Service build absolute
// WebURLs; pol (may be nil) gates results by the read policy.
func newSearchSvc(srv *httptest.Server, siteURL string, pol *policy.Policy) *Service {
	return New(confluence.New(srv.Client(), srv.URL+"/api/v2"),
		WithSiteURL(siteURL), WithPolicy(pol))
}

func TestServiceSearch(t *testing.T) {
	// One hit with a space key, one missing it (must fall back to the url path),
	// one in a space the policy will deny.
	const env = `{"results":[
		{"content":{"id":"111111","type":"page","title":"Widget policy",
			"space":{"key":"DOCS"}},
		 "title":"Widget policy",
		 "excerpt":"the @@@hl@@@widget@@@endhl@@@ rules\napply here",
		 "url":"/spaces/DOCS/pages/111111/Widget+policy",
		 "lastModified":"2025-02-03T11:22:33.000Z"},
		{"content":{"id":"","type":"page","title":"No-space page","space":{"key":""}},
		 "title":"No-space page","excerpt":"fallback case",
		 "url":"/spaces/DOCS/pages/222222/No+space+page",
		 "lastModified":"2025-03-01T00:00:00.000Z"},
		{"content":{"id":"333333","type":"page","title":"Secret","space":{"key":"OPS"}},
		 "title":"Secret","excerpt":"hidden",
		 "url":"/spaces/OPS/pages/333333/Secret","lastModified":"2025-04-01T00:00:00.000Z"}
	],"_links":{}}`

	var gotCQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCQL = r.URL.Query().Get("cql")
		io.WriteString(w, env)
	}))
	defer srv.Close()

	// Deny reads in OPS; allow everything else.
	pol := &policy.Policy{
		Default: policy.Set{policy.OpRead: true},
		Spaces:  map[string]policy.Set{"OPS": {}},
	}
	hits, err := newSearchSvc(srv, "https://example.atlassian.net", pol).
		Search(context.Background(), "widget", SearchOptions{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}

	if gotCQL != `type=page AND text ~ "widget"` {
		t.Errorf("cql = %q", gotCQL)
	}
	// OPS hit dropped by policy → 2 hits remain.
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2 (OPS dropped by policy)", len(hits))
	}

	h0 := hits[0]
	if h0.Excerpt != "the widget rules apply here" {
		t.Errorf("excerpt not cleaned: %q", h0.Excerpt)
	}
	if h0.WebURL != "https://example.atlassian.net/wiki/spaces/DOCS/pages/111111/Widget+policy" {
		t.Errorf("WebURL = %q", h0.WebURL)
	}

	// Second hit had empty space/id in content → recovered from the url path.
	if hits[1].SpaceKey != "DOCS" || hits[1].ID != "222222" {
		t.Errorf("fallback hit = %+v, want space DOCS id 222222", hits[1])
	}
}

func TestServiceSearchRawCQL(t *testing.T) {
	var gotCQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCQL = r.URL.Query().Get("cql")
		io.WriteString(w, `{"results":[],"_links":{}}`)
	}))
	defer srv.Close()

	_, err := newSearchSvc(srv, "", nil).
		Search(context.Background(), `label="x" AND type=page`, SearchOptions{RawCQL: true})
	if err != nil {
		t.Fatal(err)
	}
	if gotCQL != `label="x" AND type=page` {
		t.Errorf("raw cql passed through = %q", gotCQL)
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
