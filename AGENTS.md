# SiteGen — Static Site Generator

SiteGen is a fast, zero-dependency static site generator written in Go. It uses Go's `text/template` engine with custom helper functions, YAML frontmatter, JSON data files, and supports pagination, dynamic page generation, live reload, and minification.

## Project Structure

```
sitegen/
├── main.go                 # CLI entry point, flag parsing, build orchestration
├── worker/                 # Cloudflare Workers: the -share relay and the sitegen.dev website
│   ├── site.wrangler.jsonc # Website: serves public/ (built from site/) at sitegen.dev
│   ├── src/index.ts        # Relay router: relay.<domain>/connect and <tunnel>.<domain>
│   ├── src/tunnel.ts       # Tunnel Durable Object (hibernatable WebSockets)
│   └── src/protocol.ts     # Wire protocol constants (see docs/SHARE_PROTOCOL.md)
├── pkg/
│   ├── sitegen/            # Core static site generator engine
│   │   ├── gen.go          # Site generation, template execution, file processing
│   │   ├── server.go       # Dev server with live reload (SSE), file watcher
│   │   ├── tui.go          # Interactive TUI using bubbletea
│   │   ├── image.go        # Image optimization (resize, WebP conversion)
│   │   ├── markdown.go     # Markdown to HTML conversion via goldmark
│   │   ├── minify.go       # HTML/CSS/JS/SVG/XML/JSON minification
│   │   ├── data.go         # JSON data file loader
│   │   ├── source.go       # Source file representation and frontmatter parsing
│   │   └── funcs.go        # Custom template functions (sources, sort, paginate, etc.)
│   ├── share/              # Client for -share feature
│   │   └── client.go       # Connects to the relay, serves tunnelled HTTP over one WebSocket
│   ├── publish/            # -publish: Sign in with altengine and the static deploy API
│   │   ├── auth.go         # Browser (PKCE, loopback) and device sign-in, revoke
│   │   ├── api.go          # api.altengine.net: /v1/static and one-shot MCP tools/call
│   │   ├── deploy.go       # Manifest (path → sha256, size), limits, parallel signed uploads
│   │   ├── store.go        # Key storage (OS keyring, 0600 file fallback), site/publish.json
│   │   └── publish.go      # Key resolution, find-or-create the instance, deploy, sign out
│   └── server/             # Shared server utilities
├── site/                   # sitegen.dev, and the starter copied by -create (embedded in the binary)
│   ├── src/                # Source HTML/MD/CSS/JS files
│   ├── templates/          # Shared layout templates
│   └── data/               # JSON data files
├── build.sh                # Cross-platform build script
└── install.sh              # Installation script
```

## Architecture

- **Language**: Go (module: `github.com/altlimit/sitegen`)
- **Template Engine**: Go `html/template` and `text/template` with custom functions
- **Frontmatter**: YAML between `---` delimiters (parsed via `gopkg.in/yaml.v2`)
- **Data Files**: JSON loaded from a `data/` directory
- **Dev Server**: Built-in HTTP server with SSE-based live reload and file watching (`fsnotify`)
- **TUI**: Interactive terminal UI using `charmbracelet/bubbletea` and `lipgloss`
- **Publish Feature**: `-publish` deploys `public/` to altengine static hosting over plain HTTP (no SDK). The contract is https://www.altengine.net/docs/sign-in-with-altengine/ and https://www.altengine.net/docs/static/. Sign-in asks for `static:full:sitegen-*`, so every instance sitegen creates is `sitegen-<name>` and one key per machine covers them all (signing in again from the same device replaces the earlier key, so per-site keys can't work). sitegen's OAuth client ID is `https://sitegen.dev/altengine-client.json`, served from `site/src/altengine-client.json`; it must stay at that exact URL (altengine follows no redirects) and `-create` leaves it out of new sites.
- **Share Feature**: Public URL tunneling over one WebSocket (`coder/websocket`) to a Cloudflare Worker relay in `worker/`. Protocol v1 is in `docs/SHARE_PROTOCOL.md`; the Go client, the Worker, and that doc must change together. Tunnelled requests go through `server.ShareHandler`, which hides `/__cms` and swaps SSE live reload for the relay's WebSocket endpoint.

## Key Conventions

- **URL routing**: Source files are automatically converted to clean URLs (e.g., `src/about.html` → `/about/index.html`).
- **Template inheritance**: Pages define blocks (e.g., `{{define "content"}}`) that are rendered within layout templates specified via `template:` frontmatter key.
- **Frontmatter access**: In page templates, frontmatter keys are available directly (e.g., `.title`). When iterating over sources via `range sources`, use `.Meta.<key>` instead.
- **Non-HTML parsing**: XML, TXT, and other text files can use Go's `text/template` by setting `parse: text` in frontmatter.
- **File handlers**: CSS/JS files can specify `serve:` and `build:` frontmatter keys to run custom shell commands instead of default copy behavior.

## Building & Running

```bash
# Development with live reload
sitegen -serve

# Production build
sitegen -clean -minify

# Build and publish to altengine static hosting (ALTENGINE_API_KEY in CI)
sitegen -publish

# Build the binary
go build -o sitegen .

# Share relay (Cloudflare Worker)
cd worker && npm ci
npm run dev        # local relay; then: sitegen -serve -share -share-server ws://relay.localhost:8787
npm test           # Worker tests (vitest in workerd)
npm run deploy     # wrangler deploy

# Website (sitegen.dev): CI deploys it on push to master via .github/workflows/site.yaml
go run . -clean -minify && (cd worker && npx wrangler deploy -c site.wrangler.jsonc)
```

## Template Functions

The following custom functions are available in templates:

| Function | Signature | Description |
|----------|-----------|-------------|
| `data` | `data "file.json"` | Loads and parses JSON from `data/` directory |
| `sources` | `sources "property" "glob"` | Returns `[]*Source` matching glob on property (`Path`, `Local`, `Filename`, `RelPath`, `Ext`, `Meta.<key>`) |
| `sort` | `sort "property" "asc\|desc" list` | Sorts a slice by property |
| `limit` | `limit n list` | Returns first `n` items |
| `offset` | `offset n list` | Skips first `n` items |
| `paginate` | `paginate n list` | Paginates list, sets `.Page`/`.Pages`, generates page files |
| `pages` | `pages source` | Returns `[]Page` for pagination links (`.Path`, `.Page`, `.Active`) |
| `page` | `page "source" "slug"` | Creates a dynamically generated page, returns its path |
| `path` | `path "/url"` | Prefixes with base path (`-base` flag) |
| `hash` | `hash "/file"` | Short SHA-256 of `src/<file>`; cache-busting that changes only when the file does |
| `json` | `json value` | Serializes value to JSON string |
| `html` | `html string` | Marks string as safe HTML |
| `js` | `js string` | Marks string as safe JavaScript |
| `css` | `css string` | Marks string as safe CSS |
| `contains` | `contains substring string` | Returns true if string contains substring |
| `filter` | `filter "property" "regex" list` | Filters slice by regex match on property |
| `select` | `select map` | Converts map to sortable `[]kv` slice (`.Key`, `.Value`) |

## Page Variables

| Variable | Type | Description |
|----------|------|-------------|
| `.<key>` | `any` | Frontmatter values at root level |
| `.Dev` | `bool` | `true` in `-serve` mode |
| `.Source` | `*Source` | Current source (`.Name`, `.Local`, `.Path`, `.Ext`, `.CurrentPage`, `.TotalPages`, `.Meta`) |
| `.BasePath` | `string` | Base path (default `"/"`) |
| `.Today` | `string` | Current date `YYYY-MM-DD` |
| `.Year` | `string` | Current year `YYYY` |
| `.Path` | `string` | Current page path |
| `.Page` | `int` | Current page number (1-indexed) |
| `.Pages` | `int` | Total pages |
| `.BuildID` | `string` | Unix timestamp, changes every build |

## Special Frontmatter Keys

| Key | Type | Description |
|-----|------|-------------|
| `template` | `string` | Layout template file to use (e.g., `main.html`) |
| `path` | `string` | Override auto-generated URL path (e.g., `404.html`) |
| `parse` | `string` | Force template parsing: `text` or `html` |
| `serve` | `string` | Shell command to run in dev mode instead of copying |
| `build` | `string` | Shell command to run in production build instead of copying |
| `block` | `string` | Template block name for `.md` auto-wrap (default: `content`) |

## Testing

```bash
go test ./...
cd worker && npm test
```
