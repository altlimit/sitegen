# Built-in CMS

SiteGen ships with an optional editing UI for non-technical editors. It reads
and writes the **same source files** the generator consumes (`src/*` and
`data/*.json`), so the filesystem stays the single source of truth — git
history, frontmatter, templates, and the share tunnel all keep working. There is
no database and no separate content store.

```bash
sitegen -serve -cms                        # editor at http://localhost:8888/__cms
sitegen -serve -cms -cms-auth "me:secret"  # protect it with basic auth
```

The CMS is a **dev/serve-mode** feature: it is mounted at `/__cms` inside the
dev server, next to live-reload. A save writes the file → the file watcher
rebuilds → the live preview refreshes.

## How it works

- **Raw mode (no config).** With no `site/cms.yaml`, the editor lists the text
  files under `src/` and lets you edit their frontmatter (as YAML) and body. It
  always works as a fallback, even for files a schema doesn't describe.
- **Typed mode (`site/cms.yaml`).** A config file turns the editor into typed
  forms: a block builder for layout pages, folder collections with a create
  form, editable data files, and image uploads.

Everything is **progressive** — config only adds capability; removing
`cms.yaml` leaves the site building identically.

## `site/cms.yaml` reference

`cms.yaml` has four optional top-level keys: `media_folder`, `blocks`,
`collections`, and `data`.

```yaml
# Where image uploads are saved, relative to the source dir (default: img).
media_folder: img

# Reusable, typed content blocks for layout pages (see "Block pages" below).
blocks:
  - type: hero            # matches the block's `type` in frontmatter
    label: Hero           # shown in the editor (optional)
    fields:
      - { name: heading, widget: string }
      - { name: text,    widget: text }
      - { name: button_label, widget: string }
      - { name: button_link,  widget: string }
  - type: card_grid
    label: Card Grid
    fields:
      - name: cards
        widget: list      # a repeatable list of items...
        fields:           # ...each item has these fields
          - { name: icon,    widget: string }
          - { name: heading, widget: string }
          - { name: text,    widget: text }
  - type: image
    label: Image
    fields:
      - { name: src, widget: image }
      - { name: alt, widget: string }

# Folder collections: many entries = files under src/<folder>.
collections:
  - name: blog            # internal name
    label: Blog Post      # shown on the "+ New" button
    folder: blog          # src/blog/
    extension: md         # new entries are .md
    slug: "{{title}}"     # filename derived from this field, slugified
    fields:
      - { name: title,    widget: string }
      - { name: date,     widget: date }
      - { name: summary,  widget: text }
      - { name: template, widget: hidden, default: main.html }
      - { name: body,     widget: markdown }   # the body, not frontmatter

# Data files: typed editing of data/*.json.
data:
  - name: site
    label: Site Settings
    file: site.json       # under data/
    fields:
      - { name: title,       widget: string }
      - { name: description, widget: text }
      - { name: url,         widget: string }
  - name: nav
    label: Navigation
    file: links.json
    list: true            # the file is a JSON array of items
    fields:
      - { name: name, widget: string }
      - { name: path, widget: string }
```

### Widgets

| Widget     | Editor control                                  |
|------------|-------------------------------------------------|
| `string`   | single-line text input                          |
| `text`     | multi-line textarea                             |
| `markdown` | body editor (collections only — the file body)  |
| `date`     | native date picker (`YYYY-MM-DD`)               |
| `datetime` | native datetime picker                          |
| `boolean`  | checkbox                                         |
| `image`    | upload/replace/remove with a thumbnail          |
| `list`     | repeatable items (add / remove / reorder)       |
| `hidden`   | not shown; its `default` is written on create   |

If a field has no `widget`, the editor infers one from the existing value
(string vs. multi-line text vs. list/object).

## Block pages

A "block page" stores its layout as a `blocks:` list in frontmatter, and a
template renders each block by `type`. The editor shows a **block builder**
(add / reorder / delete, typed fields per block) instead of raw HTML — so an
editor can compose a page without seeing markup.

`src/index.html` frontmatter:

```yaml
---
template: page.html
blocks:
  - type: hero
    heading: "Static Sites, Simply."
    text: "A fast, flexible, zero-dependency static site generator."
    button_label: "Explore Features"
    button_link: "features"
  - type: card_grid
    cards:
      - { icon: "🚀", heading: "Fast", text: "Builds only what's needed." }
---
```

`templates/page.html` dispatches on `.type`:

```html
{{define "content"}}
{{- range .blocks }}
  {{- if eq .type "hero" }}
    <div class="hero"><h1>{{ .heading }}</h1><p>{{ .text }}</p></div>
  {{- else if eq .type "card_grid" }}
    <div class="grid">{{ range .cards }}<div class="card"><h3>{{ .icon }} {{ .heading }}</h3><p>{{ .text }}</p></div>{{ end }}</div>
  {{- end }}
{{- end }}
{{end}}
```

The block builder requires no engine changes — `blocks:` is ordinary
frontmatter and the dispatch lives in your template. Add a block type to
`cms.yaml` *and* a branch to your template to support it.

## New entries

For each `collection`, the sidebar shows a **"+ New"** button that opens a typed
create form. On save it derives a slug from the `slug` field (default `title`),
writes `src/<folder>/<slug>.<ext>`, and refuses to overwrite an existing entry.
Because listing pages are dependency-tracked, a new post appears in its index
(e.g. a blog listing) without a manual reload — as long as the listing uses the
`sources` template func over the collection's folder.

> Tip: for new markdown entries to show in an existing listing, make the listing
> glob the folder (`sources "RelPath" "blog/*"`) rather than a single extension.

## Data files

`data:` entries map a `data/*.json` file to a form. A single JSON object renders
as a field form; a JSON **array** (`list: true`) renders as a reorderable list
of items. Saves preserve the schema's key order for clean diffs. Keyed-object
data files (e.g. `{ "post1": {...} }`) aren't directly editable — restructure
them as an array to edit them in the CMS.

## Images

The `image` widget uploads a file via `POST /__cms/api/upload`. The image is
saved into the media folder (`media_folder`, default `src/img/`) with a
sanitized, unique filename, and the field stores its site-root URL (e.g.
`/img/photo.png`). The normal build pipeline (and `-webp`, if enabled) processes
it on the next rebuild.

## Editor tips

- **Blocks / Raw toggle** — every page can also be edited as raw frontmatter +
  body. The raw view re-syncs from disk after a block save.
- **Live preview** — follows the page you're editing, has a reload button and an
  editable URL bar, and can be hidden for a full-width editor (with an
  "Open ↗" button to pop the preview into a new tab).

## Security

The CMS reads and writes only within `src/` and `data/` (path traversal is
blocked) and is intended for **local/trusted** use during `-serve`. Use
`-cms-auth user:pass` for basic auth if you expose it (e.g. over the share
tunnel). It is not a multi-tenant, hardened hosting product.
