// Package confluence is a small typed client for the Confluence Cloud REST API
// v2, scoped to the page operations coji needs (read, create, update) plus
// space-key resolution.
package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client talks to the v2 API for a single Confluence site (cloudId). The
// supplied http.Client is expected to attach OAuth bearer auth (see
// internal/auth).
type Client struct {
	http    *http.Client
	baseURL string
}

// New builds a client targeting the given v2 API base URL. The base differs by
// auth method: OAuth routes through the api.atlassian.com gateway
// (https://api.atlassian.com/ex/confluence/{cloudId}/api/v2) while API-token
// auth talks to the site directly (https://{site}/wiki/api/v2). The caller
// (internal/auth.Session) supplies both the base URL and an http.Client that
// injects credentials.
func New(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// APIError is a non-2xx response from the Confluence API.
type APIError struct {
	StatusCode int
	Status     string
	Detail     string // best-effort human-readable detail
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("confluence API %s: %s", e.Status, e.Detail)
	}
	return fmt.Sprintf("confluence API %s", e.Status)
}

// errorBody matches the v2 error envelope: {"errors":[{"title","detail",...}]}.
type errorBody struct {
	Errors []struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Code   string `json:"code"`
	} `json:"errors"`
}

// do performs an API request, JSON-encoding body (if non-nil) and decoding a
// 2xx JSON response into out (if non-nil). Non-2xx responses become *APIError.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

func parseAPIError(resp *http.Response, data []byte) error {
	e := &APIError{StatusCode: resp.StatusCode, Status: resp.Status}
	var eb errorBody
	if json.Unmarshal(data, &eb) == nil && len(eb.Errors) > 0 {
		parts := make([]string, 0, len(eb.Errors))
		for _, x := range eb.Errors {
			msg := x.Title
			if x.Detail != "" {
				msg = strings.TrimSpace(msg + " " + x.Detail)
			}
			if msg != "" {
				parts = append(parts, msg)
			}
		}
		e.Detail = strings.Join(parts, "; ")
	}
	if e.Detail == "" {
		e.Detail = strings.TrimSpace(string(data))
	}
	return e
}

// GetPage fetches a page by ID, requesting the body in the given format. Pass
// an empty format to omit the body (metadata only).
func (c *Client) GetPage(ctx context.Context, id string, format Representation) (*Page, error) {
	q := url.Values{}
	if format != "" {
		q.Set("body-format", string(format))
	}
	var p Page
	if err := c.do(ctx, http.MethodGet, "/pages/"+url.PathEscape(id), q, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// FindPageByTitle returns the page with the given title in the given space, or
// nil when none matches. Page titles are unique within a space, so at most one
// page matches. The returned page carries _links (for building its web URL).
func (c *Client) FindPageByTitle(ctx context.Context, spaceID, title string) (*Page, error) {
	q := url.Values{}
	q.Set("space-id", spaceID)
	q.Set("title", title)
	q.Set("limit", "1")

	var out struct {
		Results []Page `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, "/pages", q, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, nil
	}
	return &out.Results[0], nil
}

// CreatePageInput holds the fields for creating a page. Body holds exactly one
// representation (set via Bodies).
type CreatePageInput struct {
	SpaceID  string
	Title    string
	ParentID string // optional
	Status   string // "current" (default) or "draft"
	Rep      Representation
	Value    string
	Private  bool // only the creator can view/edit (private query param)
}

// CreatePage creates a page and returns the created page.
func (c *Client) CreatePage(ctx context.Context, in CreatePageInput) (*Page, error) {
	status := in.Status
	if status == "" {
		status = "current"
	}
	body := &Bodies{}
	body.set(in.Rep, in.Value)

	req := map[string]any{
		"spaceId": in.SpaceID,
		"status":  status,
		"title":   in.Title,
		"body":    body,
	}
	if in.ParentID != "" {
		req["parentId"] = in.ParentID
	}

	var q url.Values
	if in.Private {
		q = url.Values{"private": {"true"}}
	}

	var p Page
	if err := c.do(ctx, http.MethodPost, "/pages", q, req, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePageInput holds the fields for updating a page. The caller supplies the
// new version number (current + 1); UpdatePageContent computes it for you.
type UpdatePageInput struct {
	ID            string
	Title         string
	Status        string // typically "current"
	Rep           Representation
	Value         string
	VersionNumber int
	VersionMsg    string // optional changelog message
}

// UpdatePage performs the raw PUT /pages/{id} with the given version number.
func (c *Client) UpdatePage(ctx context.Context, in UpdatePageInput) (*Page, error) {
	status := in.Status
	if status == "" {
		status = "current"
	}
	body := &Bodies{}
	body.set(in.Rep, in.Value)

	req := map[string]any{
		"id":     in.ID,
		"status": status,
		"title":  in.Title,
		"body":   body,
		"version": map[string]any{
			"number":  in.VersionNumber,
			"message": in.VersionMsg,
		},
	}
	var p Page
	if err := c.do(ctx, http.MethodPut, "/pages/"+url.PathEscape(in.ID), nil, req, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// listAll fetches every page of a cursor-paginated v2 collection at path,
// invoking accum with each page's raw "results" array. It follows the
// _links.next cursor until exhausted.
func (c *Client) listAll(ctx context.Context, path string, baseQuery url.Values, accum func(json.RawMessage) error) error {
	cursor := ""
	for {
		q := url.Values{}
		for k, v := range baseQuery {
			q[k] = v
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp struct {
			Results json.RawMessage `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := c.do(ctx, http.MethodGet, path, q, nil, &resp); err != nil {
			return err
		}
		if err := accum(resp.Results); err != nil {
			return err
		}
		cursor = cursorFromNext(resp.Links.Next)
		if cursor == "" {
			return nil
		}
	}
}

// cursorFromNext extracts the cursor query parameter from a _links.next URL
// (which is site-relative, so we only trust its query string).
func cursorFromNext(next string) string {
	if next == "" {
		return ""
	}
	u, err := url.Parse(next)
	if err != nil {
		return ""
	}
	return u.Query().Get("cursor")
}

// ListSpaces lists spaces, optionally filtered to a single key.
func (c *Client) ListSpaces(ctx context.Context, key string) ([]Space, error) {
	q := url.Values{}
	q.Set("limit", "250")
	if key != "" {
		q.Set("keys", key)
	}
	var all []Space
	err := c.listAll(ctx, "/spaces", q, func(raw json.RawMessage) error {
		var page []Space
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// DirectChildren lists the direct children of a page or folder in the content
// tree. parentType selects the endpoint ("folder" uses /folders, anything else
// uses /pages); the returned children carry their own type for further descent.
func (c *Client) DirectChildren(ctx context.Context, parentType ContentType, id string) ([]Child, error) {
	collection := "pages"
	if parentType == TypeFolder {
		collection = "folders"
	}
	path := "/" + collection + "/" + url.PathEscape(id) + "/direct-children"

	q := url.Values{}
	q.Set("limit", "250")
	var all []Child
	err := c.listAll(ctx, path, q, func(raw json.RawMessage) error {
		var page []Child
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// Ping makes a cheap authenticated request (listing one space) to verify that
// credentials work. It returns nil on success and an *APIError (e.g. 401) when
// auth fails.
func (c *Client) Ping(ctx context.Context) error {
	q := url.Values{}
	q.Set("limit", "1")
	var out struct {
		Results []Space `json:"results"`
	}
	return c.do(ctx, http.MethodGet, "/spaces", q, nil, &out)
}

// SpaceByKey resolves a space key (e.g. "ENG") to its space, primarily to get
// the numeric spaceId required by CreatePage.
func (c *Client) SpaceByKey(ctx context.Context, key string) (*Space, error) {
	q := url.Values{}
	q.Set("keys", key)
	q.Set("limit", "1")

	var out struct {
		Results []Space `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, "/spaces", q, nil, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, fmt.Errorf("no space found with key %q", key)
	}
	return &out.Results[0], nil
}

// SpaceByID fetches a space by its numeric id, primarily to recover its key
// for policy checks when only the id is known.
func (c *Client) SpaceByID(ctx context.Context, id string) (*Space, error) {
	var sp Space
	if err := c.do(ctx, http.MethodGet, "/spaces/"+url.PathEscape(id), nil, nil, &sp); err != nil {
		return nil, err
	}
	return &sp, nil
}

// NextVersion returns the version number an update must declare (current + 1).
func NextVersion(p *Page) int {
	if p == nil || p.Version == nil {
		return 1
	}
	return p.Version.Number + 1
}
