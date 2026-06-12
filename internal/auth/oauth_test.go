package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mgilbir/coji/internal/config"
	"golang.org/x/oauth2"
)

func TestAuthCodeURL(t *testing.T) {
	cfg := &config.Config{ClientID: "abc123", RedirectURL: "http://localhost:8723/callback"}
	oc := oauthConfig(cfg)
	raw := oc.AuthCodeURL("state-xyz",
		oauth2.SetAuthURLParam("audience", audience),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	checks := map[string]string{
		"audience":      audience,
		"client_id":     "abc123",
		"redirect_uri":  "http://localhost:8723/callback",
		"state":         "state-xyz",
		"response_type": "code",
		"prompt":        "consent",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("query %q = %q, want %q", k, got, want)
		}
	}
	if !strings.Contains(q.Get("scope"), "offline_access") {
		t.Errorf("scope %q missing offline_access", q.Get("scope"))
	}
	if !strings.HasPrefix(raw, authorizeURL) {
		t.Errorf("URL %q does not target %q", raw, authorizeURL)
	}
}

func TestCallbackHandler(t *testing.T) {
	const state = "the-state"
	tests := []struct {
		name     string
		query    string
		wantCode string
		wantErr  bool
		wantHTTP int
	}{
		{"success", "?code=AUTHCODE&state=" + state, "AUTHCODE", false, http.StatusOK},
		{"state mismatch", "?code=AUTHCODE&state=wrong", "", true, http.StatusBadRequest},
		{"provider error", "?error=access_denied&error_description=nope", "", true, http.StatusBadRequest},
		{"missing code", "?state=" + state, "", true, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codeCh := make(chan string, 1)
			errCh := make(chan error, 1)
			h := callbackHandler("/callback", state, codeCh, errCh)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/callback"+tt.query, nil)
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantHTTP {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantHTTP)
			}
			if tt.wantErr {
				select {
				case <-errCh:
				default:
					t.Error("expected an error on errCh, got none")
				}
				return
			}
			select {
			case got := <-codeCh:
				if got != tt.wantCode {
					t.Errorf("code = %q, want %q", got, tt.wantCode)
				}
			default:
				t.Error("expected a code on codeCh, got none")
			}
		})
	}
}

func TestRandomTokenUnique(t *testing.T) {
	a, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("randomToken returned identical values")
	}
	if len(a) < 40 {
		t.Errorf("randomToken too short: %d chars", len(a))
	}
}

func TestBasicAuthTransport(t *testing.T) {
	var gotUser, gotPass string
	var ok bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &basicAuthTransport{
		email: "me@example.com", token: "secret-token", base: http.DefaultTransport,
	}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if !ok || gotUser != "me@example.com" || gotPass != "secret-token" {
		t.Errorf("basic auth = (%q, %q, %v), want (me@example.com, secret-token, true)", gotUser, gotPass, ok)
	}
}
