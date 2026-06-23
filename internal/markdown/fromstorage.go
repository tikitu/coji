package markdown

import (
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Options configure storage→markdown conversion.
type Options struct {
	// SpaceKey is the key of the space containing the page being converted. It
	// qualifies internal links that omit an explicit space (Confluence stores a
	// space key only for cross-space links, so same-space links rely on this).
	SpaceKey string

	// ResolvePageURL, if set, maps an internal page link (space key + title) to
	// an absolute, browser-clickable URL. Returning ok=false (or leaving this
	// nil) falls back to the lossless confluence:// descriptor.
	ResolvePageURL func(spaceKey, title string) (url string, ok bool)
}

// FromStorage converts Confluence storage-format XHTML to Markdown. It handles
// the standard block and inline HTML elements; elements it doesn't recognize
// (e.g. Confluence macros) are descended into so their text content survives.
//
// An optional Options qualifies internal Confluence links (see Options.SpaceKey).
func FromStorage(storage string, opts ...Options) (string, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(storage), ctx)
	if err != nil {
		return "", fmt.Errorf("parsing storage: %w", err)
	}
	c := conv{}
	if len(opts) > 0 {
		c.opts = opts[0]
	}
	out := c.blocks(nodes, "")
	return strings.Trim(out, "\n") + "\n", nil
}

type conv struct{ opts Options }

// children returns a node's child nodes as a slice for convenient iteration.
func children(n *html.Node) []*html.Node {
	var out []*html.Node
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		out = append(out, ch)
	}
	return out
}

// blockTags are rendered as standalone blocks separated by blank lines.
var blockTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "ul": true, "ol": true, "pre": true, "blockquote": true,
	"table": true, "hr": true, "div": true,
}

func isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if blockTags[n.Data] {
		return true
	}
	// Confluence code macro renders as a fenced block.
	return n.Data == "ac:structured-macro" && attr(n, "ac:name") == "code"
}

// blocks renders sibling nodes as block-level markdown. Runs of inline content
// between blocks are gathered into paragraphs. indent is prepended to each
// produced line (used for nested list content).
func (c *conv) blocks(nodes []*html.Node, indent string) string {
	var out []string
	var inline strings.Builder

	flush := func() {
		s := strings.TrimSpace(inline.String())
		inline.Reset()
		if s != "" {
			out = append(out, indentLines(s, indent))
		}
	}

	for _, n := range nodes {
		if isBlock(n) {
			flush()
			if b := c.block(n, indent); strings.TrimSpace(b) != "" {
				out = append(out, b)
			}
		} else {
			inline.WriteString(c.inline([]*html.Node{n}))
		}
	}
	flush()
	return strings.Join(out, "\n\n")
}

// block renders a single block-level element.
func (c *conv) block(n *html.Node, indent string) string {
	switch n.Data {
	case "p", "div":
		return indentLines(strings.TrimSpace(c.inline(allChildren(n))), indent)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(n.Data[1] - '0')
		return indent + strings.Repeat("#", level) + " " + strings.TrimSpace(c.inline(allChildren(n)))
	case "hr":
		return indent + "---"
	case "blockquote":
		inner := c.blocks(children(n), "")
		return prefixLines(inner, indent+"> ")
	case "pre":
		return c.codeBlock(textContent(n), preLang(n), indent)
	case "ac:structured-macro":
		return c.codeBlock(macroCode(n), macroLang(n), indent)
	case "ul":
		return c.list(n, indent, false)
	case "ol":
		return c.list(n, indent, true)
	case "table":
		return c.table(n, indent)
	}
	// Unknown block: recurse so nested content is not lost.
	return c.blocks(children(n), indent)
}

// list renders ul/ol. Nested lists are indented two spaces under their item.
func (c *conv) list(n *html.Node, indent string, ordered bool) string {
	var lines []string
	i := 0
	for _, li := range children(n) {
		if li.Type != html.ElementNode || li.Data != "li" {
			continue
		}
		i++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", i)
		}

		// Split the item's children into inline lead content and nested blocks.
		var inlineNodes, nestedLists []*html.Node
		for _, ch := range children(li) {
			if ch.Type == html.ElementNode && (ch.Data == "ul" || ch.Data == "ol") {
				nestedLists = append(nestedLists, ch)
			} else {
				inlineNodes = append(inlineNodes, ch)
			}
		}

		lead := strings.TrimSpace(c.inline(inlineNodes))
		lines = append(lines, indent+marker+lead)

		childIndent := indent + strings.Repeat(" ", len(marker))
		for _, nl := range nestedLists {
			lines = append(lines, c.list(nl, childIndent, nl.Data == "ol"))
		}
	}
	return strings.Join(lines, "\n")
}

// table renders an HTML table as a GFM pipe table using the first row as the
// header.
func (c *conv) table(n *html.Node, indent string) string {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(nd *html.Node) {
		for _, ch := range children(nd) {
			if ch.Type == html.ElementNode && ch.Data == "tr" {
				var cells []string
				for _, cell := range children(ch) {
					if cell.Type == html.ElementNode && (cell.Data == "td" || cell.Data == "th") {
						txt := strings.TrimSpace(c.inline(allChildren(cell)))
						txt = strings.ReplaceAll(txt, "|", "\\|")
						txt = strings.ReplaceAll(txt, "\n", " ")
						cells = append(cells, txt)
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
				continue
			}
			walk(ch)
		}
	}
	walk(n)

	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	cols := len(rows[0])
	writeRow := func(cells []string) {
		b.WriteString(indent + "| ")
		for i := 0; i < cols; i++ {
			if i < len(cells) {
				b.WriteString(cells[i])
			}
			b.WriteString(" |")
			if i < cols-1 {
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	writeRow(rows[0])
	b.WriteString(indent + "|")
	for i := 0; i < cols; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, r := range rows[1:] {
		writeRow(r)
	}
	return strings.TrimRight(b.String(), "\n")
}

// codeBlock renders a fenced code block with optional language.
func (c *conv) codeBlock(code, lang, indent string) string {
	code = strings.TrimRight(code, "\n")
	var b strings.Builder
	b.WriteString(indent + "```" + lang + "\n")
	for _, line := range strings.Split(code, "\n") {
		b.WriteString(indent + line + "\n")
	}
	b.WriteString(indent + "```")
	return b.String()
}

// inline renders inline-level nodes (text, emphasis, links, code, etc.).
func (c *conv) inline(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(escapeText(collapseWS(n.Data)))
		case n.Type != html.ElementNode:
			// comments, doctype, etc. — skip
		default:
			b.WriteString(c.inlineElement(n))
		}
	}
	return b.String()
}

func (c *conv) inlineElement(n *html.Node) string {
	switch n.Data {
	case "strong", "b":
		return "**" + c.inline(allChildren(n)) + "**"
	case "em", "i":
		return "*" + c.inline(allChildren(n)) + "*"
	case "del", "s", "strike":
		return "~~" + c.inline(allChildren(n)) + "~~"
	case "code":
		return "`" + textContent(n) + "`"
	case "br":
		return "  \n"
	case "a":
		text := c.inline(allChildren(n))
		href := attr(n, "href")
		if href == "" {
			return text
		}
		return "[" + text + "](" + href + ")"
	case "ac:link":
		return c.confluenceLink(n)
	case "img":
		alt := attr(n, "alt")
		src := attr(n, "src")
		return "![" + alt + "](" + src + ")"
	default:
		// span, unknown inline, or stray block: render children inline.
		return c.inline(allChildren(n))
	}
}

// confluenceLink renders a Confluence <ac:link> as a Markdown link. The link
// target is a resource reference (ri:page, ri:attachment, …) that the storage
// format identifies by title/filename rather than a resolvable URL, so we
// preserve the original target verbatim as a "confluence://" descriptor rather
// than rewrite it. External links (ri:url) carry a real URL and render as
// ordinary Markdown links. Unknown references fall back to just their text so
// nothing visible is lost.
func (c *conv) confluenceLink(n *html.Node) string {
	text := c.linkText(n)
	dest, ok := c.linkDest(n)
	if !ok {
		return text
	}
	if text == "" {
		text = dest
	}
	return "[" + text + "](" + dest + ")"
}

// linkText extracts an ac:link's display text from its link-body, if any. The
// parser may nest the body inside the resource ref, so we search descendants.
func (c *conv) linkText(n *html.Node) string {
	for _, d := range descendants(n) {
		if d.Type != html.ElementNode {
			continue
		}
		switch d.Data {
		case "ac:link-body":
			return strings.TrimSpace(c.inline(allChildren(d)))
		case "ac:plain-text-link-body":
			return strings.TrimSpace(escapeText(textContent(d)))
		}
	}
	return ""
}

// linkDest derives an ac:link's destination from its resource reference. It
// returns ok=false when no recognized reference is present.
func (c *conv) linkDest(n *html.Node) (string, bool) {
	anchor := attr(n, "ac:anchor")
	for _, d := range descendants(n) {
		if d.Type != html.ElementNode || !strings.HasPrefix(d.Data, "ri:") {
			continue
		}
		switch d.Data {
		case "ri:url":
			if v := attr(d, "ri:value"); v != "" {
				return v, true // genuine external URL
			}
		case "ri:page", "ri:blog-post":
			space := attr(d, "ri:space-key")
			if space == "" {
				space = c.opts.SpaceKey
			}
			title := attr(d, "ri:content-title")
			// A resolvable title (with no in-page anchor to preserve) becomes a
			// clickable URL; otherwise fall back to the lossless descriptor.
			if anchor == "" && title != "" && c.opts.ResolvePageURL != nil {
				if u, ok := c.opts.ResolvePageURL(space, title); ok {
					return u, true
				}
			}
			return descriptor(strings.TrimPrefix(d.Data, "ri:"), [][2]string{
				{"space", space},
				{"title", title},
				{"id", attr(d, "ri:content-id")},
				{"anchor", anchor},
			}), true
		case "ri:attachment":
			return descriptor("attachment", [][2]string{
				{"filename", attr(d, "ri:filename")},
			}), true
		case "ri:space":
			return descriptor("space", [][2]string{{"space", attr(d, "ri:space-key")}}), true
		case "ri:user":
			return descriptor("user", [][2]string{{"account-id", attr(d, "ri:account-id")}}), true
		}
	}
	if anchor != "" { // same-page anchor: no resource ref
		return descriptor("page", [][2]string{{"space", c.opts.SpaceKey}, {"anchor", anchor}}), true
	}
	return "", false
}

// descriptor builds a "confluence://<scheme>?k=v&…" URI, skipping empty values
// and percent-encoding the rest (spaces as %20 for readability).
func descriptor(scheme string, params [][2]string) string {
	var b strings.Builder
	b.WriteString("confluence://")
	b.WriteString(scheme)
	sep := "?"
	for _, p := range params {
		if p[1] == "" {
			continue
		}
		b.WriteString(sep + p[0] + "=" + queryEscape(p[1]))
		sep = "&"
	}
	return b.String()
}

// queryEscape percent-encodes a descriptor value, using %20 for spaces rather
// than '+' so the result reads cleanly and decodes unambiguously.
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// --- helpers ---

func allChildren(n *html.Node) []*html.Node { return children(n) }

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key || a.Namespace+":"+a.Key == key {
			return a.Val
		}
	}
	return ""
}

// preLang extracts a fenced-code language from a <pre>'s child <code> element,
// where the markdown renderer puts it as class="language-xxx".
func preLang(n *html.Node) string {
	for _, ch := range descendants(n) {
		if ch.Type == html.ElementNode && ch.Data == "code" {
			for _, cls := range strings.Fields(attr(ch, "class")) {
				if lang, ok := strings.CutPrefix(cls, "language-"); ok {
					return lang
				}
			}
		}
	}
	return ""
}

// macroLang extracts the language parameter from a code macro.
func macroLang(n *html.Node) string {
	for _, ch := range descendants(n) {
		if ch.Type == html.ElementNode && ch.Data == "ac:parameter" && attr(ch, "ac:name") == "language" {
			return strings.TrimSpace(textContent(ch))
		}
	}
	return ""
}

// macroCode extracts the code body from a code macro. Confluence wraps it in
// <ac:plain-text-body><![CDATA[...]]></ac:plain-text-body>; the CDATA may be
// parsed as a comment, so we check both text and comment nodes.
func macroCode(n *html.Node) string {
	for _, ch := range descendants(n) {
		if ch.Type == html.ElementNode && ch.Data == "ac:plain-text-body" {
			var b strings.Builder
			for _, d := range descendants(ch) {
				switch d.Type {
				case html.TextNode, html.CommentNode:
					b.WriteString(d.Data)
				}
			}
			return strings.TrimPrefix(strings.TrimSuffix(b.String(), "]]"), "[CDATA[")
		}
	}
	return ""
}

// descendants returns all descendant nodes of n (depth-first, excluding n).
func descendants(n *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(nd *html.Node) {
		for ch := nd.FirstChild; ch != nil; ch = ch.NextSibling {
			out = append(out, ch)
			walk(ch)
		}
	}
	walk(n)
	return out
}

// textContent returns the concatenated text of all descendant text nodes.
func textContent(n *html.Node) string {
	var b strings.Builder
	for _, d := range descendants(n) {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
		}
	}
	return b.String()
}

// collapseWS collapses internal runs of whitespace to single spaces, matching
// HTML's inline whitespace handling, while preserving a single leading or
// trailing space so the gap between adjacent inline elements survives.
func collapseWS(s string) string {
	if s == "" {
		return ""
	}
	mid := strings.Join(strings.Fields(s), " ")
	if mid == "" {
		return " " // all whitespace collapses to a single space
	}
	if isSpace(rune(s[0])) {
		mid = " " + mid
	}
	if isSpace(rune(s[len(s)-1])) {
		mid = mid + " "
	}
	return mid
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

// escapeText escapes Markdown metacharacters in plain text so round-tripped
// content isn't reinterpreted as formatting.
func escapeText(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
	)
	return r.Replace(s)
}

// indentLines prefixes every line of s with indent.
func indentLines(s, indent string) string {
	if indent == "" {
		return s
	}
	return prefixLines(s, indent)
}

func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
