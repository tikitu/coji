package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mgilbir/coji/internal/core"
)

// focus identifies which pane handles input.
type focus int

const (
	focusTree     focus = iota // navigating the content tree
	focusPage                  // reading a page's markdown
	focusNewTitle              // entering a title for a new child page
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
	pageBody string // raw markdown of the page being viewed

	width, height int
	status        string
	err           error

	// External-editor session state.
	editMode editMode
	editID   string // page being edited (existing)
	editPath string // temp file backing the editor
	editOrig string // original content, to detect "no changes"

	// Pending new-child-page state.
	titleInput  textinput.Model
	newParent   *node
	newSpaceID  string
	newParentID string // parent id for the new page ("" = under homepage)
	newPrivate  bool   // create the new page as private

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
		if m.focus == focusPage {
			m.refreshViewport() // re-wrap markdown to the new width
		} else {
			m.viewport = viewport.New(msg.Width, msg.Height-2)
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
		m.pageBody = msg.body
		m.refreshViewport()
		return m, nil

	case editLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		path, err := writeTempMarkdown(msg.body)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.editMode = editExisting
		m.editID = msg.id
		m.editOrig = msg.body
		m.editPath = path
		m.status = "editing in $EDITOR…"
		return m, runEditor(path)

	case parentInfoMsg:
		if msg.err != nil {
			m.err = msg.err
			m.focus = focusTree
			return m, nil
		}
		m.newSpaceID = msg.spaceID
		m.newParentID = msg.parentID
		path, err := writeTempMarkdown("")
		if err != nil {
			m.err = err
			return m, nil
		}
		m.editMode = editNew
		m.editPath = path
		m.focus = focusTree
		m.status = "composing new page in $EDITOR…"
		return m, runEditor(path)

	case editorClosedMsg:
		return m.handleEditorClosed(msg)

	case savedMsg:
		m.editMode = editNone
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.status = fmt.Sprintf("saved %s (v%d)", msg.id, msg.version)
		if m.focus == focusPage && m.pageID == msg.id {
			m.pageBody = msg.body
			m.refreshViewport()
		}
		return m, nil

	case createdMsg:
		m.editMode = editNone
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.status = fmt.Sprintf("created %q (%s)", msg.title, msg.id)
		// Refresh the parent's children so the new page appears.
		if msg.parent != nil {
			msg.parent.loaded = false
			return m, loadChildren(m.ctx, m.svc, msg.parent)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.focus {
		case focusPage:
			return m.updatePage(msg)
		case focusNewTitle:
			return m.updateNewTitle(msg)
		default:
			return m.updateTree(msg)
		}
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
	case "e":
		if n := m.current(); n != nil && n.isPage() {
			m.status = "loading page for edit…"
			return m, fetchForEdit(m.ctx, m.svc, n.id)
		}
	case "n":
		if n := m.current(); n != nil && n.container() {
			return m.beginNewPage(n), nil
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
	case "e":
		m.status = "loading page for edit…"
		return m, fetchForEdit(m.ctx, m.svc, m.pageID)
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// beginNewPage opens the title prompt for a new child under n.
func (m model) beginNewPage(n *node) model {
	ti := textinput.New()
	ti.Placeholder = "New page title"
	ti.Focus()
	ti.CharLimit = 255
	m.titleInput = ti
	m.newParent = n
	m.newPrivate = false
	m.focus = focusNewTitle
	m.err = nil
	m.status = ""
	return m
}

// updateNewTitle handles the title-entry prompt for a new child page. On Enter
// it resolves the parent's space/parent ids and opens the editor for the body.
func (m model) updateNewTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.focus = focusTree
		m.status = "new page cancelled"
		return m, nil
	case "tab", "ctrl+p":
		m.newPrivate = !m.newPrivate
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.titleInput.Value())
		if title == "" {
			m.status = "title cannot be empty"
			return m, nil
		}
		m.status = "resolving location…"
		return m, resolveParent(m.ctx, m.svc, m.newParent)
	}
	var cmd tea.Cmd
	m.titleInput, cmd = m.titleInput.Update(msg)
	return m, cmd
}

// handleEditorClosed processes the result of an external-editor session: save
// an existing page, or create the composed new child page.
func (m model) handleEditorClosed(msg editorClosedMsg) (tea.Model, tea.Cmd) {
	mode := m.editMode
	path := m.editPath
	m.editPath = ""
	if msg.err != nil {
		m.editMode = editNone
		m.err = fmt.Errorf("editor: %w", msg.err)
		return m, nil
	}
	content, err := readAndRemove(path)
	if err != nil {
		m.editMode = editNone
		m.err = err
		return m, nil
	}

	switch mode {
	case editExisting:
		if content == m.editOrig {
			m.editMode = editNone
			m.status = "no changes"
			return m, nil
		}
		m.status = "saving…"
		return m, saveEdit(m.ctx, m.svc, m.editID, content)
	case editNew:
		title := strings.TrimSpace(m.titleInput.Value())
		m.status = "creating…"
		return m, createChild(m.ctx, m.svc, m.newParent, m.newSpaceID, m.newParentID, title, content, m.newPrivate)
	}
	m.editMode = editNone
	return m, nil
}

// refreshViewport sizes the viewport to the current window and fills it with
// the page body rendered (and word-wrapped) by glamour.
func (m *model) refreshViewport() {
	w, h := m.width, m.height-2
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 1
	}
	m.viewport = viewport.New(w, h)
	m.viewport.SetContent(renderMarkdown(m.pageBody, w))
}

func (m model) current() *node {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.visible[m.cursor]
}

func (m model) View() string {
	switch m.focus {
	case focusPage:
		return m.pageView()
	case focusNewTitle:
		return m.newTitleView()
	default:
		return m.treeView()
	}
}

func (m model) newTitleView() string {
	parent := "(space root)"
	if m.newParent != nil {
		parent = fmt.Sprintf("%s [%s]", m.newParent.title, m.newParent.kind)
	}
	private := "no"
	if m.newPrivate {
		private = "yes"
	}
	return titleStyle.Render("New child page") + "\n\n" +
		dimStyle.Render("under:   "+parent) + "\n" +
		dimStyle.Render("private: "+private+"  (tab to toggle)") + "\n\n" +
		m.titleInput.View() + "\n\n" +
		dimStyle.Render("enter to compose body in $EDITOR · esc to cancel")
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
	help := dimStyle.Render("↑/↓ scroll · e edit · q/esc back")
	return header + "\n" + m.viewport.View() + "\n" + help
}

func (m model) footer() string {
	if m.err != nil {
		return errStyle.Render("error: " + m.err.Error())
	}
	help := "↑/↓ move · enter expand · v view · e edit · n new child · y select id · q quit"
	if m.status != "" {
		return dimStyle.Render(m.status + "  ·  " + help)
	}
	return dimStyle.Render(help)
}
