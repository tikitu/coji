package tui

import "github.com/mgilbir/coji/internal/confluence"

// node is one entry in the browsable content tree. Spaces, pages, and folders
// are all containers whose children load lazily on first expand.
type node struct {
	id    string // content id (page/folder) or space id
	title string
	kind  string // "space", "page", "folder", or another content type

	// loadID is the id used to fetch children. For spaces this is the
	// homepage id (the space's content root); otherwise it equals id.
	loadID string

	depth    int
	expanded bool
	loaded   bool // children have been fetched
	loading  bool // a fetch is in flight
	children []*node
	parent   *node
}

// container reports whether the node can hold children (and is thus
// expandable). Only pages and folders descend further; spaces expand into their
// homepage's children.
func (n *node) container() bool {
	switch n.kind {
	case "space", string(confluence.TypePage), string(confluence.TypeFolder):
		return true
	}
	return false
}

// isFolder reports whether children should be fetched via the folder endpoint.
func (n *node) isFolder() bool {
	return n.kind == string(confluence.TypeFolder)
}

// isPage reports whether the node is a viewable page.
func (n *node) isPage() bool {
	return n.kind == string(confluence.TypePage)
}

// flatten returns the visible nodes in display order: a depth-first walk that
// descends only into expanded nodes.
func flatten(roots []*node) []*node {
	var out []*node
	var walk func(ns []*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			out = append(out, n)
			if n.expanded {
				walk(n.children)
			}
		}
	}
	walk(roots)
	return out
}
