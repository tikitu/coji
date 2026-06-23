package main

import (
	"os"
	"path/filepath"

	"github.com/mgilbir/coji/internal/auth"
	"github.com/mgilbir/coji/internal/config"
	"github.com/mgilbir/coji/internal/confluence"
	"github.com/mgilbir/coji/internal/core"
	"github.com/mgilbir/coji/internal/policy"
	"github.com/spf13/cobra"
)

// newClient loads config, builds an auth session, and returns a ready API
// client plus the session (for site/base info). Used by all commands that talk
// to Confluence.
func newClient(cmd *cobra.Command) (*confluence.Client, *auth.Session, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	sess, err := auth.NewSession(cmd.Context(), cfg)
	if err != nil {
		return nil, nil, err
	}
	return confluence.New(sess.HTTPClient, sess.BaseURL), sess, nil
}

// newService builds the core use-case service, gated by the resolved policy.
func newService(cmd *cobra.Command) (*core.Service, error) {
	client, sess, err := newClient(cmd)
	if err != nil {
		return nil, err
	}
	pol, err := loadPolicy()
	if err != nil {
		return nil, err
	}
	return core.New(client, core.WithPolicy(pol), core.WithSiteURL(sess.SiteURL)), nil
}

// policyFilePath returns the policy file path: --policy, else $COJI_POLICY,
// else <config dir>/policy.json.
func policyFilePath() (string, error) {
	if policyPath != "" {
		return policyPath, nil
	}
	if env := os.Getenv("COJI_POLICY"); env != "" {
		return env, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "policy.json"), nil
}

// loadPolicy loads the resolved policy file (nil when none exists).
func loadPolicy() (*policy.Policy, error) {
	path, err := policyFilePath()
	if err != nil {
		return nil, err
	}
	return policy.Load(path)
}
