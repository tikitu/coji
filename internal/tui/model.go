package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mgilbir/coji/internal/core"
)

// focus identifies which pane handles input.
type focus int

const (
	focusTree focus = iota // navigating the content tree
	focusPage              // reading a page's markdown
)

var (
	selectedStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	titleStyle    = lipgloss.NewStyle().Bold(true)
	errStyle      = lipgloss.NewStyle().Bold(true)
)

// model is the bubbletea model for `coji browse`.
type model struct {
	ctx context.Context
	svc *core.Service

	roots   []*node
	visible []*node
	cursor  int

	focus    focus
	viewport viewport.Model
	pageID   string // id whose body is in the viewport

	width, height int
	status        string
	err           error

	// selectedID is the last node the user marked (with "y") and is printed on
	// exit so it can feed `page create --parent`.
	selectedID string
}

func newModel(ctx context.Context, svc *core.Service, roots []*node) model {
	m := model{ctx: ctx, svc: svc, roots: roots}
	m.reflow()
	return m
}

func (m *model) reflow() { m.visible = flatten(m.roots) }

func (m model) Init() tea.Cmd {
	// Auto-expand the single root (a page/space the user passed) for an
	// immediately useful view; otherwise the roots (spaces) are already shown.
	if len(m.roots) == 1 && m.roots[0].container() {
		return loadChildren(m.ctx, m.svc, m.roots[0])
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewport = viewport.New(msg.Width, msg.Height-2)
		if m.focus == focusPage {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 2
		}
		return m, nil

	case childrenLoadedMsg:
		msg.parent.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		msg.parent.children = msg.children
		msg.parent.loaded = true
		msg.parent.expanded = true
		m.reflow()
		return m, nil

	case pageLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.focus = focusPage
		m.pageID = msg.id
		m.viewport = viewport.New(m.width, m.height-2)
		m.viewport.SetContent(msg.body)
		return m, nil

	case tea.KeyMsg:
		if m.focus == focusPage {
			return m.updatePage(msg)
		}
		return m.updateTree(msg)
	}
	return m, nil
}

func (m model) updateTree(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.visible) - 1
	case "enter", "right", "l", " ":
		return m.activate()
	case "left", "h":
		return m.collapseOrParent(), nil
	case "v":
		if n := m.current(); n != nil && n.isPage() {
			m.status = "loading page…"
			return m, loadPage(m.ctx, m.svc, n.id)
		}
	case "y":
		if n := m.current(); n != nil {
			m.selectedID = n.id
			m.status = fmt.Sprintf("selected %s (%s) — printed on exit", n.id, n.kind)
		}
	}
	return m, nil
}

// activate expands/collapses a container (loading children on first expand) or
// views a page leaf.
func (m model) activate() (tea.Model, tea.Cmd) {
	n := m.current()
	if n == nil {
		return m, nil
	}
	if n.container() {
		if !n.loaded {
			n.loading = true
			m.status = "loading…"
			return m, loadChildren(m.ctx, m.svc, n)
		}
		n.expanded = !n.expanded
		m.reflow()
		return m, nil
	}
	if n.isPage() {
		m.status = "loading page…"
		return m, loadPage(m.ctx, m.svc, n.id)
	}
	return m, nil
}

// collapseOrParent collapses the current node, or moves to its parent when it is
// already collapsed.
func (m model) collapseOrParent() model {
	n := m.current()
	if n == nil {
		return m
	}
	if n.expanded {
		n.expanded = false
		m.reflow()
		return m
	}
	if n.parent != nil {
		for i, v := range m.visible {
			if v == n.parent {
				m.cursor = i
				break
			}
		}
	}
	return m
}

func (m model) updatePage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "left", "h":
		m.focus = focusTree
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) current() *node {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.visible[m.cursor]
}

func (m model) View() string {
	if m.focus == focusPage {
		return m.pageView()
	}
	return m.treeView()
}

func (m model) treeView() string {
	var b strings.Builder
	for i, n := range m.visible {
		line := m.renderRow(n)
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m model) renderRow(n *node) string {
	indent := strings.Repeat("  ", n.depth)
	marker := "  "
	if n.container() {
		switch {
		case n.loading:
			marker = "⋯ "
		case n.expanded:
			marker = "▾ "
		default:
			marker = "▸ "
		}
	}
	kind := dimStyle.Render("[" + n.kind + "]")
	return fmt.Sprintf("%s%s%s %s", indent, marker, n.title, kind)
}

func (m model) pageView() string {
	header := titleStyle.Render("page " + m.pageID)
	help := dimStyle.Render("↑/↓ scroll · q/esc back")
	return header + "\n" + m.viewport.View() + "\n" + help
}

func (m model) footer() string {
	if m.err != nil {
		return errStyle.Render("error: " + m.err.Error())
	}
	if m.status != "" {
		return dimStyle.Render(m.status + "  ·  ↑/↓ move · enter expand · v view · y select id · q quit")
	}
	return dimStyle.Render("↑/↓ move · enter expand/collapse · v view page · y select id · q quit")
}
