---
title: Getting started
description: Install SiteGen, create a site and run the dev server.
section: Basics
order: 10
path: /docs
template: docs.html
---

SiteGen turns a folder of HTML, Markdown and JSON into a static website. It's one binary with no runtime to install, and it includes a dev server with live reload, a content editor and public preview links.

## Install

The easiest way to install and update SiteGen is [alt](https://github.com/altlimit/alt), which installs command-line tools from GitHub Releases.

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/altlimit/alt/main/scripts/install.sh | sh
alt install altlimit/sitegen
```

Windows (PowerShell):

```powershell
powershell -Command "iwr https://raw.githubusercontent.com/altlimit/alt/main/scripts/install.ps1 -useb | iex"
alt install altlimit/sitegen
```

You can also download a binary from the [releases page](https://github.com/altlimit/sitegen/releases) and put it on your `PATH`. Run `sitegen -version` to check it works.

## Create a site

```bash
mkdir my-site && cd my-site
sitegen -create
```

This writes a starter site into `./site`. It's a copy of the site you're reading: a home page built from blocks, these docs, a paginated blog, an Atom feed, a sitemap and a CMS config. Keep what's useful and delete the rest.

## Run the dev server

```bash
sitegen -serve
```

Open [localhost:8888](http://localhost:8888). SiteGen builds the site into `./public` and serves it. When you save a file it rebuilds what changed and reloads the browser.

The terminal shows the local URL and any build errors. Press `r` for a full rebuild and `q` to quit.

## Build for production

```bash
sitegen -clean -minify
```

`-clean` empties `public/` before building, and `-minify` minifies HTML, CSS and JavaScript. Upload the `public/` folder to any static host. See [Deploying](/docs/deploy).

Or build and publish in one step to altengine static hosting:

```bash
sitegen -publish
```

The first run signs you in to altengine in the browser and creates the site. Later runs upload only what changed and print the live URL. See [Deploying](/docs/deploy#altengine).

## Project layout

```text
my-site/
├── site/
│   ├── src/          # pages and assets: each file becomes a URL
│   ├── templates/    # layouts and partials shared by pages
│   ├── data/         # JSON files, read with the data function
│   └── cms.yaml      # optional schema for the built-in CMS
└── public/           # build output, safe to delete
```

Every folder can be moved with a flag, such as `-site`, `-public` or `-templates`. See the [CLI reference](/docs/cli).

## Your first page

Create `site/src/hello.md`:

```markdown
---
title: Hello
template: main.html
---

# Hello, world

This page is served at **/hello**.
```

Save it and open [localhost:8888/hello](http://localhost:8888/hello). The `template: main.html` line wraps the page in the layout from `site/templates/main.html`, so it gets the same header and footer as the rest of the site.

## Next steps

- [Pages and content](/docs/content): how files map to URLs, frontmatter and Markdown.
- [Templates](/docs/templates): layouts, partials and page variables.
- [Data and lists](/docs/data): JSON data, listing pages, pagination and generated pages.
