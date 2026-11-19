---
title: Deploying
description: Build for production and publish to any static host.
section: Guides
order: 80
template: docs.html
---

## Build

```bash
sitegen -clean -minify
```

The finished site is in `public/`. It's plain files, so any static host can serve it.

## Serving from a subfolder

If the site won't live at the root of its domain, as with a GitHub Pages project site at `user.github.io/project/`, build with a base path:

```bash
sitegen -clean -minify -base /project
```

SiteGen writes the site to `public/project/` and prefixes every source URL and `path` result with `/project/`. Links inside Markdown bodies are written as they are, so use relative links there or keep to the site root.

## Not-found page

The starter site's `src/404.html` sets `path: 404.html`, so it's written to `public/404.html`. GitHub Pages, Cloudflare and Netlify all serve that file for missing URLs.

## GitHub Pages

This workflow builds the site in `site/` on every push to `main` and publishes it:

```yaml
name: Deploy site
on:
  push:
    branches: [main]
permissions:
  contents: read
  pages: write
  id-token: write
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/checkout@v4
      - name: Install SiteGen
        run: |
          curl -fsSL -o sitegen https://github.com/altlimit/sitegen/releases/latest/download/sitegen_linux_amd64
          chmod +x sitegen
      - run: ./sitegen -clean -minify -base /${{ github.event.repository.name }}
      - uses: actions/upload-pages-artifact@v3
        with:
          path: public/${{ github.event.repository.name }}
      - id: deployment
        uses: actions/deploy-pages@v4
```

With a custom domain, drop the `-base` flag and upload `public`.

## Cloudflare

Serve `public/` as a Worker with static assets. Create `wrangler.jsonc` next to your `site/` folder:

```jsonc
{
  "name": "my-site",
  "compatibility_date": "2026-11-01",
  "assets": {
    "directory": "./public",
    "not_found_handling": "404-page"
  }
}
```

Then build and deploy:

```bash
sitegen -clean -minify
npx wrangler deploy
```

## Netlify, Vercel and others

Set the build command to install SiteGen and run `sitegen -clean -minify`, and the publish directory to `public`.
