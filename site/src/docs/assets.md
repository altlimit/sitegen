---
title: Assets and images
description: CSS, JavaScript, minification, custom build commands and image optimization.
section: Guides
order: 50
template: docs.html
---

## Static files

Anything in `src/` that isn't a page is copied to `public/` at the same path: stylesheets, scripts, images, fonts and downloads. Reference them with `path` so they work under a base path, and add `hash` to bust browser caches when a file changes:

```html
<link rel="stylesheet" href="{{ path "/css/styles.css" }}?v={{ hash "/css/styles.css" }}">
<script src="{{ path "/js/main.js" }}?v={{ hash "/js/main.js" }}" defer></script>
```

`hash` changes only when that file does. `.BuildID` also works, but it changes on every build, so every page changes too and `sitegen -publish` uploads them all again.

## Minification

With `-minify`, SiteGen minifies every rendered HTML page and every CSS and JavaScript file it copies. Leave it off during development so the output is easy to read.

## Custom build commands

To process a file with your own tools, such as a CSS framework, a bundler or TypeScript, give it frontmatter inside a comment. SiteGen then runs your command instead of copying the file:

```css
/*
---
serve: npm run css:dev
build: npm run css:build
---
*/
```

- `serve` runs under `sitegen -serve`, every time the file changes.
- `build` runs for a normal build.
- Your command writes its own output into `public/`.
- Commands time out after 120 seconds. Change this with `-cmd-timeout`, or set it to `0` for no limit.

The command is split on spaces and run directly, not through a shell, so pipes, `&&` and quoted arguments don't work. Put anything more complex in a script or an npm script and run that.

## Images

JPEG and PNG files are copied unchanged unless you ask for more:

- **`-minify`** scales images wider than 1920 pixels down to 1920.
- **`-webp`** writes a `.webp` copy next to every JPEG and PNG, and wraps each `<img>` that points at one in a `<picture>` element, so browsers that support WebP download the smaller file.

```bash
sitegen -clean -minify -webp
```

SVG, GIF and other formats are always copied as they are.
