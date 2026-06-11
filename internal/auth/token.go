package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgilbir/coji/internal/config"
	"golang.org/x/oauth2"
)

// tokenFile is the filename for the cached OAuth token under config.Dir. Only
// used for MethodOAuth; token auth keeps its credential in the config.
const tokenFile = "token.json"

func tokenPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFile), nil
}

// loadOAuthToken reads the cached OAuth token. A missing file returns
// (nil, nil) so callers can treat "logged out" uniformly.
func loadOAuthToken() (*oauth2.Token, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading token: %w", err)
	}
	var t oauth2.Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}
	return &t, nil
}

// saveOAuthToken writes the token with 0600 permissions.
func saveOAuthToken(t *oauth2.Token) error {
	path, err := tokenPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding token: %w", err)
	}
	return config.WriteFileAtomic(path, data, 0o600)
}

// deleteOAuthToken removes the cached token, succeeding if already absent.
func deleteOAuthToken() error {
	path, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
