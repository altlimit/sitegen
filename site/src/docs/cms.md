---
title: Built-in CMS
description: An editor for pages, blocks, collections and data files that saves to your source files.
section: Guides
order: 60
template: docs.html
---

SiteGen includes an editing UI for people who'd rather not touch code. It reads and writes the same files the generator uses, `src/*` and `data/*.json`, so there's no database and your git history stays the source of truth.

```bash
sitegen -serve -cms                         # editor at http://localhost:8888/__cms
sitegen -serve -cms -cms-auth "me:secret"   # require a password
```

Saving writes the file, the watcher rebuilds it, and the preview next to the editor reloads.

## Without configuration

With no config file, the editor lists the text files in `src/` and lets you edit each one's frontmatter and body. This always works, even for files a config doesn't describe.

## With cms.yaml

Add `site/cms.yaml` to get typed forms. It has four optional sections:

```yaml
media_folder: img       # where uploaded images go, under src/

blocks:                 # building blocks for block pages
  - type: hero
    label: Hero
    fields:
      - { name: heading, widget: string }
      - { name: text, widget: text }

collections:            # folders of entries, with a "+ New" button
  - name: blog
    label: Blog post
    folder: blog
    extension: md
    fields:
      - { name: title, widget: string }
      - { name: date, widget: date }
      - { name: template, widget: hidden, default: post.html }
      - { name: body, widget: markdown }

data:                   # forms for data/*.json
  - name: nav
    label: Navigation
    file: links.json
    list: true
    fields:
      - { name: name, widget: string }
      - { name: path, widget: string }
```

The starter site's `cms.yaml` describes its own home page blocks, blog, docs and data files, so you can try every part of the editor.

### Widgets

| Widget | Control |
|---|---|
| `string` | One-line text |
| `text` | Multi-line text |
| `markdown` | The file body, in collections |
| `date`, `datetime` | Date or date-and-time picker |
| `boolean` | Checkbox |
| `image` | Upload with a thumbnail |
| `list` | Repeatable items that can be reordered |
| `hidden` | Not shown. Its `default` is written when an entry is created |

## Block pages

A block page keeps its layout in a `blocks:` list in frontmatter, and its template renders each block by `type`. The editor shows these as a block builder: add, reorder and remove blocks, and edit each one's fields. This site's home page is a block page. Compare `src/index.html` with `templates/page.html` to see how it works.

To add a block type, describe its fields in `cms.yaml` and add a matching branch to the template.

## Security

The CMS is for local use during `-serve`:

- It only reads and writes inside `src/` and `data/`.
- It's never reachable through a [share link](/docs/sharing). `/__cms` returns 404 there.
- The dev server listens on every network interface, so use `-cms-auth` on a shared network.

The full reference is in [docs/CMS.md](https://github.com/altlimit/sitegen/blob/master/docs/CMS.md) on GitHub.
