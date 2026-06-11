package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mgilbir/coji/internal/core"
)

// editMode tracks what an in-progress external-editor session will do on close.
type editMode int

const (
	editNone     editMode = iota
	editExisting          // editing an existing page's body
	editNew               // composing a new child page's body
)

// --- messages ---

// editLoadedMsg carries an existing page's markdown, fetched so it can be
// opened in the editor.
type editLoadedMsg struct {
	id   string
	body string
	err  error
}

// parentInfoMsg carries the resolved space/parent ids for a new child page.
type parentInfoMsg struct {
	parentNode *node
	spaceID    string
	parentID   string
	err        error
}

// editorClosedMsg is sent when the external editor process exits.
type editorClosedMsg struct{ err error }

// savedMsg is the result of saving an edited page.
type savedMsg struct {
	id      string
	body    string
	version int
	err     error
}

// createdMsg is the result of creating a new child page.
type createdMsg struct {
	parent *node
	id     string
	title  string
	err    error
}

// --- commands ---

// fetchForEdit loads a page's markdown so it can be edited.
func fetchForEdit(ctx context.Context, svc *core.Service, id string) tea.Cmd {
	return func() tea.Msg {
		p, err := svc.GetPage(ctx, id, core.FormatMarkdown)
		if err != nil {
			return editLoadedMsg{id: id, err: err}
		}
		return editLoadedMsg{id: id, body: p.Body}
	}
}

// resolveParent determines the space id and parent id for creating a child
// under n. A space parents at its homepage (empty parentId); a page/folder
// parents directly, fetching the space id only if the node didn't carry it.
func resolveParent(ctx context.Context, svc *core.Service, n *node) tea.Cmd {
	return func() tea.Msg {
		if n.kind == "space" {
			return parentInfoMsg{parentNode: n, spaceID: n.spaceID, parentID: ""}
		}
		spaceID := n.spaceID
		if spaceID == "" {
			p, err := svc.GetPage(ctx, n.id, "")
			if err != nil {
				return parentInfoMsg{parentNode: n, err: err}
			}
			spaceID = p.SpaceID
		}
		return parentInfoMsg{parentNode: n, spaceID: spaceID, parentID: n.id}
	}
}

// runEditor opens path in the user's editor ($VISUAL, then $EDITOR, else vi),
// suspending the TUI until the editor exits.
func runEditor(path string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	args := append(parts[1:], path)
	c := exec.Command(parts[0], args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorClosedMsg{err: err}
	})
}

// saveEdit writes new body content to an existing page (bumping the version).
func saveEdit(ctx context.Context, svc *core.Service, id, body string) tea.Cmd {
	return func() tea.Msg {
		p, err := svc.EditPage(ctx, core.EditInput{
			ID:         id,
			Format:     core.FormatMarkdown,
			Content:    body,
			VersionMsg: "edited via coji browse",
		})
		if err != nil {
			return savedMsg{id: id, err: err}
		}
		return savedMsg{id: id, body: body, version: p.Version}
	}
}

// createChild creates a new page under parent with the given title and body.
func createChild(ctx context.Context, svc *core.Service, parent *node, spaceID, parentID, title, body string) tea.Cmd {
	return func() tea.Msg {
		p, err := svc.CreatePage(ctx, core.CreateInput{
			SpaceID:  spaceID,
			ParentID: parentID,
			Title:    title,
			Format:   core.FormatMarkdown,
			Content:  body,
		})
		if err != nil {
			return createdMsg{parent: parent, title: title, err: err}
		}
		return createdMsg{parent: parent, id: p.ID, title: title}
	}
}

// writeTempMarkdown writes content to a new temp .md file and returns its path.
func writeTempMarkdown(content string) (string, error) {
	f, err := os.CreateTemp("", "coji-*.md")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// readAndRemove reads a temp file's contents and deletes it.
func readAndRemove(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	_ = os.Remove(path)
	return string(data), nil
}
