// Package core is coji's use-case layer: high-level page operations expressed
// in terms of plain inputs and outputs, with no terminal or presentation
// concerns. Both the CLI and any future TUI consume this package, so behavior
// stays identical across front-ends.
package core

import (
	"context"
	"fmt"

	"github.com/mgilbir/coji/internal/confluence"
	"github.com/mgilbir/coji/internal/markdown"
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

// Service exposes page operations over a Confluence client.
type Service struct {
	client *confluence.Client
}

// New returns a Service backed by the given client.
func New(client *confluence.Client) *Service {
	return &Service{client: client}
}

// Page is the front-end-facing view of a page: metadata plus the body rendered
// in the requested Format.
type Page struct {
	ID      string
	Title   string
	SpaceID string
	Version int
	Format  Format
	Body    string
}

// GetPage fetches a page and returns its body in the requested format. For
// FormatMarkdown the storage body is converted to markdown; otherwise the raw
// representation is returned.
func (s *Service) GetPage(ctx context.Context, id string, format Format) (*Page, error) {
	p, err := s.client.GetPage(ctx, id, format.rep())
	if err != nil {
		return nil, err
	}
	body, err := bodyOut(p.Body.Get(format.rep()), format)
	if err != nil {
		return nil, err
	}
	out := &Page{ID: p.ID, Title: p.Title, SpaceID: p.SpaceID, Format: format, Body: body}
	if p.Version != nil {
		out.Version = p.Version.Number
	}
	return out, nil
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
}

// CreatePage creates a page from the given content.
func (s *Service) CreatePage(ctx context.Context, in CreateInput) (*Page, error) {
	spaceID := in.SpaceID
	if spaceID == "" {
		if in.SpaceKey == "" {
			return nil, fmt.Errorf("a space id or key is required")
		}
		sp, err := s.client.SpaceByKey(ctx, in.SpaceKey)
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

// bodyOut converts an API body to boundary content for a format.
func bodyOut(b *confluence.Body, format Format) (string, error) {
	if b == nil {
		return "", nil
	}
	if format == FormatMarkdown {
		return markdown.FromStorage(b.Value)
	}
	return b.Value, nil
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
