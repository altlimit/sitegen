# Share Relay

The share relay server enables `sitegen -serve -share` to expose local dev servers via public URLs through yamux tunneling.

**Caddyfile:**

```caddyfile
*.sitegen.dev {
    tls {
        dns cloudflare {env.CF_API_TOKEN}
    }
    reverse_proxy sharerelay:8080
}
```

## CLI Flags

| Flag | Default | Description |
|---|---|---|
| `-domain` | `sitegen.dev` | Base domain for subdomains |
| `-client-port` | `9443` | TCP port for client tunnel connections |
| `-http-port` | `8080` | HTTP port for public requests (behind reverse proxy) |
| `-max-sessions-per-ip` | `5` | Max concurrent sessions per client IP |
| `-tls-cert` | `""` | TLS certificate file for tunnel listener |
| `-tls-key` | `""` | TLS private key file for tunnel listener |

## DNS Setup

Add these DNS records:

- `A` → `sitegen.dev` → `your-server-ip`
- `A` → `*.sitegen.dev` → `your-server-ip`

## How It Works

1. `sitegen -serve -share` connects to the relay on port 9443 via TCP
2. Relay assigns a random subdomain (e.g., `a1b2c3d4.sitegen.dev`)
3. Public HTTP requests to that subdomain are forwarded through yamux streams
4. Only GET/HEAD requests are allowed
