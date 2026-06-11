package main

import (
	"github.com/mgilbir/coji/internal/auth"
	"github.com/mgilbir/coji/internal/config"
	"github.com/mgilbir/coji/internal/confluence"
	"github.com/mgilbir/coji/internal/core"
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

// newService builds the core use-case service for page commands.
func newService(cmd *cobra.Command) (*core.Service, error) {
	client, _, err := newClient(cmd)
	if err != nil {
		return nil, err
	}
	return core.New(client), nil
}
