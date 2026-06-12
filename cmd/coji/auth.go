package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mgilbir/coji/internal/auth"
	"github.com/mgilbir/coji/internal/config"
	"github.com/mgilbir/coji/internal/confluence"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication with Confluence",
	}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthTokenCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	cmd.AddCommand(newAuthStatusCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var clientID, clientSecret, site, redirect string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authorize via OAuth 2.0 (3LO)",
		Long: `Authorize coji using the OAuth 2.0 (3LO) flow.

You must first register an OAuth 2.0 app at https://developer.atlassian.com,
grant it the Confluence scopes (read:page, write:page, read:space), and add the
callback URL ` + config.DefaultRedirectURL + ` . Provide the app's client ID and
secret (from Settings -> Authentication details) via flags or the
COJI_CLIENT_ID / COJI_CLIENT_SECRET environment variables; they are saved to
your config so you only pass them once.

For a simpler, registration-free setup, use ` + "`coji auth token`" + ` instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Flags override env override previously-saved config.
			if clientID == "" {
				clientID = os.Getenv("COJI_CLIENT_ID")
			}
			if clientSecret == "" {
				clientSecret = os.Getenv("COJI_CLIENT_SECRET")
			}
			if clientID != "" {
				cfg.ClientID = clientID
			}
			if clientSecret != "" {
				cfg.ClientSecret = clientSecret
			}
			if redirect != "" {
				cfg.RedirectURL = redirect
			}
			if cfg.ClientID == "" || cfg.ClientSecret == "" {
				return fmt.Errorf("missing client credentials: pass --client-id/--client-secret or set COJI_CLIENT_ID/COJI_CLIENT_SECRET")
			}
			cfg.AuthMethod = config.MethodOAuth
			if err := cfg.Save(); err != nil {
				return err
			}

			ctx := cmd.Context()
			tok, err := auth.Login(ctx, cfg)
			if err != nil {
				return err
			}

			// Resolve which Confluence site this token targets.
			sites, err := auth.AccessibleResources(ctx, tok.AccessToken)
			if err != nil {
				return fmt.Errorf("listing accessible sites: %w", err)
			}
			chosen, err := pickSite(sites, site)
			if err != nil {
				return err
			}
			cfg.CloudID = chosen.ID
			cfg.SiteURL = chosen.URL
			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Logged in to %s (%s) via OAuth\n", chosen.URL, chosen.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID (or COJI_CLIENT_ID)")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret (or COJI_CLIENT_SECRET)")
	cmd.Flags().StringVar(&site, "site", "", "site URL or name to select when the token grants access to multiple")
	cmd.Flags().StringVar(&redirect, "redirect-url", "", "OAuth callback URL (must match the app registration; default "+config.DefaultRedirectURL+")")
	return cmd
}

// tokenURLHint is where users create an API token.
const tokenURLHint = "https://id.atlassian.com/manage-profile/security/api-tokens"

func newAuthTokenCmd() *cobra.Command {
	var email, site string

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Authenticate with an API token (basic auth)",
		Long: `Authenticate using your Atlassian account email and an API token.

Create a token at ` + tokenURLHint + ` .
This needs no app registration. With no flags, coji prompts for the values
(the token input is hidden). For non-interactive use, supply --email and --site
and set the token in the COJI_API_TOKEN environment variable. Credentials are
stored in your config (0600).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if email == "" {
				email = os.Getenv("COJI_EMAIL")
			}
			token := os.Getenv("COJI_API_TOKEN")

			// Interactively fill anything still missing. The token is never
			// taken from the command line; only env or a hidden prompt.
			if email == "" || site == "" || token == "" {
				if !stdinIsTerminal() {
					return fmt.Errorf("missing credentials: set --email, --site, and COJI_API_TOKEN, or run interactively")
				}
			}
			if email == "" {
				if email, err = promptLine("Atlassian account email: "); err != nil {
					return err
				}
			}
			if site == "" {
				fmt.Fprintln(os.Stderr, "Your Confluence site is the base address you see in the browser, e.g. https://acme.atlassian.net")
				if site, err = promptLine("Confluence site URL: "); err != nil {
					return err
				}
			}
			if token == "" {
				fmt.Fprintf(os.Stderr, "Create an API token at %s\n", tokenURLHint)
				if token, err = promptSecret("API token (input hidden): "); err != nil {
					return err
				}
			}

			if email == "" || token == "" || site == "" {
				return fmt.Errorf("email, site, and API token are all required")
			}

			cfg.AuthMethod = config.MethodToken
			cfg.Email = email
			cfg.APIToken = token
			cfg.SiteURL = normalizeSite(site)
			if err := cfg.Save(); err != nil {
				return err
			}

			// Verify the credentials by making a real call.
			if err := verifySession(cmd, cfg); err != nil {
				return fmt.Errorf("credentials saved but verification failed: %w", err)
			}
			fmt.Printf("Authenticated to %s as %s via API token\n", cfg.SiteURL, email)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Atlassian account email (or COJI_EMAIL; prompted if omitted)")
	cmd.Flags().StringVar(&site, "site", "", "Confluence site base URL (the address in your browser), e.g. https://acme.atlassian.net (prompted if omitted)")
	return cmd
}

// normalizeSite ensures the site has a scheme and no trailing slash.
func normalizeSite(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s != "" && !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	return s
}

// verifySession constructs a session and makes a cheap authenticated request to
// confirm the credentials work.
func verifySession(cmd *cobra.Command, cfg *config.Config) error {
	sess, err := auth.NewSession(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	return confluence.New(sess.HTTPClient, sess.BaseURL).Ping(cmd.Context())
}

// pickSite selects the Confluence site to operate on. With a single site it is
// chosen automatically; otherwise the user must disambiguate via want (matched
// against URL or name, case-insensitively).
func pickSite(sites []auth.Site, want string) (auth.Site, error) {
	if len(sites) == 0 {
		return auth.Site{}, fmt.Errorf("the token has no accessible Confluence sites; check the app's scopes")
	}
	if want != "" {
		w := strings.ToLower(want)
		for _, s := range sites {
			if strings.EqualFold(s.URL, want) || strings.Contains(strings.ToLower(s.URL), w) || strings.EqualFold(s.Name, want) {
				return s, nil
			}
		}
		return auth.Site{}, fmt.Errorf("no accessible site matches %q", want)
	}
	if len(sites) == 1 {
		return sites[0], nil
	}
	var b strings.Builder
	b.WriteString("multiple sites are accessible; re-run with --site set to one of:\n")
	for _, s := range sites {
		fmt.Fprintf(&b, "  %s  (%s)\n", s.URL, s.Name)
	}
	return auth.Site{}, fmt.Errorf("%s", b.String())
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			// Clear the cached OAuth token and any stored credentials.
			if err := auth.Logout(); err != nil {
				return err
			}
			cfg.AuthMethod = ""
			cfg.APIToken = ""
			cfg.Email = ""
			cfg.CloudID = ""
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Println("Logged out (credentials removed).")
			return nil
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			switch cfg.AuthMethod {
			case config.MethodToken:
				fmt.Printf("Authenticated to %s as %s (API token)\n", cfg.SiteURL, cfg.Email)
			case config.MethodOAuth:
				tok, err := auth.CurrentOAuthToken()
				if err != nil {
					return err
				}
				if tok == nil {
					fmt.Println("OAuth configured but not logged in. Run `coji auth login`.")
					return nil
				}
				fmt.Printf("Logged in to %s (OAuth)\n", cfg.SiteURL)
				fmt.Printf("  cloud id: %s\n", cfg.CloudID)
				if tok.Expiry.IsZero() {
					fmt.Println("  token:    no known expiry")
				} else if tok.Valid() {
					fmt.Printf("  token:    valid until %s\n", tok.Expiry.Format("2006-01-02 15:04:05"))
				} else {
					fmt.Println("  token:    expired (will refresh on next use)")
				}
			default:
				fmt.Println("Not authenticated. Run `coji auth login` or `coji auth token`.")
			}
			return nil
		},
	}
}
