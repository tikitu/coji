// Package tui is the interactive bubbletea front-end for coji. It browses the
// Confluence content tree (spaces → pages/folders) with lazy-loaded children
// and a scrollable page viewer. Like the CLI, it consumes internal/core only.
package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mgilbir/coji/internal/confluence"
	"github.com/mgilbir/coji/internal/core"
)

// childrenLoadedMsg carries the result of fetching a node's children.
type childrenLoadedMsg struct {
	parent   *node
	children []*node
	err      error
}

// pageLoadedMsg carries a page's body rendered as markdown.
type pageLoadedMsg struct {
	id   string
	body string
	err  error
}

// loadChildren fetches a container node's children in the background.
func loadChildren(ctx context.Context, svc *core.Service, n *node) tea.Cmd {
	return func() tea.Msg {
		kids, err := svc.Children(ctx, n.loadID, n.isFolder())
		if err != nil {
			return childrenLoadedMsg{parent: n, err: err}
		}
		nodes := make([]*node, 0, len(kids))
		for _, ch := range kids {
			nodes = append(nodes, &node{
				id:      ch.ID,
				loadID:  ch.ID,
				title:   ch.Title,
				kind:    string(ch.Type),
				spaceID: ch.SpaceID,
				depth:   n.depth + 1,
				parent:  n,
			})
		}
		return childrenLoadedMsg{parent: n, children: nodes}
	}
}

// loadPage fetches a page rendered as markdown for the viewer.
func loadPage(ctx context.Context, svc *core.Service, id string) tea.Cmd {
	return func() tea.Msg {
		p, err := svc.GetPage(ctx, id, core.FormatMarkdown)
		if err != nil {
			return pageLoadedMsg{id: id, err: err}
		}
		return pageLoadedMsg{id: id, body: p.Body}
	}
}

// spaceNode builds a root node for a space; its children load from the space
// homepage (the content root).
func spaceNode(s confluence.Space) *node {
	title := s.Name
	if s.Key != "" {
		title = fmt.Sprintf("%s (%s)", s.Name, s.Key)
	}
	return &node{
		id:      s.ID,
		loadID:  s.HomepageID,
		title:   title,
		kind:    "space",
		spaceID: s.ID,
	}
}

// Run starts the interactive browser. start may be empty (list all spaces), a
// numeric page id (root at that page), or a space key (root at that space). On
// exit, any id the user selected with "y" is printed to stdout, so it can feed
// `coji page create --parent`.
func Run(ctx context.Context, svc *core.Service, start string) error {
	roots, err := rootsFor(ctx, svc, start)
	if err != nil {
		return err
	}

	p := tea.NewProgram(newModel(ctx, svc, roots), tea.WithAltScreen(), tea.WithContext(ctx))
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(model); ok && fm.selectedID != "" {
		fmt.Println(fm.selectedID)
	}
	return nil
}

// rootsFor resolves the starting roots from the start argument.
func rootsFor(ctx context.Context, svc *core.Service, start string) ([]*node, error) {
	switch {
	case start == "":
		spaces, err := svc.ListSpaces(ctx, "")
		if err != nil {
			return nil, err
		}
		roots := make([]*node, 0, len(spaces))
		for _, s := range spaces {
			roots = append(roots, spaceNode(s))
		}
		return roots, nil

	case isNumeric(start):
		return []*node{{id: start, loadID: start, kind: string(confluence.TypePage), title: "page " + start}}, nil

	default:
		spaces, err := svc.ListSpaces(ctx, start)
		if err != nil {
			return nil, err
		}
		if len(spaces) == 0 {
			return nil, fmt.Errorf("no space found with key %q", start)
		}
		return []*node{spaceNode(spaces[0])}, nil
	}
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
