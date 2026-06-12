package core

import (
	"context"

	"github.com/mgilbir/coji/internal/confluence"
)

// maxTreeDepth bounds recursive tree walks as a safety net against unexpectedly
// deep (or cyclic) hierarchies.
const maxTreeDepth = 100

// ListSpaces lists spaces, optionally filtered to a single key.
func (s *Service) ListSpaces(ctx context.Context, key string) ([]confluence.Space, error) {
	return s.client.ListSpaces(ctx, key)
}

// Children returns the direct children of a page (or folder, when forFolder is
// true) in the content tree.
func (s *Service) Children(ctx context.Context, id string, forFolder bool) ([]confluence.Child, error) {
	pt := confluence.TypePage
	if forFolder {
		pt = confluence.TypeFolder
	}
	return s.client.DirectChildren(ctx, pt, id)
}

// TreeNode is a node in a rendered content tree.
type TreeNode struct {
	ID       string
	Title    string
	Type     string
	Children []*TreeNode
}

// Tree builds the content subtree rooted at the given id, descending through
// pages and folders (the container types). The root is treated as a folder when
// rootIsFolder is set, otherwise a page.
func (s *Service) Tree(ctx context.Context, id string, rootIsFolder bool) (*TreeNode, error) {
	rootType := confluence.TypePage
	title := ""
	if rootIsFolder {
		rootType = confluence.TypeFolder
	} else {
		// Fetch the root page's title for a labeled root.
		if p, err := s.client.GetPage(ctx, id, ""); err == nil {
			title = p.Title
		}
	}
	root := &TreeNode{ID: id, Title: title, Type: string(rootType)}
	if err := s.fillChildren(ctx, root, rootType, id, 0); err != nil {
		return nil, err
	}
	return root, nil
}

// fillChildren recursively populates node.Children from the content tree.
func (s *Service) fillChildren(ctx context.Context, node *TreeNode, parentType confluence.ContentType, id string, depth int) error {
	if depth >= maxTreeDepth {
		return nil
	}
	children, err := s.client.DirectChildren(ctx, parentType, id)
	if err != nil {
		return err
	}
	for _, ch := range children {
		cn := &TreeNode{ID: ch.ID, Title: ch.Title, Type: string(ch.Type)}
		node.Children = append(node.Children, cn)
		// Only pages and folders can contain further children.
		if ch.Type == confluence.TypePage || ch.Type == confluence.TypeFolder {
			if err := s.fillChildren(ctx, cn, ch.Type, ch.ID, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
