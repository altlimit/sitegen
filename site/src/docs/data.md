---
title: Data and lists
description: JSON data files, listing pages, pagination and pages generated from data.
section: Guides
order: 40
template: docs.html
---

## JSON data

Put JSON files in `data/` and read them with `data`. An object becomes a map, and an array becomes a list you can loop over:

```json
// data/links.json
[
  { "name": "Docs", "path": "/docs" },
  { "name": "Blog", "path": "/blog" }
]
```

```html
<nav>
  {{ range data "links.json" }}
    <a href="{{ path .path }}">{{ .name }}</a>
  {{ end }}
</nav>
```

This site keeps its navigation, footer links, site settings and the [function reference](/docs/functions) in `data/`. The [CMS](/docs/cms) can edit data files as forms.

## Listing pages

`sources` finds files in `src/`, which is how you build blog indexes, docs sidebars and "latest posts" sections. Match on a property with a glob:

```html
{{ $posts := sort "Meta.date" "desc" (sources "RelPath" "blog/*") }}

{{ range limit 3 $posts }}
  <a href="{{.Path}}">
    <h3>{{ .Meta.title }}</h3>
    <p>{{ .Meta.summary }}</p>
  </a>
{{ end }}
```

Properties you can match are `Path`, `Local`, `Filename`, `RelPath`, `Ext` and `Meta.<key>`. Narrow the results with `filter`, and order them with `sort`:

```html
{{ $docs := sources "RelPath" "docs/*" }}
{{ range sort "Meta.order" "asc" (filter "Meta.section" "^Guides$" $docs) }}
  <a href="{{.Path}}">{{ .Meta.title }}</a>
{{ end }}
```

`sort` compares values as text, so give numbers the same number of digits. This site's docs use `order: 10`, `20`, `30` and so on.

### Rebuilds while you work

Under `-serve`, a page that calls `sources` or `data` is marked as a listing. When you add, edit or delete a file, SiteGen rebuilds that file and every listing, so a new blog post appears on the blog index straight away.

## Pagination

`paginate` returns one page of a list and writes the other pages for you. A listing at `/blog` that calls `paginate 4` over ten posts produces `/blog`, `/blog/2` and `/blog/3`:

```html
{{ $posts := sort "Meta.date" "desc" (sources "RelPath" "blog/*") }}

{{ range paginate 4 $posts }}
  <article><a href="{{.Path}}">{{ .Meta.title }}</a></article>
{{ end }}

{{ if gt .Source.TotalPages 1 }}
  <nav>
    Page {{ .Source.CurrentPage }} of {{ .Source.TotalPages }}
    {{ range pages .Source }}
      <a href="{{.Path}}"{{ if .Active }} aria-current="page"{{ end }}>{{ .Page }}</a>
    {{ end }}
  </nav>
{{ end }}
```

Call `paginate` once per page. The [blog](/blog) on this site uses exactly this code.

## Pages generated from data

`page` renders a template file once for each slug you give it, and returns the new page's URL. Use it to create a page for every item in a data file: team members, products, or the [function reference](/docs/functions) on this site.

1. Write the generator, a page that reads its slug from `.Path` and looks up the matching item. Save it as `src/team/member.html`:

   <pre><code>---
   template: main.html
   path: /team
   ---
   {{ define "content" }}
     {{ $slug :=&#32;.Path }}
     {{ range data "team.json" }}
       {{ if eq .slug $slug }}&lt;h1&gt;{{ .name }}&lt;/h1&gt;&lt;p&gt;{{ .bio }}&lt;/p&gt;{{ end }}
     {{ end }}
   {{ end }}</code></pre>

   The `path` key sets the URL prefix, so pages land at `/team/<slug>`.

   SiteGen recognises a generator by <code>.Path</code> written after a space, as in <code>{{&#32;.Path }}</code> or <code>$slug :=&#32;.Path</code>. It skips that file in the normal build and only renders it through `page`. Without the space, as in <code>{{.Path}}</code>, the file is built as an ordinary page.

2. Call `page` from a listing page, which both generates the pages and links to them:

   ```html
   {{ range data "team.json" }}
     <a href="{{ page "team/member.html" .slug }}">{{ .name }}</a>
   {{ end }}
   ```

Generated pages aren't sources, so `sources` won't find them. This site's `sitemap.xml` lists them by looping over the same data file.
