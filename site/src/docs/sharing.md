---
title: Share previews
description: Give your dev server a public HTTPS link, with live reload for everyone viewing it.
section: Guides
order: 70
template: docs.html
---

Add `-share` to the dev server to get a public link to your site, for showing work in progress to a client or a teammate:

```bash
sitegen -serve -share
```

The terminal shows a URL like `https://k3x9q2m7a4bz.sitegen.dev`. Anyone with the link sees your local site, and their browser reloads when you save, just like yours.

## Passwords

To keep a preview private, require a username and password:

```bash
sitegen -serve -share -share-auth "client:preview-pass"
```

Visitors get a browser login prompt before any page loads.

## What visitors can reach

- Only the built files in `public/`, read-only. Requests other than GET and HEAD are refused.
- Not the [CMS](/docs/cms) or its API, which stay on your machine.
- Live-reload notifications aren't password-protected. They only ever contain the word "updated".

## How it works

`-share` opens one secure WebSocket from your machine to a relay on Cloudflare's network, so it works behind firewalls and NAT without opening ports. Each preview runs in its own Durable Object, which costs nothing while nobody is visiting.

- **Stable links.** If your connection drops, SiteGen reconnects and gets the same URL back. The link is released when you quit.
- **Upgrades.** If the relay needs a newer version of SiteGen, the terminal tells you to upgrade.

## Limits

Each preview allows:

| Limit | Value |
|---|---|
| Requests | 50 per second, bursts of 100 |
| Requests in progress | 64 |
| Response size | 50 MB |
| Time for your site to start responding | 30 seconds |
| Live-reload viewers | 100 |

## Running your own relay

The relay is open source, in the [worker/](https://github.com/altlimit/sitegen/tree/master/worker) folder of the repository. Deploy it to your own Cloudflare account and point SiteGen at it:

```bash
sitegen -serve -share -share-server wss://relay.example.com
```
