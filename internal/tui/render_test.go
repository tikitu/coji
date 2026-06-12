package tui

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

func TestRenderMarkdownStyled(t *testing.T) {
	st := mdStyle{style: "dark", profile: termenv.ANSI256}
	out := renderMarkdown("# Hello\n\nSome **bold** text.", 80, st)
	if strings.TrimSpace(out) == "" {
		t.Fatal("rendered output is empty")
	}
	// With a forced color profile, glamour must emit ANSI escapes (this is
	// what was missing when auto-detection fell back to plain text).
	if !strings.ContainsRune(out, 0x1b) {
		t.Errorf("expected ANSI escape codes in styled output, got plain text:\n%q", out)
	}
	// And the heading marker should be styled away, not left literal.
	if strings.Contains(out, "# Hello") {
		t.Errorf("heading rendered as literal markdown:\n%q", out)
	}
}

func TestRenderMarkdownFallbackWidth(t *testing.T) {
	st := mdStyle{style: "dark", profile: termenv.ANSI256}
	out := renderMarkdown("plain text", 5, st)
	// Glamour interleaves ANSI codes between words, so check the words rather
	// than a contiguous substring.
	for _, w := range []string{"plain", "text"} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q: %q", w, out)
		}
	}
}
