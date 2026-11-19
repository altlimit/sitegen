---
title: Sitemaps with real last-modified dates
date: 2026-07-01
summary: Every source now has a LastMod date, so a generated sitemap tells search engines which pages actually changed.
template: post.html
---

A sitemap is most useful when its `<lastmod>` dates are accurate: search engines use them to decide what to crawl again. SiteGen sources now have a `LastMod` value you can use for that.

`LastMod` is the page's `updated:` frontmatter date if it has one, and otherwise the file's modification date. Pinning `updated:` is useful in CI, because a fresh `git clone` gives every file the same modification time.

```yaml
---
title: Pricing
updated: 2026-07-01
---
```

The starter site's `sitemap.xml` is a text template that lists every HTML and Markdown page, so new pages appear in it without any editing:

```xml
{{ range sources "Local" "**/src/**.{html,md}" }}
  <url>
    <loc>{{ $site.url }}{{.Path}}</loc>
    <lastmod>{{.LastMod}}</lastmod>
  </url>
{{ end }}
```

Leave a page out with `sitemap: false` in its frontmatter.
