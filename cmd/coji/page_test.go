package main

import (
	"strings"
	"testing"

	"github.com/mgilbir/coji/internal/core"
)

func TestBuildTreeLabels(t *testing.T) {
	root := &core.TreeNode{
		ID: "1", Title: "Root", Type: "page",
		Children: []*core.TreeNode{
			{ID: "2", Title: "Folder", Type: "folder", Children: []*core.TreeNode{
				{ID: "3", Title: "Nested", Type: "page"},
			}},
			{ID: "4", Title: "Sibling", Type: "page"},
		},
	}
	out := buildTree(root).String()

	for _, want := range []string{"Root", "[page 1]", "Folder", "[folder 2]", "Nested", "[page 3]", "Sibling", "[page 4]"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output missing %q\n%s", want, out)
		}
	}
}
