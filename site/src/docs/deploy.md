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

## altengine

```bash
sitegen -publish
```

This builds the site as `-clean -minify` and publishes it to [altengine](https://www.altengine.net/docs/static/) static hosting. The first run opens your browser to sign in to altengine, creates the site, and writes its name to `site/publish.json`. Commit that file so everyone publishes to the same site. Each run uploads only the files that changed and prints the live URL:

```text
Published 42 files (1 uploaded) → https://k3x9q2-web.altengine.app/
```

The site is the altengine instance `sitegen-<name>`, where `<name>` is your project folder's name unless you pass `-publish-name`. If a site of that name already exists and this project hasn't published to it before, sitegen stops instead of overwriting it; pass `-publish-name` to confirm it's yours. altengine gives the site a random subdomain, which you can change in the site's settings in the altengine console, or add a custom domain there.

On a machine without a browser, add `-publish-device` to sign in with a code instead. `-publish-message "v1.2"` labels the deployment.

In CI, set `ALTENGINE_API_KEY` to an altengine API key that can deploy the site. sitegen then never prompts and stores nothing:

```yaml
      - run: ./sitegen -publish -publish-message "${{ github.sha }}"
        env:
          ALTENGINE_API_KEY: ${{ secrets.ALTENGINE_API_KEY }}
```

The key sitegen gets when you sign in can create and deploy `sitegen-*` sites and nothing else, so you sign in once per machine for all your sites. It's kept in your OS keyring, never in your project. `sitegen -publish-signout` revokes it.

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
