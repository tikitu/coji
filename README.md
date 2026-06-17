# coji

A CLI for reading, creating, and editing [Confluence Cloud](https://developer.atlassian.com/cloud/confluence/rest/v2/intro/)
pages from Markdown.

`coji` fetches pages as Markdown, and creates/edits them by passing Markdown
in. It speaks the Confluence REST API **v2** and converts Markdown ⇆ the
Confluence *storage* format under the hood.

## Install

Requires **Go 1.26+**.

```sh
go install github.com/mgilbir/coji/cmd/coji@latest
```

This installs the `coji` binary to `$(go env GOBIN)` (or `$(go env GOPATH)/bin`,
typically `~/go/bin`). Make sure that directory is on your `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

To install a specific version, replace `@latest` with a tag (e.g. `@v0.1.0`) or
commit. To build from a clone instead:

```sh
git clone https://github.com/mgilbir/coji.git
cd coji
go build -o coji ./cmd/coji   # or: go install ./cmd/coji
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

# Create a private page (only you can view/edit it)
coji page create --space ENG --title "My notes" --input notes.md --private

# Edit a page (version is bumped automatically)
coji page edit 123456 --input notes.md --message "update design"
cat notes.md | coji page edit 123456
```

`--format` accepts `markdown` (default), `storage`, or `adf`. With `storage` or
`adf`, the body is passed through verbatim with no conversion.

### Browsing & nesting

To find where to put a page, browse the content tree:

```sh
# List spaces (KEY for --space, HOMEPAGE is a page ID you can use as a tree root)
coji space list
coji space list --key ENG

# Direct children of a page (type + ID for each)
coji page children 123456

# Full subtree, drawn as a tree (descends into folders too)
coji page children 123456 --recursive

# If the ID is a folder rather than a page
coji page children 789012 --folder
```

Each entry shows its **type** and **ID**. To nest a new page, pass that ID as
`--parent` — it accepts a **page or a folder** ID:

```sh
coji page create --space ENG --title "Child page" --parent 123456 --input notes.md
```

Omitting `--parent` puts the page under the space homepage.

### Interactive browser

`coji browse` opens an interactive content-tree browser:

```sh
coji browse            # start from your spaces
coji browse ENG        # start in a space
coji browse 123456     # root the tree at a page
```

Keys:

| Key | Action |
| --- | --- |
| `↑`/`↓` | move |
| `enter` | expand/collapse (loads children lazily) |
| `v` | view the page (rendered markdown, scrollable) |
| `e` | edit the page in `$EDITOR`, saved back with a version bump |
| `n` | create a child page under the selected node (prompts for a title — `tab` toggles private — then opens `$EDITOR`) |
| `y` | select a node's ID (printed on exit — handy for `--parent`) |
| `q` | quit |

Editing uses `$VISUAL`, then `$EDITOR`, falling back to `vi`.

## Access policy

coji can enforce a per-space access policy as a **local guardrail** — for
example, treating some spaces as read-only so you don't accidentally edit them.
This is *not* a security boundary (your credentials' real permissions still
apply server-side); it just stops coji from performing operations you've opted
out of.

Create a `policy.json` in the config dir (or point `--policy` / `$COJI_POLICY`
at one), keyed by **space key**:

```json
{
  "default":          "read-write",
  "personal-default": "none",
  "spaces": {
    "ENG":     "read-write",
    "ARCHIVE": "read-only",
    "SECRET":  "none",
    "DRAFTS":  ["read", "create", "edit"],
    "~jdoe":   "read-only"
  }
}
```

- Access levels: `none`, `read-only`, `read-write` — or an explicit list of
  `read` / `create` / `edit` / `delete`.
- `default` applies to spaces not listed (defaults to `read-write`, so the file
  only restricts what you name; set it to `read-only` for an allowlist style).
- `personal-default` applies to **personal spaces** — those whose key starts
  with `~` — that aren't listed explicitly. It defaults to `none`, so personal
  spaces are off-limits unless you opt in (either by raising `personal-default`
  or by naming a specific personal space, like `~jdoe` above, which overrides
  it). This keeps coji out of individuals' personal spaces by default, even
  when `default` is permissive.
- With **no** policy file, everything is allowed (default behavior).

Checks apply to both the CLI and the TUI. Inspect the resolved policy with:

```sh
coji policy show
```

## Design

`coji` separates concerns so a future TUI can reuse the same logic:

- `internal/confluence` — typed v2 API client (pages, spaces).
- `internal/auth` — OAuth (via `golang.org/x/oauth2`) and API-token sessions;
  produces an authorized `*http.Client` and the correct API base URL.
- `internal/markdown` — Markdown ⇆ storage conversion (goldmark + `x/net/html`).
- `internal/core` — the use-case layer (get/create/edit) with no UI concerns,
  where the access policy is enforced.
- `internal/policy` — the per-space access policy (a local guardrail).
- `cmd/coji` — the cobra CLI, a thin shell over `internal/core`.

### Generated types

`internal/confluence/gen` holds Go types generated from the OpenAPI spec in
`spec/` (models only — the HTTP client is hand-written). As the tool grows to
cover more endpoints, build them with these ready-made types. Regenerate after
updating the spec:

```sh
go generate ./internal/confluence/...
```

The generator (`oapi-codegen`) is pinned as a tool dependency in `go.mod`, so no
separate install is needed.
