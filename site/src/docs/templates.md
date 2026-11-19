---
title: Templates
description: Layouts, partials and the variables every page can use.
section: Basics
order: 30
template: docs.html
---

SiteGen uses Go's [text/template](https://pkg.go.dev/text/template) language, with [extra functions](/docs/functions) for data, lists and pages.

## Layouts

Every file in `templates/` is loaded into one set, so any page or layout can use any template defined there. A layout is a complete HTML document that leaves gaps for the page to fill:

```html
<!-- templates/main.html -->
<!DOCTYPE html>
<html lang="en">
<head>{{ template "head" . }}</head>
<body>
  {{ template "header" . }}
  <main>{{ template "content" . }}</main>
  {{ template "footer" . }}
</body>
</html>
```

A page chooses the layout with `template: main.html` and defines the `content` block. You can use as many layouts as you like. This site has `main.html` for plain pages, `page.html` for block pages, `post.html` for blog posts and `docs.html` for these docs.

## Partials

Define reusable pieces once and call them anywhere:

```html
<!-- templates/partials.html -->
{{ define "header" }}
<header>
  {{ range data "links.json" }}
    <a href="{{ path .path }}">{{ .name }}</a>
  {{ end }}
</header>
{{ end }}
```

Pass a value as the second argument to give the partial a different `.`. This site's icons work that way: `{{ template "icon" "github" }}`.

### Optional blocks

To let pages add to a layout, define an empty block and call it from the layout. Pages that need it define their own version:

```html
<!-- in a layout -->
{{ define "addHead" }}{{ end }}
<head>{{ template "addHead" . }}</head>

<!-- in one page -->
{{ define "addHead" }}<link rel="stylesheet" href="{{ path "/css/charts.css" }}">{{ end }}
```

## Page variables

| Variable | Value |
|---|---|
| `.title`, `.anything` | The page's frontmatter values |
| `.Meta` | The page's frontmatter as a map |
| `.Source` | The current source file (see below) |
| `.Dev` | `true` under `sitegen -serve` |
| `.BasePath` | The `-base` path, `/` by default |
| `.BuildID` | Changes on every build. Add it to asset URLs to bust caches |
| `.Today` | The build date, `YYYY-MM-DD` |
| `.Year` | The build year, `YYYY` |
| `.Path` | In a page made with `page`, the slug it was generated for |

### Source fields

Each source, whether it's `.Source` or one returned by `sources`, has:

| Field | Value |
|---|---|
| `.Path` | Public URL, including the base path |
| `.Name` | File name |
| `.Local` | Absolute path on disk |
| `.Ext` | Extension, such as `.md` |
| `.Meta` | Frontmatter map |
| `.LastMod` | `updated:` from frontmatter, or the file's modification date |
| `.CurrentPage`, `.TotalPages` | Position in a paginated listing |
| `.Value "prop"` | Any property `sources` can match, such as `RelPath` |

## Output isn't escaped

Because SiteGen renders with `text/template`, values are printed exactly as they are. That means you can store HTML in frontmatter or JSON and print it directly. It also means you should only print content you trust, and escape characters such as `<` yourself when you want them shown as text.

## Links and the base path

If the site might be served from a subfolder, write internal links and asset URLs with `path`, which adds the `-base` path:

```html
<link rel="stylesheet" href="{{ path "/css/styles.css" }}?v={{ .BuildID }}">
<a href="{{ path "/docs" }}">Docs</a>
```

Source paths, such as the result of `.Source.Path` or the URLs in a `sources` loop, already include the base path.

## Useful built-ins

Go's template language includes comparisons and helpers you'll use often: `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `and`, `or`, `not`, `len`, `index`, `slice`, `print` and `printf`. Variables are declared with `:=` and reassigned with `=`:

```html
{{ $first := true }}
{{ range .items }}
  {{ if not $first }}, {{ end }}{{ . }}
  {{ $first = false }}
{{ end }}
```

Use `{{-` and `-}}` to trim the whitespace around an action.
