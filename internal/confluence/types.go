package confluence

// Representation is a Confluence body content format. Reads support storage,
// atlas_doc_format, and view; writes support storage, atlas_doc_format, and
// wiki. See the v2 API body-format docs.
type Representation string

const (
	Storage Representation = "storage"          // XHTML-based storage format
	ADF     Representation = "atlas_doc_format" // Atlassian Document Format (JSON)
	View    Representation = "view"             // rendered HTML (read-only)
	Wiki    Representation = "wiki"             // legacy wiki markup (write-only)
)

// Body is a single content representation: a value plus the format it is in.
type Body struct {
	Representation string `json:"representation,omitempty"`
	Value          string `json:"value,omitempty"`
}

// Bodies is the superset of body representations. On reads, Confluence
// populates the requested format(s); on writes, exactly one field should be
// set (the nested write form, e.g. {"storage": {...}}).
type Bodies struct {
	Storage *Body `json:"storage,omitempty"`
	ADF     *Body `json:"atlas_doc_format,omitempty"`
	View    *Body `json:"view,omitempty"`
	Wiki    *Body `json:"wiki,omitempty"`
}

// Get returns the body for the given representation, or nil if absent.
func (b *Bodies) Get(r Representation) *Body {
	if b == nil {
		return nil
	}
	switch r {
	case Storage:
		return b.Storage
	case ADF:
		return b.ADF
	case View:
		return b.View
	case Wiki:
		return b.Wiki
	}
	return nil
}

// set assigns value to the field for representation r.
func (b *Bodies) set(r Representation, value string) {
	body := &Body{Representation: string(r), Value: value}
	switch r {
	case Storage:
		b.Storage = body
	case ADF:
		b.ADF = body
	case View:
		b.View = body
	case Wiki:
		b.Wiki = body
	}
}

// Version captures a page's version, used for optimistic concurrency on update.
type Version struct {
	Number    int    `json:"number"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// Links holds the page's hypermedia links. webui is a site-relative path (e.g.
// "/spaces/ENG/pages/123/Title") that, prefixed with the site's "/wiki" base,
// forms the page's browser URL.
type Links struct {
	WebUI string `json:"webui,omitempty"`
}

// Page is a Confluence page as returned by reads and accepted (in part) by
// writes.
type Page struct {
	ID        string   `json:"id,omitempty"`
	Status    string   `json:"status,omitempty"`
	Title     string   `json:"title,omitempty"`
	SpaceID   string   `json:"spaceId,omitempty"`
	ParentID  string   `json:"parentId,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"` // page creation time (v2 top-level field)
	Version   *Version `json:"version,omitempty"`
	Body      *Bodies  `json:"body,omitempty"`
	Links     *Links   `json:"_links,omitempty"`
}

// Space is the subset of space fields coji needs: resolving a human-friendly
// key to the numeric spaceId, and rooting a content tree at the homepage.
type Space struct {
	ID         string `json:"id"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	HomepageID string `json:"homepageId"`
}

// ContentType identifies a node in the content tree.
type ContentType string

const (
	TypePage       ContentType = "page"
	TypeFolder     ContentType = "folder"
	TypeWhiteboard ContentType = "whiteboard"
	TypeDatabase   ContentType = "database"
	TypeEmbed      ContentType = "embed"
)

// Child is one entry in a content tree (a page, folder, whiteboard, etc.), as
// returned by the direct-children endpoints.
type Child struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Type          ContentType `json:"type"`
	SpaceID       string      `json:"spaceId"`
	ChildPosition *int        `json:"childPosition"`
}
