---
name: sitegen
description: How to build and manage static sites using the sitegen static site generator
---

# SiteGen — Static Site Generator

SiteGen is a fast, zero-dependency static site generator written in Go. It uses Go's `text/template` engine with custom helper functions, YAML frontmatter, JSON data files, and supports pagination, dynamic page generation, live reload, and minification.

## Project Structure

A sitegen project has this directory layout:

```
my-site/                  # -site flag (default: ./site)
├── src/                  # -source flag (default: src) — source files
│   ├── index.html        # becomes /public/index.html (root page)
│   ├── about.html        # becomes /public/about/index.html
│   ├── blog.html         # becomes /public/blog/index.html
│   ├── blog/
│   │   ├── post1.html    # becomes /public/blog/post1/index.html
│   │   └── post2.html    # becomes /public/blog/post2/index.html
│   ├── css/
│   │   └── styles.css    # copied as-is (or minified with -minify)
│   ├── img/
│   │   └── logo.svg      # copied as-is
│   ├── 404.html          # custom 404 page (needs path: 404.html in frontmatter)
│   └── sitemap.xml       # can use text templates with parse: text
├── templates/            # -templates flag (default: templates) — shared templates
│   ├── main.html         # base layout template
│   ├── head.html         # reusable head partial
│   └── nav.html          # reusable nav partial
└── data/                 # -data flag (default: data) — JSON data files
    ├── site.json         # global site config
    └── links.json        # navigation links, etc.

public/                   # -public flag (default: ./public) — build output
```

## CLI Flags

```bash
sitegen [options]

  -create        Create a new site from the built-in template
  -site <path>   Root site path (default: "./site")
  -source <dir>  Source folder relative to site path (default: "src")
  -data <dir>    Data folder relative to site path (default: "data")
  -templates <d> Template folder relative to site path (default: "templates")
  -public <path> Output directory (default: "./public")
  -base <path>   Base URL path prefix (default: "/")
  -serve         Start dev server with file watcher + live reload
  -port <port>   Dev server port (default: "8888")
  -clean         Remove public dir before building
  -minify        Minify HTML, JS, CSS, SVG, XML, JSON output
  -buildall      Always rebuild all files on any change (serve mode)
  -exclude <re>  Regex to exclude from watcher (default: "^(node_modules|bower_components)")
  -version       Show version
```

### Common Commands

```bash
# Create new project
sitegen -create

# Development with live reload
sitegen -serve

# Production build
sitegen -clean -minify

# Custom paths
sitegen -site ./my-site -public ./dist -base /blog/

# Dev server on different port
sitegen -serve -port 3000
```

## URL Routing / Path Generation

SiteGen automatically converts source file paths to clean URLs:

| Source File              | Generated URL Path | Output File                    |
|--------------------------|--------------------|--------------------------------|
| `src/index.html`         | `/`                | `public/index.html`            |
| `src/about.html`         | `/about`           | `public/about/index.html`      |
| `src/blog.html`          | `/blog`            | `public/blog/index.html`       |
| `src/blog/post1.html`    | `/blog/post1`      | `public/blog/post1/index.html` |
| `src/css/styles.css`     | `/css/styles.css`  | `public/css/styles.css`        |
| `src/img/logo.svg`       | `/img/logo.svg`    | `public/img/logo.svg`          |

Non-HTML files (CSS, JS, images, etc.) are copied directly to the output path.

## YAML Frontmatter

Source files can include YAML frontmatter between `---` delimiters. For non-HTML files (CSS, JS), wrap the frontmatter in a comment block:

### HTML frontmatter

```html
---
title: My Page Title
description: "Page description for SEO"
date: 2026-01-15
template: main.html
custom_key: any value
---

{{define "content"}}
<h1>{{.Meta.title}}</h1>
{{end}}
```

### CSS/JS frontmatter (inside comments)

```css
/*
---
serve: npm run build:css
build: npm run build:prod:css
---
*/
body { ... }
```

### Special Frontmatter Keys

| Key        | Type   | Description |
|------------|--------|-------------|
| `template` | string | Name of a template file from the templates dir to use as the layout (e.g. `main.html`) |
| `path`     | string | Override the auto-generated URL path (e.g. `404.html` to output at root level) |
| `parse`    | string | Force template parsing for non-HTML files. Values: `text` or `html` |
| `serve`    | string | Shell command to run instead of copying the file (only in `-serve` dev mode) |
| `build`    | string | Shell command to run instead of copying the file (only in production build mode) |

All other frontmatter keys are accessible via `.Meta.<key>` in templates. **Important**: Multi-line string values MUST be quoted in YAML, otherwise parsing will fail.

```yaml
# ✅ Correct
description: "This is a long description
  that spans multiple lines"

# ❌ Wrong — will cause a build error
description: This has a colon: and
  breaks yaml parsing
```

## Template System

SiteGen uses Go's `html/template` engine. Templates in the `templates/` folder are automatically loaded and available to all source files.

### Template Inheritance Pattern

**Base layout** (`templates/main.html`):
```html
<!DOCTYPE html>
<html lang="en">
<head>
    {{ template "head" . }}
</head>
<body>
    {{ template "nav" . }}
    <main>
        {{ template "content" . }}
    </main>
    <footer>
        <p>&copy; {{ .Today }} My Site</p>
    </footer>
</body>
</html>
```

**Page** (`src/about.html`):
```html
---
title: About Us
template: main.html
---

{{define "content"}}
<h1>{{.Meta.title}}</h1>
<p>About page content here.</p>
{{end}}
```

**Partial** (`templates/head.html`):
```html
{{define "head"}}
{{$site := data "site.json"}}
<meta charset="utf-8">
<title>{{if .Meta.title}}{{.Meta.title}} - {{end}}{{$site.title}}</title>
<meta name="description" content="{{if .Meta.description}}{{.Meta.description}}{{else}}{{$site.description}}{{end}}">
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<link rel="stylesheet" href="{{.BasePath}}css/styles.css">
{{end}}
```

### Overridable Template Blocks

Define empty default blocks in templates that pages can optionally override:

```html
<!-- In templates/head.html -->
{{define "addHead"}}{{end}}

<!-- In a page that needs extra head content -->
{{define "addHead"}}
<link rel="stylesheet" href="{{.BasePath}}css/special.css">
{{end}}
```

### Page Variables

All these variables are available in every template:

| Variable     | Type              | Description |
|--------------|-------------------|-------------|
| `.Meta`      | `map[string]any`  | All YAML frontmatter key-value pairs |
| `.Meta.title`| `any`             | Example: accessing the `title` frontmatter key |
| `.Dev`       | `bool`            | `true` when running with `-serve` |
| `.Source`    | `*Source`          | Current source object (has `.Name`, `.Local`, `.Path`, `.Ext`, `.CurrentPage`, `.TotalPages`) |
| `.BasePath`  | `string`          | Configured base path (default `"/"`) |
| `.Today`     | `string`          | Current date as `YYYY-MM-DD` |
| `.Path`      | `string`          | Current page path (for parameterized/paginated pages) |
| `.Page`      | `int`             | Current page number (pagination, 1-indexed) |
| `.Pages`     | `int`             | Total number of pages (pagination) |

## Template Functions Reference

### `data "filename.json"`
Loads and parses a JSON file from the `data/` directory. Returns the parsed JSON as Go types (`map[string]interface{}` for objects, `[]interface{}` for arrays).

```html
{{ $site := data "site.json" }}
<title>{{ $site.title }}</title>

{{ $links := data "links.json" }}
{{ range $links }}
<a href="{{.path}}">{{.name}}</a>
{{ end }}
```

### `sources "property" "glob-pattern"`
Returns a list of `*Source` objects matching the glob pattern against the given property. Uses the [gobwas/glob](https://github.com/gobwas/glob) library for pattern matching.

**Matchable properties**: `Path`, `Local`, `Filename`, `RelPath`, `Ext`, `Meta.<key>`

```html
<!-- All blog posts -->
{{ range sources "RelPath" "blog/*.html" }}
<a href="{{.Path}}">{{.Meta.title}}</a>
{{ end }}

<!-- All images -->
{{ range sources "Path" "/img/*" }}
<img src="{{.Path}}" />
{{ end }}

<!-- All HTML sources (recursive) -->
{{ range sources "Local" "**/src/**.html" }}
<url><loc>{{.Path}}</loc></url>
{{ end }}

<!-- Find by title -->
{{ range sources "Meta.title" "Welcome*" }}
...
{{ end }}
```

### `sort "property" "order" list`
Sorts a slice by a property. Order is `"asc"` or `"desc"`. Works with `*Source` slices, JSON data arrays, and `kv` slices.

```html
<!-- Sort blog posts by date, newest first -->
{{ $posts := sort "Meta.date" "desc" (sources "RelPath" "blog/*.html") }}

<!-- Sort JSON data -->
{{ range sort "name" "asc" (data "links.json") }}
<a href="{{.path}}">{{.name}}</a>
{{ end }}
```

### `limit n list`
Returns the first `n` items from a slice.

```html
{{ range limit 5 (sort "Meta.date" "desc" (sources "RelPath" "blog/*.html")) }}
<!-- Latest 5 posts -->
{{ end }}
```

### `offset n list`
Skips the first `n` items from a slice.

```html
{{ range offset 3 (data "links.json") }}
<!-- Skip first 3 items -->
{{ end }}
```

### `paginate n list`
Automatically paginates a list into pages of `n` items. This function:
1. Sets `.Page` (current page number, 1-indexed)
2. Sets `.Pages` (total page count)
3. Auto-generates additional page files (`/2`, `/3`, etc.)
4. Returns only the items for the current page

```html
---
title: Blog
template: main.html
---

{{define "content"}}
{{ $posts := sort "Meta.date" "desc" (sources "RelPath" "blog/*.html") }}
{{ $paginated := paginate 2 $posts }}

{{ range $paginated }}
<div class="card">
    <h3><a href="{{.Path}}">{{.Meta.title}}</a></h3>
    <p>{{.Meta.summary}}</p>
</div>
{{ end }}

<!-- Pagination links -->
{{ if gt .Source.TotalPages 1 }}
<div class="pagination">
    {{ range pages .Source }}
    <a href="{{.Path}}" class="{{if .Active}}active{{end}}">{{.Page}}</a>
    {{ end }}
</div>
{{ end }}
{{end}}
```

This generates:
- `/blog/index.html` — page 1
- `/blog/2/index.html` — page 2
- `/blog/3/index.html` — page 3
- etc.

### `pages source`
Returns a `[]Page` slice for building pagination links. Each `Page` has `.Path`, `.Page` (number), and `.Active` (bool). Only returns pages when `TotalPages > 1`.

```html
{{ range pages .Source }}
<a href="{{.Path}}" class="{{if .Active}}active{{end}}">{{.Page}}</a>
{{ end }}
```

### `page "source-path" "slug"`
Creates a dynamically generated page from a source template. Used for parameterized pages (detail pages from a list). Returns the generated page's path.

```html
<!-- Generate individual pages from data -->
{{ range data "products.json" }}
<a href="{{ page "product-detail.html" .slug }}">{{ .name }}</a>
{{ end }}
```

The source file `product-detail.html` receives `.Path` as the slug and can access data accordingly.

### `path "url-path"`
Prefixes a path with the configured base path (`-base` flag). Use this for absolute links that respect the base URL.

```html
<a href="{{ path "/about" }}">About</a>
<!-- With -base /blog/ this outputs: /blog/about -->
```

### `json value`
Serializes any value to a JSON string (safe for embedding in `<script>` tags).

```html
<script>
var config = {{ json .Meta }};
</script>
```

### `js string`
Marks a string as safe JavaScript (bypasses escaping in templates).

```html
<script>{{ js "alert('hello')" }}</script>
```

### `html string`
Marks a string as safe HTML (bypasses escaping in templates).

```html
{{ html "<strong>Bold text</strong>" }}
```

### `css string`
Marks a string as safe CSS (bypasses escaping in templates).

```html
<style>{{ css "body { color: red; }" }}</style>
```

### `contains substring string`
Returns `true` if `string` contains `substring`. Note: the substring comes first.

```html
{{ if .Path | contains "blog" }}
<!-- We're in the blog section -->
{{ end }}
```

### `filter "property" "regex-pattern" list`
Filters a slice, keeping only items where the property matches the regex pattern.

```html
{{ range filter "Page" "^[A-Za-z]+$" (data "items.json") }}
<!-- Only items where Page matches the regex -->
{{ end }}
```

### `select map`
Converts a `map[string]interface{}` into a sortable `[]kv` slice where each item has `.Key` and `.Value`.

```html
{{ $site := data "site.json" }}
{{ range select $site }}
<p>{{.Key}}: {{.Value}}</p>
{{ end }}
```

## Non-HTML Template Parsing

XML, TXT, and other text files can use Go's `text/template` engine by setting `parse: text` in frontmatter. This is useful for generating sitemaps, RSS feeds, robots.txt, etc.

```xml
---
parse: text
---
{{- $site := data "site.json" -}}
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
{{range sources "Local" "**/src/**.html"}}
{{- if not (.Local | contains "404.html") -}}
  <url>
    <loc>{{$site.url}}{{.Path}}</loc>
    <lastmod>{{$.Today}}</lastmod>
  </url>
{{- end -}}
{{- end -}}
</urlset>
```

## File Handlers (serve/build)

For non-template files like CSS or JS, you can specify shell commands to run instead of the default copy behavior. This is useful for integrating preprocessors or bundlers.

- `serve`: Runs during `-serve` dev mode only
- `build`: Runs during production build (without `-serve`) only

```css
/*
---
serve: npx tailwindcss -i ./site/src/css/input.css -o ./public/css/styles.css --watch
build: npx tailwindcss -i ./site/src/css/input.css -o ./public/css/styles.css --minify
---
*/
```

## Custom 404 Pages

Create `src/404.html` with `path: 404.html` in frontmatter. The dev server will automatically serve this for unknown routes.

```html
---
title: Page Not Found
path: 404.html
---
<h1>404 - Not Found</h1>
```

## Dev Server Features

When running with `-serve`:
- **Live reload**: Browser auto-refreshes on file changes via Server-Sent Events (SSE)
- **Hot reload script**: Automatically injected before `</body>` in HTML responses
- **404 fallback**: Serves `404.html` for missing routes
- **File watching**: Watches the site directory for changes (excludes `node_modules`, `bower_components` by default)
- **Port auto-detection**: If the default port is busy, automatically finds the next available port
- **Interactive TUI**: Shows build stats, server info, recent activity, and errors

## Minification

With `-minify` flag, SiteGen minifies:
- HTML (preserves document tags)
- CSS
- JavaScript
- SVG
- XML
- JSON

## Tips and Patterns

### Conditional Dev-Only Content
```html
{{ if .Dev }}
<script src="/debug.js"></script>
{{ end }}
```

### Active Navigation Link
```html
{{ range $links }}
<a href="{{.path | path}}" class="{{if eq $.Path .path}}active{{end}}">{{.name}}</a>
{{ end }}
```

### Chaining Functions (Pipe Syntax)
Go templates support pipe syntax for chaining:
```html
<!-- These are equivalent -->
{{ range sort "name" "desc" (limit 3 (data "links.json")) }}
{{ range data "links.json" | limit 3 | sort "name" "desc" }}
```

### Building a Sitemap
See the non-HTML template parsing section above for a complete sitemap.xml example.

### SEO Meta Tags Pattern
```html
{{define "head"}}
{{$site := data "site.json"}}
<title>{{if .Meta.title}}{{.Meta.title}} - {{end}}{{$site.title}}</title>
<meta name="description" content="{{if .Meta.description}}{{.Meta.description}}{{else}}{{$site.description}}{{end}}">
{{end}}
```

### Full Pagination Example

`src/blog.html`:
```html
---
title: Blog
template: main.html
---

{{define "content"}}
<h1>Blog</h1>
{{ $posts := sort "Meta.date" "desc" (sources "RelPath" "blog/*.html") }}
{{ $paginated := paginate 2 $posts }}

<div class="grid">
    {{ range $paginated }}
    <div class="card">
        <h3><a href="{{.Path}}">{{.Meta.title}}</a></h3>
        <p class="date">{{.Meta.date}}</p>
        <p>{{.Meta.summary}}</p>
    </div>
    {{ end }}
</div>

{{ if gt .Source.TotalPages 1 }}
<div class="pagination">
    {{ range pages .Source }}
    <a href="{{.Path}}" class="{{if .Active}}active{{end}}">{{.Page}}</a>
    {{ end }}
</div>
{{ end }}
{{end}}
```

Blog post (`src/blog/my-post.html`):
```html
---
title: My Blog Post
date: 2026-01-15
summary: "A short summary of this post."
template: main.html
---

{{define "content"}}
<h1>{{.Meta.title}}</h1>
<p>Published on {{.Meta.date}}</p>
<p>Post content goes here...</p>
<a href="{{.BasePath}}blog">← Back to Blog</a>
{{end}}
```
