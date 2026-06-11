package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/mgilbir/coji/internal/config"
	"golang.org/x/oauth2"
)

// ErrNotLoggedIn is returned when an operation needs credentials but none are
// configured. Callers should prompt the user to run `coji auth login`.
var ErrNotLoggedIn = fmt.Errorf("not authenticated: run `coji auth login` or `coji auth token`")

// Session is everything the API client needs to make authenticated calls: an
// HTTP client that injects credentials (refreshing OAuth tokens as needed) and
// the correct v2 API base URL for the chosen auth method.
type Session struct {
	HTTPClient *http.Client
	BaseURL    string // full v2 base, e.g. .../api/v2
	SiteURL    string // human-facing site, for display
}

// NewSession builds a Session from the persisted config, selecting the auth
// method. The OAuth and token methods target different API hosts.
func NewSession(ctx context.Context, cfg *config.Config) (*Session, error) {
	switch cfg.AuthMethod {
	case config.MethodToken:
		return tokenSession(cfg)
	case config.MethodOAuth:
		return oauthSession(ctx, cfg)
	case "":
		return nil, ErrNotLoggedIn
	default:
		return nil, fmt.Errorf("unknown auth method %q in config", cfg.AuthMethod)
	}
}

// oauthSession builds an OAuth-backed session. The token source auto-refreshes
// and persists rotated tokens. OAuth calls route through the api.atlassian.com
// gateway, keyed by cloudId.
func oauthSession(ctx context.Context, cfg *config.Config) (*Session, error) {
	tok, err := loadOAuthToken()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, ErrNotLoggedIn
	}
	if cfg.CloudID == "" {
		return nil, fmt.Errorf("no cloud id in config; re-run `coji auth login`")
	}

	src := &persistingTokenSource{src: oauthConfig(cfg).TokenSource(ctx, tok), last: tok}
	client := oauth2.NewClient(ctx, src)

	return &Session{
		HTTPClient: client,
		BaseURL:    fmt.Sprintf("https://api.atlassian.com/ex/confluence/%s/api/v2", cfg.CloudID),
		SiteURL:    cfg.SiteURL,
	}, nil
}

// tokenSession builds a basic-auth session. Token auth talks directly to the
// site's /wiki/api/v2 endpoint.
func tokenSession(cfg *config.Config) (*Session, error) {
	if cfg.Email == "" || cfg.APIToken == "" || cfg.SiteURL == "" {
		return nil, fmt.Errorf("%w (token auth needs email, api token, and site)", ErrNotLoggedIn)
	}
	client := &http.Client{Transport: &basicAuthTransport{
		email: cfg.Email,
		token: cfg.APIToken,
		base:  http.DefaultTransport,
	}}
	return &Session{
		HTTPClient: client,
		BaseURL:    strings.TrimRight(cfg.SiteURL, "/") + "/wiki/api/v2",
		SiteURL:    cfg.SiteURL,
	}, nil
}

// persistingTokenSource wraps an oauth2.TokenSource, saving the token whenever
// it changes (Atlassian rotates refresh tokens on every refresh).
type persistingTokenSource struct {
	src  oauth2.TokenSource
	last *oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	t, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	if p.last == nil || t.AccessToken != p.last.AccessToken || t.RefreshToken != p.last.RefreshToken {
		if err := saveOAuthToken(t); err != nil {
			return nil, fmt.Errorf("persisting refreshed token: %w", err)
		}
		p.last = t
	}
	return t, nil
}

// basicAuthTransport injects HTTP basic auth (email + API token) on each
// request, per the RoundTripper contract (clone, don't mutate the input).
type basicAuthTransport struct {
	email string
	token string
	base  http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.SetBasicAuth(t.email, t.token)
	return t.base.RoundTrip(r)
}

// Logout removes any cached OAuth token. (Token-auth credentials live in the
// config and are cleared by `coji auth logout` at the command layer.)
func Logout() error {
	return deleteOAuthToken()
}

// CurrentOAuthToken returns the cached OAuth token without refreshing, for
// status display. Returns nil when not present.
func CurrentOAuthToken() (*oauth2.Token, error) {
	return loadOAuthToken()
}
