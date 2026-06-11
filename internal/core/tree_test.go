package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgilbir/coji/internal/confluence"
)

// TestTreeDescendsFolders verifies the tree walk fetches the root title, lists
// direct children, and recurses into both pages and folders.
func TestTreeDescendsFolders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pages/root":
			io.WriteString(w, `{"id":"root","title":"Root"}`)
		case "/pages/root/direct-children":
			io.WriteString(w, `{"results":[
				{"id":"f1","type":"folder","title":"Folder"},
				{"id":"p1","type":"page","title":"Child"}],"_links":{}}`)
		case "/folders/f1/direct-children":
			io.WriteString(w, `{"results":[{"id":"p2","type":"page","title":"In folder"}],"_links":{}}`)
		case "/pages/p1/direct-children", "/pages/p2/direct-children":
			io.WriteString(w, `{"results":[],"_links":{}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := New(confluence.New(srv.Client(), srv.URL))
	root, err := svc.Tree(context.Background(), "root", false)
	if err != nil {
		t.Fatal(err)
	}
	if root.Title != "Root" || len(root.Children) != 2 {
		t.Fatalf("root = %+v", root)
	}
	folder := root.Children[0]
	if folder.Type != "folder" || len(folder.Children) != 1 || folder.Children[0].Title != "In folder" {
		t.Errorf("folder subtree = %+v", folder)
	}
	if root.Children[1].Title != "Child" {
		t.Errorf("second child = %+v", root.Children[1])
	}
}
