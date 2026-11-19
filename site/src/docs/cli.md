---
title: CLI reference
description: Every sitegen command-line flag.
section: Reference
order: 90
template: docs.html
---

```text
sitegen [flags]
```

Run in the folder that contains your `site/` directory. With no flags, SiteGen builds the site once into `public/` and exits.

## Common commands

| Command | What it does |
|---|---|
| `sitegen -create` | Create a starter site in `./site` |
| `sitegen -serve` | Build, serve on port 8888, and rebuild on changes |
| `sitegen -serve -share` | Also get a public preview link |
| `sitegen -serve -cms` | Also open the editor at `/__cms` |
| `sitegen -clean -minify` | Production build |

## Building

| Flag | Default | Description |
|---|---|---|
| `-clean` | off | Empty the public folder before building |
| `-minify` | off | Minify HTML, CSS and JavaScript, and scale large images down |
| `-webp` | off | Write WebP copies of JPEG and PNG images and use them in pages |
| `-base` | `/` | Base URL path, for sites served from a subfolder |
| `-cmd-timeout` | `120` | Seconds a `serve` or `build` command may run. `0` disables the limit |

## Folders

| Flag | Default | Description |
|---|---|---|
| `-site` | `./site` | The site's root folder |
| `-source` | `src` | Pages and assets, inside the site folder |
| `-templates` | `templates` | Layouts and partials, inside the site folder |
| `-data` | `data` | JSON data files, inside the site folder |
| `-public` | `./public` | Where the built site is written |

## Dev server

| Flag | Default | Description |
|---|---|---|
| `-serve` | off | Serve the site with live reload and rebuild on changes |
| `-port` | `8888` | Port for the dev server |
| `-exclude` | `^(node_modules\|bower_components)` | Regular expression of paths the watcher ignores |
| `-buildall` | off | Rebuild the whole site on every change |
| `-share` | off | Publish the dev server at a public `sitegen.dev` URL |
| `-share-auth` | | Require `user:pass` from share visitors |
| `-share-server` | `wss://relay.sitegen.dev` | Share relay to connect to |
| `-cms` | off | Enable the editor at `/__cms` |
| `-cms-auth` | | Require `user:pass` for the editor |

## Other

| Flag | Description |
|---|---|
| `-create` | Create a starter site at `-site` and exit. Fails if the folder exists |
| `-version` | Print the version and exit |
| `-help` | List all flags |

While the dev server is running, press `r` to rebuild everything and `q` to quit.
