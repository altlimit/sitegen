---
title: GitHub-flavored Markdown, and literal template examples
date: 2026-07-02
summary: Tables, task lists and heading anchors in Markdown pages, and {{ }} in a Markdown body is now shown as written.
template: post.html
---

Markdown pages now use GitHub-flavored Markdown. You get:

| Feature | Example |
|---|---|
| Tables | This one |
| Task lists | `- [x] done` |
| Strikethrough | ~~like this~~ |
| Autolinks | https://sitegen.dev |

Every heading also gets an `id`, so you can link straight to a section, and raw HTML passes through untouched when Markdown isn't enough.

## Template syntax stays literal

Writing about templates in Markdown used to be awkward: any `{{ }}` in the text was run as template code. Now a Markdown body is treated as content by default, so this shows exactly as typed:

```html
{{ range sources "RelPath" "blog/*" }}
  <a href="{{.Path}}">{{ .Meta.title }}</a>
{{ end }}
```

To run template code in a Markdown page, add `templated: true` to its frontmatter. The [docs](/docs/content) are written this way, with every example shown as it is.
