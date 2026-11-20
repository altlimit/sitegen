---
title: Publish with one command
date: 2026-11-19
summary: sitegen -publish builds your site and puts it live on altengine static hosting. It signs you in the first time and uploads only what changed.
template: post.html
---

Getting a site online used to mean a build, then a separate deploy tool. Now it's one command:

```bash
sitegen -publish
```

SiteGen builds the site for production, the same as `-clean -minify`, and publishes it to [altengine](https://www.altengine.net/docs/static/) static hosting. When it's done it prints the live URL.

## No key to copy

The first time, your browser opens to sign in to altengine. Click Authorize and SiteGen creates the site and publishes it. The key it gets is kept in your OS keyring, not in your project. It can create and deploy SiteGen sites and nothing else in your organization, so one sign-in covers every site on that machine.

## Only what changed

Every file is hashed, and altengine says which ones it doesn't have yet. Change one page and one file is uploaded. Publish with no changes and nothing is.

## Teammates and CI

The first publish writes `site/publish.json` with the site's name. Commit it, and everyone who publishes the project updates the same site. In CI, set `ALTENGINE_API_KEY` and SiteGen uses it without prompting:

```yaml
      - run: ./sitegen -publish -publish-message "${{ github.sha }}"
        env:
          ALTENGINE_API_KEY: ${{ secrets.ALTENGINE_API_KEY }}
```

altengine gives each site a random subdomain. You can change it, or add your own domain, in the site's settings. See [Deploying](/docs/deploy#altengine) for the details.
