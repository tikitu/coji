package markdown

import (
	"strings"
	"testing"
)

func TestToStorage(t *testing.T) {
	tests := []struct {
		name     string
		md       string
		contains []string
	}{
		{"heading", "# Title", []string{"<h1>Title</h1>"}},
		{"paragraph", "hello world", []string{"<p>hello world</p>"}},
		{"bold-italic", "**b** and *i*", []string{"<strong>b</strong>", "<em>i</em>"}},
		{"link", "[text](https://x.com)", []string{`<a href="https://x.com">text</a>`}},
		{"inline code", "use `go test`", []string{"<code>go test</code>"}},
		{"unordered list", "- a\n- b", []string{"<ul>", "<li>a</li>", "<li>b</li>"}},
		{"code fence", "```go\nx := 1\n```", []string{"<pre><code", "x := 1"}},
		{"table", "| a | b |\n| --- | --- |\n| 1 | 2 |", []string{"<table>", "<th>a</th>", "<td>1</td>"}},
		{"raw html passthrough", `<ac:structured-macro ac:name="info"/>`, []string{"ac:structured-macro"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToStorage([]byte(tt.md))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("ToStorage(%q) = %q\n  missing %q", tt.md, got, want)
				}
			}
		})
	}
}

func TestFromStorage(t *testing.T) {
	tests := []struct {
		name    string
		storage string
		want    string
	}{
		{"heading", "<h2>Sub</h2>", "## Sub"},
		{"paragraph", "<p>hello world</p>", "hello world"},
		{"bold and italic spacing", "<p>a <strong>b</strong> <em>c</em></p>", "a **b** *c*"},
		{"link", `<p><a href="https://x.com">text</a></p>`, "[text](https://x.com)"},
		{"inline code", "<p>use <code>go test</code></p>", "use `go test`"},
		{"hr", "<hr/>", "---"},
		{"blockquote", "<blockquote><p>quoted</p></blockquote>", "> quoted"},
		{
			"unordered list",
			"<ul><li>a</li><li>b</li></ul>",
			"- a\n- b",
		},
		{
			"ordered list",
			"<ol><li>first</li><li>second</li></ol>",
			"1. first\n2. second",
		},
		{
			"nested list",
			"<ul><li>a<ul><li>a1</li></ul></li><li>b</li></ul>",
			"- a\n  - a1\n- b",
		},
		{
			"table",
			"<table><tbody><tr><th>a</th><th>b</th></tr><tr><td>1</td><td>2</td></tr></tbody></table>",
			"| a | b |\n| --- | --- |\n| 1 | 2 |",
		},
		{
			"pre code block",
			"<pre><code>x := 1\ny := 2</code></pre>",
			"```\nx := 1\ny := 2\n```",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromStorage(tt.storage)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Errorf("FromStorage(%q):\n got: %q\nwant: %q", tt.storage, strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestFromStorageConfluenceLinks covers <ac:link> resource references, which
// carry no resolvable URL: the target is preserved as a confluence:// descriptor
// rather than rewritten. spaceKey qualifies same-space links (no ri:space-key).
func TestFromStorageConfluenceLinks(t *testing.T) {
	tests := []struct {
		name     string
		storage  string
		spaceKey string
		want     string
	}{
		{
			"page same space",
			`<p><ac:link><ri:page ri:content-title="Design Notes" /><ac:link-body>Design Notes</ac:link-body></ac:link></p>`,
			"ENG",
			"[Design Notes](confluence://page?space=ENG&title=Design%20Notes)",
		},
		{
			"page explicit space overrides default",
			`<p><ac:link><ri:page ri:space-key="OPS" ri:content-title="Runbook" /><ac:link-body>Runbook</ac:link-body></ac:link></p>`,
			"ENG",
			"[Runbook](confluence://page?space=OPS&title=Runbook)",
		},
		{
			"title with special chars",
			`<p><ac:link><ri:page ri:content-title="Q3. Review: Foo (Bar)" /><ac:link-body>Q3. Review: Foo (Bar)</ac:link-body></ac:link></p>`,
			"DOCS",
			"[Q3. Review: Foo (Bar)](confluence://page?space=DOCS&title=Q3.%20Review%3A%20Foo%20%28Bar%29)",
		},
		{
			"attachment",
			`<p><ac:link><ri:attachment ri:filename="report 2024.pdf" /><ac:link-body>the report</ac:link-body></ac:link></p>`,
			"DOCS",
			"[the report](confluence://attachment?filename=report%202024.pdf)",
		},
		{
			"no body falls back to descriptor",
			`<p><ac:link><ri:page ri:content-title="Orphan" /></ac:link></p>`,
			"ENG",
			"[confluence://page?space=ENG&title=Orphan](confluence://page?space=ENG&title=Orphan)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromStorage(tt.storage, Options{SpaceKey: tt.spaceKey})
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Errorf("FromStorage(%q):\n got: %q\nwant: %q", tt.storage, strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestRoundTrip checks that markdown survives md -> storage -> md for the
// common constructs, which is the primary real-world path (edit a fetched page).
func TestRoundTrip(t *testing.T) {
	cases := []string{
		"# Heading\n\nA paragraph with **bold** and *italic* text.",
		"- one\n- two\n- three",
		"1. first\n2. second",
		"> a quote",
		"Some `inline code` here.",
		"[a link](https://example.com)",
		"| h1 | h2 |\n| --- | --- |\n| a | b |",
		"```go\nfmt.Println(\"hi\")\n```",
	}
	for _, md := range cases {
		t.Run(md[:min(12, len(md))], func(t *testing.T) {
			storage, err := ToStorage([]byte(md))
			if err != nil {
				t.Fatal(err)
			}
			back, err := FromStorage(storage)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(back); got != md {
				t.Errorf("round trip mismatch:\n in:      %q\n storage: %q\n out:     %q", md, storage, got)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
