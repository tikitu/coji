package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
)

// mdStyle captures the glamour rendering settings detected once, before
// bubbletea takes over the terminal. Detecting at render time (e.g. via
// glamour.WithAutoStyle) is unreliable inside bubbletea's managed alt-screen,
// where color-profile/background queries fall back to no color — which is why
// rendered markdown could come out as plain text.
type mdStyle struct {
	style   string          // "dark" or "light"
	profile termenv.Profile // forced color profile
}

// detectStyle determines the glamour style and color profile from the real
// terminal. Call this before starting the bubbletea program.
func detectStyle() mdStyle {
	style := "dark"
	if !termenv.HasDarkBackground() {
		style = "light"
	}
	profile := termenv.ColorProfile()
	// We always render for an interactive terminal; if detection reports no
	// color, assume a capable terminal so output is still styled.
	if profile == termenv.Ascii {
		profile = termenv.ANSI256
	}
	return mdStyle{style: style, profile: profile}
}

// renderMarkdown styles markdown for the terminal with glamour, using the
// pre-detected style/profile and word-wrapped to width. It falls back to the
// raw markdown if a renderer can't be built or rendering fails, so the viewer
// never ends up blank.
func renderMarkdown(md string, width int, st mdStyle) string {
	if width < 20 {
		width = 80
	}
	style := st.style
	if style == "" {
		style = "dark"
	}
	profile := st.profile
	if profile == termenv.Ascii {
		profile = termenv.ANSI256
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithColorProfile(profile),
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
