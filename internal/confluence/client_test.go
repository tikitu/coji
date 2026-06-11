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
