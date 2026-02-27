# SiteGen

[![Go Report Card](https://goreportcard.com/badge/github.com/altlimit/sitegen)](https://goreportcard.com/report/github.com/altlimit/sitegen)
[![Latest Release](https://img.shields.io/github/v/release/altlimit/sitegen)](https://github.com/altlimit/sitegen/releases)
[![License](https://img.shields.io/github/license/altlimit/sitegen)](LICENSE)

Sitegen is a simple, flexible, and fast static site generator written in Go. It supports incremental builds, live reloading, and a powerful template system.

## Features

- 🚀 **Fast & Incremental**: Builds only what's needed.
- 🔄 **Live Reload**: Built-in development server with changes detection.
- 🎨 **Templating**: Flexible Go templates with custom functions.
- 📦 **Zero Dependency**: Single binary, easy to install.
- 🔧 **File Handlers**: Custom build commands for specific file types (e.g. CSS, JS).

## Installation

### Unix/Linux/macOS

```bash
curl -s -S -L https://raw.githubusercontent.com/altlimit/sitegen/master/install.sh | bash
```

### Windows

Download the latest release from the [Releases Page](https://github.com/altlimit/sitegen/releases).

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
  -create       Create a new site template
  -site <path>  Root site path (default: "./site")
  -serve        Start development server
  -port <port>  Port for development server (default: "8888")
  -clean        Clean public dir before build
  -minify       Minify HTML/JS/CSS output
  -public <dir> Public output directory (default: "./public")
  -base <path>  Base URL path (default: "/")
  -help         Show help
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

## Contributing

Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

## License

[MIT](LICENSE.txt)