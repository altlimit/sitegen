# SiteGen — Static Site Generator

SiteGen is a fast, zero-dependency static site generator written in Go. It uses Go's `text/template` engine with custom helper functions, YAML frontmatter, JSON data files, and supports pagination, dynamic page generation, live reload, and minification.

## Project Structure

```
sitegen/
├── main.go                 # CLI entry point, flag parsing, build orchestration
├── cmd/
│   └── sharerelay/         # Standalone relay server for the -share feature
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
│   ├── share/              # Client for -share feature (tunnel via yamux)
│   │   └── client.go       # Connects to relay, multiplexes HTTP over WebSocket
│   └── server/             # Shared server utilities
├── site/                   # SiteGen's own website (built with sitegen)
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
- **Share Feature**: Public URL tunneling via `hashicorp/yamux` multiplexing over WebSocket

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

# Build the binary
go build -o sitegen .

# Build the share relay server
go build -o sharerelay ./cmd/sharerelay
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
```
