package tui

import "github.com/charmbracelet/glamour"

// renderMarkdown styles markdown for the terminal with glamour, word-wrapped to
// width. It falls back to the raw markdown if a renderer can't be built or
// rendering fails, so the viewer never ends up blank.
func renderMarkdown(md string, width int) string {
	if width < 20 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-2),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
