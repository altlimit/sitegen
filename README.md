# SiteGen

[![Go Report Card](https://goreportcard.com/badge/github.com/altlimit/sitegen)](https://goreportcard.com/report/github.com/altlimit/sitegen)
[![Latest Release](https://img.shields.io/github/v/release/altlimit/sitegen)](https://github.com/altlimit/sitegen/releases)
[![License](https://img.shields.io/github/license/altlimit/sitegen)](LICENSE)

Sitegen is a simple, flexible, and fast static site generator written in Go. It supports incremental builds, live reloading, and a powerful template system.

## Features

- 🚀 **Fast & Incremental**: Builds only what's needed.
- 🔄 **Live Reload**: Built-in development server with changes detection.
- 🎨 **Templating**: Flexible Go templates with custom functions.
- 📝 **Markdown**: Write pages in `.md` with automatic HTML conversion.
- 📦 **Zero Dependency**: Single binary, easy to install.
- 🔧 **File Handlers**: Custom build commands for specific file types (e.g. CSS, JS).
- 🌐 **Public Sharing**: Instantly share your dev server via a public URL (like ngrok, built-in).
- ✏️ **Built-in CMS**: Optional `-cms` editing UI for non-technical editors — block builder, collections, data files, and image uploads, writing the same source files. See [docs/CMS.md](docs/CMS.md).

## Installation

Install via [alt](https://github.com/altlimit/alt) — a zero-config CLI distribution proxy for GitHub Releases:

```bash
# Install alt (one-time setup)
curl -fsSL https://raw.githubusercontent.com/altlimit/alt/main/scripts/install.sh | sh

# Install sitegen
alt install altlimit/sitegen
```

### Windows (PowerShell)

```powershell
# Install alt (one-time setup)
powershell -Command "iwr https://raw.githubusercontent.com/altlimit/alt/main/scripts/install.ps1 -useb | iex"

# Install sitegen
alt install altlimit/sitegen
```

Or download binaries directly from the [Releases Page](https://github.com/altlimit/sitegen/releases).

## Quick Start

1. **Create a new project:**
   ```bash
   mkdir my-website
   cd my-website
   sitegen -create
   ```

2. **Start development server:**
   ```bash
   sitegen -serve
   ```
   Open [http://localhost:8888](http://localhost:8888) in your browser.

3. **Build for production:**
   ```bash
   sitegen -clean -minify
   ```

## Usage

```bash
sitegen [options]

Options:
  -create              Create a new site template
  -site <path>         Root site path (default: "./site")
  -serve               Start development server
  -port <port>         Port for development server (default: "8888")
  -clean               Clean public dir before build
  -minify              Minify HTML/JS/CSS output
  -cmd-timeout <secs>  Timeout for serve/build frontmatter commands (default: 120, 0 disables)
  -public <dir>        Public output directory (default: "./public")
  -base <path>         Base URL path (default: "/")
  -share               Enable public sharing via sitegen.dev
  -share-auth <u:p>    Basic auth for share ("user:pass")
  -share-server <addr> Share relay server (default: "sitegen.dev:9443")
  -cms                 Enable the built-in editing UI at /__cms (serve mode)
  -cms-auth <u:p>      Basic auth for the CMS ("user:pass")
  -help                Show help
```

## Template System

Sitegen uses Go's `html/template` with extra helper functions.

### Functions

| Function | Description |
|----------|-------------|
| `path` | Prefixes path with base URL. |
| `sources "prop" "pattern"` | Returns list of sources matching pattern. |
| `data "file.json"` | Loads JSON data from `data/` directory. |
| `sort "prop" "order"` | Sorts input array/slice. |
| `limit n` | Limits the array/slice to `n` items. |
| `offset n` | Offsets the array/slice by `n` items. |
| `paginate n` | Paginates input. Populates `.Page` and `.Pages`. |
| `page "path"` | Creates a parameterized page from current source. |

### Page Variables

- `.<key>`: Any variable defined in YAML frontmatter is accessible directly at the root (e.g., `.title`). When iterating over sources (e.g. `range sources`), use `.Meta.<key>` on the source item instead.
- `.Dev`: Boolean, true if running in development mode.
- `.Source`: Current source object (`.Source.Meta` has the raw frontmatter map).
- `.BasePath`: Configured base path.
- `.Today`: Current date (YYYY-MM-DD).
- `.Year`: Current year (YYYY).
- `.Path`: Current page path (if parameterized).
- `.Page`, `.Pages`: Pagination info.
- `.BuildID`: Unix timestamp string, regenerated on every build (useful for cache busting).
- `.LastMod`: Last-modified date (YYYY-MM-DD). Uses the `updated:` frontmatter if set, otherwise the source file's mtime. Also available per-source in `range sources` loops (e.g. `{{.LastMod}}`).

### Basic Example

**`src/about.html`**:
```html
---
title: About Us
template: main.html
---

{{define "content"}}
  <h1>{{ .title }}</h1>
  <p>Welcome to our site!</p>
{{end}}
```

## Sitemap

A generated site includes a `src/sitemap.xml` that is itself a template — it lists
every page automatically, so new content shows up without editing anything.

```xml
---
parse: text
---
{{- $site := data "site.json" -}}
{{- $t := .Today -}}
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
{{range sources "Local" "**/src/**.{html,md}"}}
{{- if and (not (.Local | contains "404.html")) (ne (.Value "Meta.sitemap") "false") -}}
  <url>
    <loc>{{$site.url}}{{.Path}}</loc>
    <lastmod>{{if .LastMod}}{{.LastMod}}{{else}}{{$t}}{{end}}</lastmod>
  </url>
  {{- end -}}
{{- end -}}
</urlset>
```

- **All page types**: the `{html,md}` glob covers both HTML and Markdown pages.
- **`<loc>`**: uses each source's `.Path` prefixed with `url` from `data/site.json`.
- **`<lastmod>`**: uses `.LastMod` — the `updated:` frontmatter date if present, else
  the file's mtime, falling back to the build date.

Exclude a page from the sitemap with a single frontmatter key:

```yaml
---
sitemap: false
---
```

Pin an explicit last-modified date (survives a fresh `git clone`, which resets mtimes):

```yaml
---
updated: 2026-07-01
---
```

## File Handlers

Customize how files are processed by adding a frontmatter block to any file (css, js, etc).

```css
/*
---
serve: npm run build:css
build: npm run build:prod:css
---
*/
```

## Public Sharing

Share your development server publicly with a single flag — no ngrok or third-party tunnels needed:

```bash
sitegen -serve -share
```

This creates a public URL like `https://<id>.sitegen.dev` that tunnels to your local dev server with hot reload support.

To require a password:

```bash
sitegen -serve -share -share-auth "admin:secret"
```

The share tunnel only serves static files from your `public/` directory (GET/HEAD only) and reconnects automatically if the connection drops.

## Built-in CMS

A built-in editing UI lets non-technical editors (marketing, writers) edit your
content without touching code or git. It reads and writes the **same source
files** the generator already consumes (`src/*` and `data/*.json`) — no database,
no separate content store, so git history and templates keep working.

```bash
sitegen -serve -cms                       # editor at http://localhost:8888/__cms
sitegen -serve -cms -cms-auth "me:secret" # require a password
```

Without any config it works in **raw mode** (list source files, edit frontmatter
+ body). Add an optional `site/cms.yaml` to get typed widgets, a block builder,
folder collections with a "+ New" button, editable data files, and image
uploads. The save → rebuild → live-reload loop runs through the normal pipeline,
and the preview pane follows the page you're editing.

See **[docs/CMS.md](docs/CMS.md)** for the full `cms.yaml` reference.

## Contributing

Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

## License

[MIT](LICENSE.txt)