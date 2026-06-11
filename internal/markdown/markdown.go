// Package markdown converts between Markdown and Confluence "storage" format
// (an XHTML-based representation).
//
// Markdown -> storage uses goldmark to render XHTML, which the storage format
// accepts directly. storage -> Markdown walks the XHTML with x/net/html and
// emits Markdown, degrading gracefully (recursing into unknown/macro elements
// so their text survives).
package markdown

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// converter is configured once and reused. GFM gives us tables, strikethrough,
// task lists and autolinks; XHTML emits well-formed self-closing tags expected
// by the storage format; Unsafe lets raw HTML/macro snippets in the source pass
// through untouched, so power users can embed storage markup directly.
var converter = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(
		html.WithXHTML(),
		html.WithUnsafe(),
	),
)

// ToStorage converts Markdown source to Confluence storage-format XHTML.
func ToStorage(md []byte) (string, error) {
	var buf bytes.Buffer
	if err := converter.Convert(md, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
