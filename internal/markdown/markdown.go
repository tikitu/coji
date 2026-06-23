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
	stdhtml "html"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// converter is configured once and reused. GFM gives us tables, strikethrough,
// task lists and autolinks; XHTML emits well-formed self-closing tags expected
// by the storage format; Unsafe lets raw HTML/macro snippets in the source pass
// through untouched, so power users can embed storage markup directly. The
// confluenceLinkTransformer reconstructs <ac:link> markup from the
// confluence:// descriptors that FromStorage emits, so internal links survive a
// fetch→edit→save round-trip.
var converter = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithASTTransformers(util.Prioritized(confluenceLinkTransformer{}, 100)),
	),
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

var confluenceScheme = []byte("confluence://")

// confluenceLinkTransformer rewrites Markdown links whose destination is a
// confluence:// descriptor (as produced by FromStorage) back into Confluence
// <ac:link> storage markup, so internal links round-trip. Links with any other
// destination are left untouched. Resolved https:// links are deliberately not
// reconstructed — by then they're plain external links and the descriptor info
// is gone — which is why link resolution is kept off the edit path.
type confluenceLinkTransformer struct{}

func (confluenceLinkTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()

	// Collect first, mutate after: replacing nodes mid-walk is fragile.
	var links []*ast.Link
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if l, ok := n.(*ast.Link); ok && bytes.HasPrefix(l.Destination, confluenceScheme) {
				links = append(links, l)
			}
		}
		return ast.WalkContinue, nil
	})

	for _, l := range links {
		markup, ok := confluenceLinkStorage(string(l.Destination), nodeText(l, source))
		if !ok {
			continue // unknown scheme: leave it as an ordinary link
		}
		parent := l.Parent()
		if parent == nil {
			continue
		}
		// A String node marked "code" is written verbatim by the html renderer
		// (no HTML-escaping), which is what we need for raw storage markup.
		s := ast.NewString([]byte(markup))
		s.SetCode(true)
		parent.ReplaceChild(parent, l, s)
	}
}

// confluenceLinkStorage rebuilds the <ac:link> storage markup for a
// confluence:// descriptor and its display text. It returns ok=false for an
// unrecognized scheme, leaving the link to render normally.
//
// Best-effort: the descriptor preserves enough for the common references (page,
// blog-post, attachment-by-filename, space, user, same-page anchor). It does
// not carry cross-page attachment containers, smart-link card attributes, or
// rich link-body formatting, so those degrade.
func confluenceLinkStorage(dest, linkText string) (string, bool) {
	rest := strings.TrimPrefix(dest, "confluence://")
	scheme, rawQuery, _ := strings.Cut(rest, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", false
	}

	anchor := q.Get("anchor")
	var ref string
	switch scheme {
	case "page", "blog-post":
		title, id := q.Get("title"), q.Get("id")
		// title/id absent means a same-page anchor link: no resource reference.
		if title != "" || id != "" {
			ref = "<ri:" + scheme +
				riAttr("ri:space-key", q.Get("space")) +
				riAttr("ri:content-title", title) +
				riAttr("ri:content-id", id) + " />"
		}
	case "attachment":
		ref = "<ri:attachment" + riAttr("ri:filename", q.Get("filename")) + " />"
	case "space":
		ref = "<ri:space" + riAttr("ri:space-key", q.Get("space")) + " />"
	case "user":
		ref = "<ri:user" + riAttr("ri:account-id", q.Get("account-id")) + " />"
	default:
		return "", false
	}

	var b strings.Builder
	b.WriteString("<ac:link")
	if anchor != "" {
		b.WriteString(` ac:anchor="` + stdhtml.EscapeString(anchor) + `"`)
	}
	b.WriteString(">")
	b.WriteString(ref)
	b.WriteString("<ac:link-body>" + stdhtml.EscapeString(linkText) + "</ac:link-body>")
	b.WriteString("</ac:link>")
	return b.String(), true
}

// riAttr renders a single resource-reference attribute, or "" when the value is
// empty (so absent fields don't emit empty attributes).
func riAttr(name, val string) string {
	if val == "" {
		return ""
	}
	return " " + name + `="` + stdhtml.EscapeString(val) + `"`
}

// nodeText collects the plain-text content of an inline node's descendants,
// used as an <ac:link>'s body. Inline formatting inside a link body is not
// preserved.
func nodeText(n ast.Node, source []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(source))
		case *ast.String:
			b.Write(t.Value)
		default:
			b.WriteString(nodeText(c, source))
		}
	}
	return b.String()
}
