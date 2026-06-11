package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	out := renderMarkdown("# Hello\n\nSome **bold** text.", 80)
	if strings.TrimSpace(out) == "" {
		t.Fatal("rendered output is empty")
	}
	// Glamour adds ANSI styling but preserves the text content.
	for _, want := range []string{"Hello", "bold"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderMarkdownFallback(t *testing.T) {
	// A tiny width is clamped, not an error; output should still contain text.
	out := renderMarkdown("plain text", 5)
	if !strings.Contains(out, "plain text") {
		t.Errorf("output missing text: %q", out)
	}
}
