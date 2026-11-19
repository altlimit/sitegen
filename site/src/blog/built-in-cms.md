---
title: Meet the built-in CMS
date: 2026-06-24
summary: Run sitegen -serve -cms and edit pages, blocks, blog posts and data files in the browser. Everything is saved to your source files.
template: post.html
---

Not everyone who edits a website wants to open a code editor. SiteGen now includes a content editor for them:

```bash
sitegen -serve -cms
```

Open `http://localhost:8888/__cms` and you get a list of your pages with a live preview beside the editor. Save, and the page rebuilds and the preview reloads.

## No database

The editor reads and writes the same files the generator uses, `src/*` and `data/*.json`. There's nothing to migrate or back up separately, and every change shows up in `git diff` like any other edit.

## Typed forms when you want them

With no configuration the editor shows each file's frontmatter and body. Add a `cms.yaml` and you get:

- a **block builder** for pages laid out as a list of blocks, like this site's home page
- **collections**, such as a blog, with a "+ New" button and a form for each entry
- **data files** edited as forms or reorderable lists
- **image uploads** with thumbnails

Read the [CMS guide](/docs/cms) to set it up.
