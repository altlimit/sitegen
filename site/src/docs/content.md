---
title: Pages and content
description: How files become URLs, and how to use frontmatter and Markdown.
section: Basics
order: 20
template: docs.html
---

## Files become URLs

Everything in `src/` is built into `public/`. HTML and Markdown pages get clean URLs, and other files are copied as they are.

| Source file | URL |
|---|---|
| `src/index.html` | `/` |
| `src/about.html` | `/about` |
| `src/blog/hello.md` | `/blog/hello` |
| `src/docs/index.md` | `/docs/` |
| `src/css/styles.css` | `/css/styles.css` |

A page at `/about` is written to `public/about/index.html`, so it works on any static host. To choose a URL yourself, set `path` in the frontmatter. The 404 page uses `path: 404.html` so hosts can find it.

## Frontmatter

Frontmatter is YAML between two `---` lines at the top of a file. In the page's own template, each key is available at the top level:

```html
---
title: About us
description: Who we are.
template: main.html
---

{{ define "content" }}
  <h1>{{ .title }}</h1>
  <p>{{ .description }}</p>
{{ end }}
```

The same values are also in `.Meta`, which is how you read them from other pages, for example when listing blog posts with `sources`.

### Keys SiteGen uses

| Key | What it does |
|---|---|
| `template` | The layout from `templates/` to render the page with. |
| `path` | Overrides the page's URL, for example `404.html`. |
| `parse` | Renders a non-HTML file as a template: `text`, `html` or `markdown`. |
| `templated` | `true` runs template code inside a Markdown body. |
| `block` | The block a Markdown body fills. Default `content`. |
| `serve`, `build` | A command to run for this file instead of copying it. See [Assets](/docs/assets). |
| `updated` | The page's last-modified date, `YYYY-MM-DD`. Defaults to the file's modification time. |
| `sitemap` | `false` leaves the page out of the starter `sitemap.xml`. |

Any other key is yours to use. This site uses `section` and `order` to build the docs sidebar.

## HTML pages

An HTML page picks a layout with `template` and fills its blocks with `define`:

```html
---
title: Pricing
template: main.html
---

{{ define "content" }}
  <h1>{{ .title }}</h1>
{{ end }}
```

Everything outside `define` blocks is ignored when a layout is used, so keep page content inside them. A page without `template` is rendered on its own, as a complete HTML document.

## Markdown pages

A `.md` file is converted to HTML and placed in the `content` block of its layout. It supports GitHub-flavored Markdown:

- tables, strikethrough, task lists and autolinks
- an `id` on every heading, so `#links` work
- raw HTML, passed through unchanged

| Feature | Supported |
|---|---|
| Tables | ✓ |
| Task lists | ✓ |
| Fenced code | ✓ |

- [x] Write the docs
- [ ] ~~Skip the examples~~

### Template code in Markdown

A Markdown body is treated as content, so `{{ }}` in it is shown as written. That's what lets these docs show template examples.

To run template code in a Markdown page, add `templated: true` to its frontmatter.

## Text files

`.txt` files are rendered as text templates. To render other files, such as XML or JSON, add `parse: text`:

```xml
---
parse: text
---
{{- $site := data "site.json" -}}
<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>{{ $site.title }}</title>
</feed>
```

This site builds `sitemap.xml`, `feed.xml` and `robots.txt` this way, so they list new pages and posts without being edited.
