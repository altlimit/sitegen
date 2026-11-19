# sitegen.dev Workers

This folder holds two Cloudflare Workers:

- **The share relay** ([wrangler.jsonc](wrangler.jsonc)), the public side of `sitegen -serve -share`. Most of this README is about it.
- **The website** ([site.wrangler.jsonc](site.wrangler.jsonc)), an assets-only Worker serving `sitegen.dev` from the build of [../site](../site). See [The website](#the-website).

The relay has two parts:

- **The router** ([src/index.ts](src/index.ts)) accepts the CLI's WebSocket at `relay.<domain>/connect`, and visitor requests at `<tunnel>.<domain>`.
- **The Tunnel Durable Object** ([src/tunnel.ts](src/tunnel.ts)) runs one instance per tunnel. It holds the CLI's socket using the WebSocket Hibernation API, so an idle tunnel is evicted from memory and costs nothing. It forwards each visitor request down that socket.

The wire protocol is in [../docs/SHARE_PROTOCOL.md](../docs/SHARE_PROTOCOL.md).

## Local development

```bash
npm ci
npm run dev                       # relay on http://localhost:8787
# in a site directory, in another terminal:
sitegen -serve -share -share-server ws://relay.localhost:8787
```

The CLI prints a URL such as `http://<tunnel>.localhost:8787/`. Browsers and curl resolve `*.localhost` to your machine.

The `dev` script passes `--infer-origin-from-routes=false`. Without it, wrangler rewrites every local request to the production route's host, and routing breaks.

Tests run inside workerd with `@cloudflare/vitest-plugin`:

```bash
npm test
npm run typecheck
```

## Configuration ([wrangler.jsonc](wrangler.jsonc))

| Setting | Purpose |
|---|---|
| `routes` | `*.sitegen.dev/*`. It covers `relay.sitegen.dev`, every tunnel host, and `www`, which redirects to the apex. |
| `PUBLIC_DOMAIN` | The parent domain for tunnels. Any other host gets 404. |
| `RELAY_LABEL` | The subdomain the CLI connects to (default `relay`). |
| `CONNECT_LIMITER` | Rate Limiting binding: 10 new tunnels per minute per IP, per Cloudflare location. |

To self-host, change the route and `PUBLIC_DOMAIN` to your own zone, deploy, and point the CLI at your relay:

```bash
sitegen -serve -share -share-server wss://relay.example.com
```

## Deploying

The `Relay` workflow ([../.github/workflows/relay.yaml](../.github/workflows/relay.yaml)) deploys on every push to `master` that touches `worker/**`, after the tests pass. It needs two repository secrets:

| Secret | Value |
|---|---|
| `CLOUDFLARE_API_TOKEN` | An API token from the "Edit Cloudflare Workers" template, with account resources set to your account and zone resources set to `sitegen.dev` (the zone is needed for the route) |
| `CLOUDFLARE_ACCOUNT_ID` | The account ID shown in the Cloudflare dashboard sidebar |

To deploy by hand instead, run `npx wrangler login`, then `npm run deploy`.

A Workers Paid plan is recommended for real traffic. On the Free plan, Durable Objects stop serving after 100,000 requests per day.

## The website

`sitegen.dev` is the starter site in [../site](../site), built by the sitegen in the same commit and served as static assets:

- [site.wrangler.jsonc](site.wrangler.jsonc) routes `sitegen.dev/*` to the files in `../public`. Pages are served without a trailing slash, and missing URLs get `404.html` with a 404 status.
- The `Site` workflow ([../.github/workflows/site.yaml](../.github/workflows/site.yaml)) builds and deploys it on pushes to `master` that touch `site/`, the engine or this config. It uses the same two secrets as the relay.

To deploy by hand, from the repo root:

```bash
go run . -clean -minify
cd worker && npx wrangler deploy -c site.wrangler.jsonc
```

## DNS

Both Workers run on routes, so the `sitegen.dev` zone needs proxied records for them to receive traffic. Nothing sits behind them, so the records can point at the placeholder address `100::`:

| Type | Name | Content | Proxy |
|---|---|---|---|
| `AAAA` | `sitegen.dev` | `100::` | Proxied |
| `AAAA` | `*` | `100::` | Proxied |
