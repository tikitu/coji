// Package config handles on-disk locations and persisted settings for coji.
//
// Everything coji persists (the OAuth client registration and the cached
// tokens) lives under a single directory returned by Dir, with 0600 file
// permissions. We deliberately use os.UserConfigDir so the location is sane
// per-platform without extra dependencies:
//
//	macOS:   ~/Library/Application Support/coji
//	Linux:   ~/.config/coji            (or $XDG_CONFIG_HOME/coji)
//	Windows: %AppData%\coji
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// dirName is the per-user subdirectory under os.UserConfigDir.
const dirName = "coji"

// Auth method identifiers stored in Config.AuthMethod.
const (
	MethodOAuth = "oauth" // OAuth 2.0 (3LO), bring-your-own Atlassian app
	MethodToken = "token" // email + API token (HTTP basic auth)
)

// Config holds how coji authenticates and which Confluence site it targets.
// Some fields are secrets (ClientSecret, APIToken), so the file is written
// 0600. All values are user-supplied; coji never embeds credentials.
type Config struct {
	// AuthMethod selects the credential flow: MethodOAuth or MethodToken.
	AuthMethod string `json:"auth_method,omitempty"`

	// SiteURL is the Confluence site (e.g. https://acme.atlassian.net). Used
	// for display, and as the API base host for token auth.
	SiteURL string `json:"site_url,omitempty"`

	// --- OAuth (MethodOAuth) ---

	// ClientID and ClientSecret come from the Atlassian app's
	// Settings -> Authentication details (OAuth 2.0), not the App ID.
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`

	// CloudID identifies the site for OAuth API calls, resolved from the
	// access token via the accessible-resources endpoint and cached here.
	CloudID string `json:"cloud_id,omitempty"`

	// RedirectURL is the loopback callback registered in the Atlassian app.
	// Must match exactly. Defaults to DefaultRedirectURL when empty.
	RedirectURL string `json:"redirect_url,omitempty"`

	// --- API token (MethodToken) ---

	// Email is the Atlassian account email used for HTTP basic auth.
	Email string `json:"email,omitempty"`
	// APIToken is a token created at id.atlassian.com/manage-profile/security/api-tokens.
	APIToken string `json:"api_token,omitempty"`
}

// DefaultRedirectURL is the loopback callback the login flow listens on. It
// must be registered verbatim as a Callback URL in the Atlassian app.
const DefaultRedirectURL = "http://localhost:8723/callback"

// Dir returns the coji config directory, creating it (0700) if needed.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	dir := filepath.Join(base, dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

func configPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the persisted config. A missing file yields a zero Config and no
// error, so callers can treat "not configured yet" uniformly.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &c, nil
}

// Save writes the config atomically with 0600 permissions.
func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return WriteFileAtomic(path, data, 0o600)
}

// Redirect returns the configured redirect URL or the default.
func (c *Config) Redirect() string {
	if c.RedirectURL != "" {
		return c.RedirectURL
	}
	return DefaultRedirectURL
}

// WriteFileAtomic writes data to a temp file in the same dir then renames it
// into place, so a crash mid-write never leaves a truncated file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
