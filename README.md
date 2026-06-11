# coji

A CLI for reading, creating, and editing [Confluence Cloud](https://developer.atlassian.com/cloud/confluence/rest/v2/intro/)
pages from Markdown.

`coji` fetches pages as Markdown, and creates/edits them by passing Markdown
in. It speaks the Confluence REST API **v2** and converts Markdown ⇆ the
Confluence *storage* format under the hood.

## Install

```sh
go install github.com/mgilbir/coji/cmd/coji@latest
```

or build from source:

```sh
go build -o coji ./cmd/coji
```

## Authentication

`coji` never embeds any credentials. You authenticate with **your own**
credentials, stored locally under your OS config dir
(`~/Library/Application Support/coji` on macOS, `~/.config/coji` on Linux) with
`0600` permissions. Two methods are supported.

> **Your site URL** is the base web address of your Confluence Cloud instance —
> what you see in the browser when you use Confluence, e.g.
> `https://acme.atlassian.net`. Use that base only (no `/wiki` or page path);
> coji appends the API path itself. You can paste it with or without the
> `https://` prefix.

### API token (simplest — no app registration)

Run `coji auth token` and follow the prompts — it asks for your email, site,
and the token (hidden input), pointing you to where to create one:

```sh
coji auth token
# Atlassian account email: you@example.com
# Confluence site URL (e.g. https://acme.atlassian.net): https://your-domain.atlassian.net
# Create an API token at https://id.atlassian.com/manage-profile/security/api-tokens
# API token (input hidden): ••••••••
```

The token is never accepted on the command line. For non-interactive use, pass
`--email`/`--site` and set `COJI_API_TOKEN` (and optionally `COJI_EMAIL`).

### OAuth 2.0 (3LO)

For OAuth you register your own Atlassian app (Atlassian has no shared/public
client for CLIs):

1. Create an **OAuth 2.0 (3LO)** app at <https://developer.atlassian.com>.
2. Under **Permissions → Confluence API**, grant at least `read:page`,
   `write:page`, and `read:space`.
3. Under **Authorization**, add the callback URL `http://localhost:8723/callback`.
4. Copy the **Client ID** and **Secret** from **Settings → Authentication details**
   (note: that is *not* the App ID).
5. Log in:

   ```sh
   coji auth login --client-id <id> --client-secret <secret>
   ```

   (Credentials can also come from `COJI_CLIENT_ID` / `COJI_CLIENT_SECRET`.) Your
   browser opens for consent; the token (with refresh) is cached and refreshed
   automatically.

Check or clear your session anytime:

```sh
coji auth status
coji auth logout
```

## Usage

```sh
# Read a page as Markdown (default)
coji page get 123456

# Read raw storage or ADF
coji page get 123456 --format storage
coji page get 123456 -o page.md          # write to a file

# Create a page from a Markdown file (or stdin)
coji page create --space ENG --title "Design Notes" --input notes.md
echo "# Hi" | coji page create -s ENG -t "Quick note"

# Edit a page (version is bumped automatically)
coji page edit 123456 --input notes.md --message "update design"
cat notes.md | coji page edit 123456
```

`--format` accepts `markdown` (default), `storage`, or `adf`. With `storage` or
`adf`, the body is passed through verbatim with no conversion.

## Design

`coji` separates concerns so a future TUI can reuse the same logic:

- `internal/confluence` — typed v2 API client (pages, spaces).
- `internal/auth` — OAuth (via `golang.org/x/oauth2`) and API-token sessions;
  produces an authorized `*http.Client` and the correct API base URL.
- `internal/markdown` — Markdown ⇆ storage conversion (goldmark + `x/net/html`).
- `internal/core` — the use-case layer (get/create/edit) with no UI concerns.
- `cmd/coji` — the cobra CLI, a thin shell over `internal/core`.
