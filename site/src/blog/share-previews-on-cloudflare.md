---
title: Share previews now run on Cloudflare
date: 2026-11-19
summary: "-share moved from a single server to Cloudflare's network. Links survive reconnects, and you'll need SiteGen v1.0.39 or later."
template: post.html
---

`sitegen -serve -share` gives your dev server a public link, so a client or teammate can watch a site take shape. Until now those links went through one server we ran ourselves. If it went down, nobody could share.

Starting with **v1.0.39**, previews run on Cloudflare's network instead. Your machine keeps one secure WebSocket open to the relay, and every preview gets its own Durable Object, which sleeps while nobody is looking and costs nothing.

## What's better

- **Links survive reconnects.** If your laptop sleeps or the Wi-Fi drops, SiteGen reconnects and gets the same URL back, so the link you sent still works.
- **Live reload for viewers.** People viewing your preview see changes as soon as you save, without refreshing.
- **The CMS stays private.** The `-cms` editor is never reachable through a share link, even without `-cms-auth`.
- **You can run your own.** The relay is open source in the [worker/](https://github.com/altlimit/sitegen/tree/master/worker) folder, and `-share-server` points SiteGen at your copy.

## Upgrade to keep sharing

Older versions of SiteGen spoke a protocol the new relay doesn't understand, and the old server has been retired. To keep using `-share`, install the latest release:

```bash
alt install altlimit/sitegen
```

Or download it from the [releases page](https://github.com/altlimit/sitegen/releases). Everything else in SiteGen works as before. [Share previews](/docs/sharing) in the docs has the details and limits.
