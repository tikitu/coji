package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mgilbir/coji/internal/config"
	"golang.org/x/oauth2"
)

// Atlassian OAuth 2.0 (3LO) endpoints.
const (
	authorizeURL = "https://auth.atlassian.com/authorize"
	tokenURL     = "https://auth.atlassian.com/oauth/token"
	// audience scopes the access token to the Atlassian API gateway. Atlassian
	// requires it as an extra parameter on the authorize request.
	audience = "api.atlassian.com"
	// accessibleResourcesURL lists the sites a token can reach, which is how
	// we resolve the cloudId used in Confluence API paths.
	accessibleResourcesURL = "https://api.atlassian.com/oauth/token/accessible-resources"
)

// DefaultScopes are the OAuth scopes coji requests. offline_access is required
// to receive a refresh token; the rest map to page/space read+write.
var DefaultScopes = []string{
	"offline_access",
	"read:page:confluence",
	"write:page:confluence",
	"read:space:confluence",
}

// oauthConfig builds the x/oauth2 config from coji's config. x/oauth2 owns the
// token exchange, refresh, and rotation; we only supply endpoints and creds.
func oauthConfig(cfg *config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.Redirect(),
		Scopes:       DefaultScopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authorizeURL,
			TokenURL: tokenURL,
		},
	}
}

// Site is one Confluence site reachable by a token, from accessible-resources.
type Site struct {
	ID     string   `json:"id"` // the cloudId
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Scopes []string `json:"scopes"`
}

// Login runs the authorization-code flow: it spins up a loopback server, opens
// the browser to the consent screen, exchanges the returned code for a token
// (via x/oauth2), and persists it. Returns the token on success.
func Login(ctx context.Context, cfg *config.Config) (*oauth2.Token, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret must be configured first (see `coji auth login --help`)")
	}

	redirect, err := url.Parse(cfg.Redirect())
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URL %q: %w", cfg.Redirect(), err)
	}

	state, err := randomToken()
	if err != nil {
		return nil, err
	}

	// Listen on the exact host:port of the registered redirect before opening
	// the browser, so we never miss the callback.
	ln, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return nil, fmt.Errorf("listening on %s for OAuth callback (is the port free?): %w", redirect.Host, err)
	}
	defer ln.Close()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{Handler: callbackHandler(redirect.Path, state, codeCh, errCh)}
	go srv.Serve(ln)
	defer srv.Close()

	oc := oauthConfig(cfg)
	authURL := oc.AuthCodeURL(state,
		oauth2.SetAuthURLParam("audience", audience),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	fmt.Println("Opening your browser to authorize coji.")
	fmt.Println("If it doesn't open automatically, visit:")
	fmt.Println()
	fmt.Println("  " + authURL)
	fmt.Println()
	_ = openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("timed out waiting for authorization")
	}

	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging authorization code: %w", err)
	}
	if err := saveOAuthToken(tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// callbackHandler returns the HTTP handler that captures the auth code from the
// loopback redirect, validating the path and state.
func callbackHandler(path, state string, codeCh chan<- string, errCh chan<- error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			fail(w, errCh, fmt.Errorf("authorization denied: %s %s", e, q.Get("error_description")))
			return
		}
		if q.Get("state") != state {
			fail(w, errCh, fmt.Errorf("state mismatch; possible CSRF, aborting"))
			return
		}
		code := q.Get("code")
		if code == "" {
			fail(w, errCh, fmt.Errorf("no authorization code in callback"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, successHTML)
		codeCh <- code
	})
	return mux
}

func fail(w http.ResponseWriter, errCh chan<- error, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
	errCh <- err
}

const successHTML = `<!doctype html><html><head><meta charset="utf-8"><title>coji</title></head>
<body style="font-family:system-ui;text-align:center;margin-top:4rem">
<h1>Authorized ✅</h1><p>You can close this tab and return to the terminal.</p></body></html>`

// AccessibleResources lists the Confluence sites the given access token can
// reach. Used to resolve (and let the user pick) the cloudId.
func AccessibleResources(ctx context.Context, accessToken string) ([]Site, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, accessibleResourcesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("accessible-resources request: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accessible-resources returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var sites []Site
	if err := json.Unmarshal(data, &sites); err != nil {
		return nil, fmt.Errorf("decoding accessible-resources: %w", err)
	}
	return sites, nil
}

// randomToken returns a URL-safe 256-bit random string for CSRF state.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// openBrowser best-effort opens a URL in the user's default browser.
func openBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}
