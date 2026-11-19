---
title: Write pages in Markdown
date: 2026-03-01
summary: Drop a .md file into src/ and it becomes a page, wrapped in the layout you choose.
template: post.html
---

Not every page needs HTML. SiteGen now builds `.md` files too: `src/about.md` becomes `/about`, converted to HTML and placed in your layout's `content` block.

```markdown
---
title: About
template: main.html
---

# About us

We build **fast** websites.
```

Frontmatter works exactly as it does for HTML pages, so `title`, `template` and your own keys are all available to the layout. To fill a block other than `content`, set `block:` in the frontmatter.

This release also adds `.BuildID`, a value that changes on every build. Add it to your stylesheet and script URLs and browsers will fetch fresh copies after each deploy:

```html
<link rel="stylesheet" href="/css/styles.css?v={{ .BuildID }}">
```
