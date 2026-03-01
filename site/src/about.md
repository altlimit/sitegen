---
title: About
description: "Learn more about SiteGen and how it works."
template: main.html
---

# About SiteGen

SiteGen is a fast, flexible, and **zero-dependency** static site generator written in Go.

## Features

- **Incremental builds** for rapid development
- **Live reload** during development
- Go **template engine** with custom helpers
- **YAML frontmatter** for metadata
- **Pagination** and dynamic page generation
- **Markdown support** — write pages in `.md` and they are converted to HTML automatically

## Markdown Support

This page is written as a `.md` file demonstrating that markdown content
is automatically converted to HTML and wrapped in the `content` block.

No need to write `&lbrace;&lbrace;define "content"&rbrace;&rbrace;` — it is added automatically for `.md` files.

Current build: **{{ .BuildID }}**
