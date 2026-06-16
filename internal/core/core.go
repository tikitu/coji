// Package core is coji's use-case layer: high-level page operations expressed
// in terms of plain inputs and outputs, with no terminal or presentation
// concerns. Both the CLI and any future TUI consume this package, so behavior
// stays identical across front-ends.
package core

import (
	"context"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/mgilbir/coji/internal/confluence"
	"github.com/mgilbir/coji/internal/markdown"
	"github.com/mgilbir/coji/internal/policy"
)

// Format is how page body content is represented at coji's boundary (input or
// output), independent of the API's internal representations.
type Format string

const (
	// FormatMarkdown converts to/from Confluence storage format.
	FormatMarkdown Format = "markdown"
	// FormatStorage passes Confluence storage-format XHTML through verbatim.
	FormatStorage Format = "storage"
	// FormatADF passes Atlassian Document Format (JSON) through verbatim.
	FormatADF Format = "adf"
)

// ParseFormat validates and normalizes a format string.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatMarkdown, FormatStorage, FormatADF:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown format %q (want markdown, storage, or adf)", s)
	}
}

// rep maps a boundary Format to the API representation used to fetch/send body.
func (f Format) rep() confluence.Representation {
	switch f {
	case FormatStorage, FormatMarkdown:
		return confluence.Storage
	case FormatADF:
		return confluence.ADF
	default:
		return confluence.Storage
	}
}

// Service exposes page operations over a Confluence client, optionally gated by
// a policy.
type Service struct {
	client  *confluence.Client
	policy  *policy.Policy
	siteURL string // human-facing site base (e.g. https://acme.atlassian.net), for building page URLs

	mu       sync.Mutex
	keyCache map[string]string // spaceID -> space key, for policy checks
	idCache  map[string]string // space key -> spaceID, for link resolution
	urlCache map[string]string // "spaceKey\x00title" -> page URL ("" = looked up, not found)
}

// Option configures a Service.
type Option func(*Service)

// WithPolicy gates operations behind the given policy (nil means allow all).
func WithPolicy(p *policy.Policy) Option {
	return func(s *Service) { s.policy = p }
}

// WithSiteURL sets the human-facing site base used to build absolute page URLs
// in fetched-page metadata.
func WithSiteURL(url string) Option {
	return func(s *Service) { s.siteURL = url }
}

// New returns a Service backed by the given client.
func New(client *confluence.Client, opts ...Option) *Service {
	s := &Service{
		client:   client,
		keyCache: map[string]string{},
		idCache:  map[string]string{},
		urlCache: map[string]string{},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// spaceKey resolves a numeric space id to its key (cached), for policy lookups.
// On failure it returns "", letting the policy's default apply.
func (s *Service) spaceKey(ctx context.Context, spaceID string) string {
	if spaceID == "" {
		return ""
	}
	s.mu.Lock()
	if k, ok := s.keyCache[spaceID]; ok {
		s.mu.Unlock()
		return k
	}
	s.mu.Unlock()

	sp, err := s.client.SpaceByID(ctx, spaceID)
	if err != nil || sp == nil {
		return ""
	}
	s.mu.Lock()
	s.keyCache[spaceID] = sp.Key
	s.mu.Unlock()
	return sp.Key
}

// Page is the front-end-facing view of a page: metadata plus the body rendered
// in the requested Format.
type Page struct {
	ID       string
	Title    string
	SpaceID  string
	SpaceKey string // human-friendly key (e.g. ENG); empty if it couldn't be resolved
	Version  int
	Created  string // ISO-8601 timestamp the page was created
	Updated  string // ISO-8601 timestamp the current version was created (last edit)
	Format   Format
	WebURL   string // absolute browser URL, when the site base is known
	Body     string
}

// Frontmatter renders the page's provenance metadata as a YAML frontmatter
// block (including the delimiters and a trailing blank line), suitable for
// prepending to a Markdown body. Fields that are empty are omitted.
func (p *Page) Frontmatter() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + yamlString(p.Title) + "\n")
	if p.ID != "" {
		b.WriteString("id: " + yamlString(p.ID) + "\n")
	}
	if p.Version != 0 {
		fmt.Fprintf(&b, "version: %d\n", p.Version)
	}
	if p.Created != "" {
		b.WriteString("created: " + yamlString(p.Created) + "\n")
	}
	if p.Updated != "" {
		b.WriteString("updated: " + yamlString(p.Updated) + "\n")
	}
	if p.SpaceKey != "" {
		b.WriteString("space: " + yamlString(p.SpaceKey) + "\n")
	}
	if p.SpaceID != "" {
		b.WriteString("spaceId: " + yamlString(p.SpaceID) + "\n")
	}
	if p.WebURL != "" {
		b.WriteString("source: " + yamlString(p.WebURL) + "\n")
	}
	b.WriteString("---\n\n")
	return b.String()
}

// yamlString renders s as a double-quoted YAML scalar, escaping backslashes and
// double quotes. Double-quoting keeps values safe regardless of special
// characters (colons, leading symbols) common in titles and URLs.
func yamlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// GetPage fetches a page and returns its body in the requested format. For
// FormatMarkdown the storage body is converted to markdown; otherwise the raw
// representation is returned.
func (s *Service) GetPage(ctx context.Context, id string, format Format) (*Page, error) {
	p, err := s.client.GetPage(ctx, id, format.rep())
	if err != nil {
		return nil, err
	}
	spaceKey := s.spaceKey(ctx, p.SpaceID)
	if err := s.policy.Check(spaceKey, policy.OpRead); err != nil {
		return nil, err
	}
	// Seed the key→id cache with this page's own space so same-space links
	// resolve without an extra lookup.
	if spaceKey != "" && p.SpaceID != "" {
		s.mu.Lock()
		s.idCache[spaceKey] = p.SpaceID
		s.mu.Unlock()
	}
	opts := markdown.Options{
		SpaceKey: spaceKey,
		ResolvePageURL: func(linkSpaceKey, title string) (string, bool) {
			return s.resolvePageURL(ctx, linkSpaceKey, title)
		},
	}
	body, err := bodyOut(p.Body.Get(format.rep()), format, opts)
	if err != nil {
		return nil, err
	}
	out := &Page{
		ID:       p.ID,
		Title:    p.Title,
		SpaceID:  p.SpaceID,
		SpaceKey: spaceKey,
		Created:  p.CreatedAt,
		Format:   format,
		WebURL:   s.webURL(p.Links),
		Body:     body,
	}
	if p.Version != nil {
		out.Version = p.Version.Number
		out.Updated = p.Version.CreatedAt
	}
	return out, nil
}

// webURL builds an absolute browser URL from a page's site-relative webui link
// and the configured site base. Returns "" when either is unavailable.
func (s *Service) webURL(links *confluence.Links) string {
	if links == nil || links.WebUI == "" || s.siteURL == "" {
		return ""
	}
	return strings.TrimRight(s.siteURL, "/") + "/wiki" + links.WebUI
}

// SearchOptions tunes a search. SpaceKey restricts to one space; Limit caps
// results (0 → a sensible default); RawCQL passes the query through as a literal
// CQL expression instead of wrapping it in a text match.
type SearchOptions struct {
	SpaceKey string
	Limit    int
	RawCQL   bool
}

// SearchHit is the front-end-facing view of a search result: enough to triage a
// hit (title, excerpt, space, last edit) and to act on it (ID for dedup against
// a local archive and for fetching; WebURL for the user to open).
type SearchHit struct {
	ID       string
	Title    string
	SpaceKey string
	Type     string
	Excerpt  string
	Updated  string
	WebURL   string
}

// Search runs a CQL search and returns hits the policy permits reading. When
// opts.RawCQL is false, query is wrapped as `type=page AND text ~ "query"`
// (optionally scoped to opts.SpaceKey); when true, query is used verbatim.
func (s *Service) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchHit, error) {
	cql := query
	if !opts.RawCQL {
		cql = buildCQL(query, opts.SpaceKey)
	}
	results, err := s.client.Search(ctx, cql, opts.Limit)
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(results))
	for _, r := range results {
		spaceKey := r.SpaceKey
		id := r.ID
		// The result URL ("/spaces/KEY/pages/ID/Slug") carries the space key and
		// id; use it to backfill either if the API omitted them.
		if spaceKey == "" || id == "" {
			urlKey, urlID := parseSpaceAndID(r.URL)
			if spaceKey == "" {
				spaceKey = urlKey
			}
			if id == "" {
				id = urlID
			}
		}
		// Honor the read policy, but drop disallowed hits rather than failing the
		// whole search (a broad query legitimately spans many spaces).
		if !s.policy.Allow(spaceKey, policy.OpRead) {
			continue
		}
		hits = append(hits, SearchHit{
			ID:       id,
			Title:    cleanExcerpt(r.Title),
			SpaceKey: spaceKey,
			Type:     r.Type,
			Excerpt:  cleanExcerpt(r.Excerpt),
			Updated:  r.Updated,
			WebURL:   s.searchWebURL(r.URL),
		})
	}
	return hits, nil
}

// buildCQL composes a default page-text CQL query, optionally scoped to a space.
func buildCQL(query, spaceKey string) string {
	cql := fmt.Sprintf(`type=page AND text ~ "%s"`, cqlEscape(query))
	if spaceKey != "" {
		cql += fmt.Sprintf(` AND space="%s"`, cqlEscape(spaceKey))
	}
	return cql
}

// cqlEscape escapes the characters that are special inside a CQL double-quoted
// string value (backslash and double quote).
func cqlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// cleanExcerpt strips Confluence's @@@hl@@@…@@@endhl@@@ search-highlight markers,
// decodes HTML entities (excerpts and the top-level title arrive HTML-escaped,
// e.g. &amp; and &#39;), and collapses internal whitespace (excerpts carry
// embedded newlines).
func cleanExcerpt(s string) string {
	s = strings.ReplaceAll(s, "@@@hl@@@", "")
	s = strings.ReplaceAll(s, "@@@endhl@@@", "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// searchWebURL turns a search result's site-relative URL into an absolute
// browser URL using the configured site base. Returns "" when unavailable.
func (s *Service) searchWebURL(relURL string) string {
	if relURL == "" || s.siteURL == "" {
		return ""
	}
	return strings.TrimRight(s.siteURL, "/") + "/wiki" + relURL
}

// parseSpaceAndID extracts the space key and page id from a Confluence content
// URL of the form "/spaces/<KEY>/pages/<ID>/<slug>". Either may come back empty.
func parseSpaceAndID(relURL string) (spaceKey, id string) {
	parts := strings.Split(relURL, "/")
	for i, p := range parts {
		switch p {
		case "spaces":
			if i+1 < len(parts) {
				spaceKey = parts[i+1]
			}
		case "pages":
			if i+1 < len(parts) {
				id = parts[i+1]
			}
		}
	}
	return spaceKey, id
}

// CreateInput describes a page to create. Exactly one of SpaceID/SpaceKey is
// required; SpaceKey is resolved to an ID.
type CreateInput struct {
	SpaceID  string
	SpaceKey string
	Title    string
	ParentID string
	Format   Format
	Content  string // body in the given Format (markdown source, or raw storage/adf)
	Private  bool   // create as private (only the creator can view/edit)
}

// CreatePage creates a page from the given content.
func (s *Service) CreatePage(ctx context.Context, in CreateInput) (*Page, error) {
	spaceID := in.SpaceID
	spaceKey := in.SpaceKey
	if spaceID == "" && spaceKey == "" {
		return nil, fmt.Errorf("a space id or key is required")
	}
	// Determine the key for the policy check before any mutating call, and
	// before resolving the id from the key (so a blocked create costs nothing).
	if spaceKey == "" {
		spaceKey = s.spaceKey(ctx, spaceID)
	}
	if err := s.policy.Check(spaceKey, policy.OpCreate); err != nil {
		return nil, err
	}
	if spaceID == "" {
		sp, err := s.client.SpaceByKey(ctx, spaceKey)
		if err != nil {
			return nil, err
		}
		spaceID = sp.ID
	}

	value, err := bodyIn(in.Content, in.Format)
	if err != nil {
		return nil, err
	}

	p, err := s.client.CreatePage(ctx, confluence.CreatePageInput{
		SpaceID:  spaceID,
		Title:    in.Title,
		ParentID: in.ParentID,
		Rep:      in.Format.rep(),
		Value:    value,
		Private:  in.Private,
	})
	if err != nil {
		return nil, err
	}
	return pageMeta(p, in.Format), nil
}

// EditInput describes an edit. Title, when empty, is preserved from the current
// page (the API requires a title on every update).
type EditInput struct {
	ID         string
	Title      string
	Format     Format
	Content    string
	VersionMsg string
}

// EditPage performs a read-modify-write: it fetches the current version (and
// title, if not overridden), then writes the new body with the next version
// number, satisfying the API's optimistic-concurrency requirement.
func (s *Service) EditPage(ctx context.Context, in EditInput) (*Page, error) {
	current, err := s.client.GetPage(ctx, in.ID, "") // metadata only
	if err != nil {
		return nil, err
	}
	if err := s.policy.Check(s.spaceKey(ctx, current.SpaceID), policy.OpEdit); err != nil {
		return nil, err
	}

	title := in.Title
	if title == "" {
		title = current.Title
	}

	value, err := bodyIn(in.Content, in.Format)
	if err != nil {
		return nil, err
	}

	p, err := s.client.UpdatePage(ctx, confluence.UpdatePageInput{
		ID:            in.ID,
		Title:         title,
		Rep:           in.Format.rep(),
		Value:         value,
		VersionNumber: confluence.NextVersion(current),
		VersionMsg:    in.VersionMsg,
	})
	if err != nil {
		return nil, err
	}
	return pageMeta(p, in.Format), nil
}

// bodyIn converts boundary content to the API body value for a representation.
func bodyIn(content string, format Format) (string, error) {
	if format == FormatMarkdown {
		return markdown.ToStorage([]byte(content))
	}
	return content, nil
}

// bodyOut converts an API body to boundary content for a format. opts qualifies
// internal links (only used for markdown).
func bodyOut(b *confluence.Body, format Format, opts markdown.Options) (string, error) {
	if b == nil {
		return "", nil
	}
	if format == FormatMarkdown {
		return markdown.FromStorage(b.Value, opts)
	}
	return b.Value, nil
}

// resolvePageURL maps an internal page link (space key + title) to its absolute
// browser URL, returning ok=false when it can't be resolved (unknown space,
// missing/renamed page, or no site base configured). Results — including
// misses — are cached for the Service's lifetime.
func (s *Service) resolvePageURL(ctx context.Context, spaceKey, title string) (string, bool) {
	if s.siteURL == "" {
		return "", false
	}
	cacheKey := spaceKey + "\x00" + title
	s.mu.Lock()
	if u, ok := s.urlCache[cacheKey]; ok {
		s.mu.Unlock()
		return u, u != ""
	}
	s.mu.Unlock()

	url, _ := s.lookupPageURL(ctx, spaceKey, title)
	s.mu.Lock()
	s.urlCache[cacheKey] = url // cache misses ("") too, to avoid re-querying
	s.mu.Unlock()
	return url, url != ""
}

// lookupPageURL performs the uncached space-id resolution and page lookup.
func (s *Service) lookupPageURL(ctx context.Context, spaceKey, title string) (string, error) {
	spaceID := s.spaceID(ctx, spaceKey)
	if spaceID == "" {
		return "", nil
	}
	p, err := s.client.FindPageByTitle(ctx, spaceID, title)
	if err != nil || p == nil {
		return "", err
	}
	// The title filter isn't guaranteed exact; confirm before trusting it.
	if !strings.EqualFold(strings.TrimSpace(p.Title), strings.TrimSpace(title)) {
		return "", nil
	}
	return s.webURL(p.Links), nil
}

// spaceID resolves a space key to its numeric id (cached). Returns "" on
// failure, leaving the link unresolved.
func (s *Service) spaceID(ctx context.Context, key string) string {
	if key == "" {
		return ""
	}
	s.mu.Lock()
	if id, ok := s.idCache[key]; ok {
		s.mu.Unlock()
		return id
	}
	s.mu.Unlock()

	sp, err := s.client.SpaceByKey(ctx, key)
	if err != nil || sp == nil {
		return ""
	}
	s.mu.Lock()
	s.idCache[key] = sp.ID
	s.mu.Unlock()
	return sp.ID
}

// pageMeta builds a Page view carrying only metadata (no body), used for
// create/edit results.
func pageMeta(p *confluence.Page, format Format) *Page {
	out := &Page{ID: p.ID, Title: p.Title, SpaceID: p.SpaceID, Format: format}
	if p.Version != nil {
		out.Version = p.Version.Number
	}
	return out
}
