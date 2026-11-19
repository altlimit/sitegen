# sitegen share relay (Cloudflare Worker)

This Worker is the public side of `sitegen -serve -share`. It has two parts:

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
| `routes` | `*.sitegen.dev/*`. It covers `relay.sitegen.dev` and every tunnel host. |
| `PUBLIC_DOMAIN` | The parent domain for tunnels. Any other host gets 404. |
| `RELAY_LABEL` | The subdomain the CLI connects to (default `relay`). |
| `LEGACY_PASSTHROUGH` | `"true"` forwards old 8-hex-character tunnel hosts to the origin, which is the old VPS. |
| `CONNECT_LIMITER` | Rate Limiting binding: 10 new tunnels per minute per IP, per Cloudflare location. |

To self-host, change the route and `PUBLIC_DOMAIN` to your own zone, set `LEGACY_PASSTHROUGH` to `"false"`, deploy, and point the CLI at your relay:

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

## Migration from the Go relay (sitegen.dev)

Released sitegen versions before this change connect to `sitegen.dev:9443` over raw TLS, which a Worker cannot accept. The plan is to run both relays for 60 days, then shut the VPS down. This assumes the `sitegen.dev` zone is already on Cloudflare, as the old Caddy DNS-01 setup implies.

1. **Deploy the Worker**, either by pushing to `master` with the secrets set or by hand (see [Deploying](#deploying)). Until step 2, tunnel hosts are DNS-only, so no traffic reaches the Worker yet.
2. **Update DNS** in the `sitegen.dev` zone:
   - Set the `*` record (the VPS IP) to **Proxied**, so tunnel hosts reach the Worker. With `LEGACY_PASSTHROUGH` on, old 8-hex hosts are still forwarded to the VPS.
   - Leave the apex `sitegen.dev` record **DNS only**, so old CLIs can still reach port 9443 directly.
   - Set the SSL/TLS mode to **Full (strict)**. Caddy's wildcard certificate keeps working for the passthrough.
3. **Check both generations at once:**
   - Run an old release with `sitegen -serve -share`. You should get an 8-hex URL that works.
   - Run a build with this change. You should get a 12-character URL that works.
4. **Release the new CLI.**
5. **After 60 days:**
   - Set `LEGACY_PASSTHROUGH` to `"false"` and redeploy.
   - Replace the `*` record with a proxied `AAAA * 100::`.
   - Shut down the VPS.
   - Old CLIs will then fail to connect and must upgrade.
