# Share relay protocol v1

This is how `sitegen -serve -share` talks to the relay. There are two implementations, and both must follow this document:

- the Go client in [pkg/share/client.go](../pkg/share/client.go)
- the Cloudflare Worker in [worker/src/](../worker/src/)

A change to any one of the three must be made in all of them. A change that breaks compatibility must bump the protocol version.

## Overview

```
sitegen CLI ──wss──> relay.<domain>/connect ─┐
                                             ├─> Worker ──> Tunnel Durable Object (one per tunnel ID)
visitor ──https──> <tunnel>.<domain> ────────┘
```

The CLI holds a single WebSocket to the relay. The relay sends visitor requests down that socket, and the CLI serves them from its local handler and streams the responses back. Nothing on the relay listens on raw TCP.

## Connecting

The CLI opens a WebSocket to `wss://relay.<domain>/connect` with these request headers:

| Header | Required | Value |
|---|---|---|
| `Sitegen-Protocol` | yes | `1` |
| `Sitegen-Version` | yes | sitegen version string (informational) |
| `Sitegen-Share-Auth` | no | base64 of `user:pass`. Visitors must then send matching HTTP Basic credentials. |
| `Sitegen-Resume` | no | `<tunnel>:<token>`, taken from a previous `ready`, to keep the same URL |

If the relay refuses the connection, it does so before the upgrade, with a `text/plain` body:

| Status | Meaning | What the client does |
|---|---|---|
| 400 | Malformed headers | Show the error and retry with backoff |
| 401 | Resume token unknown or expired | Drop the resume and connect fresh, without waiting |
| 409 | Tunnel ID in use (new connections only) | Retry |
| 426 | Unsupported protocol version | Stop and tell the user to upgrade sitegen |
| 429 | Too many new tunnels from this IP | Retry with backoff. Resumes are not rate limited. |

Once the upgrade succeeds, the relay's first message is always `ready`.

## Messages

Text frames carry JSON objects with a `type` field. Request IDs (`id`) are non-zero uint32 values chosen by the relay.

| Direction | Message | Meaning |
|---|---|---|
| relay → CLI | `{"type":"ready","tunnel":"<12 chars>","url":"https://<tunnel>.<domain>","token":"<opaque>"}` | Registered. The token rotates on every connect. |
| relay → CLI | `{"type":"req","id":N,"method":"GET","path":"/a?b","headers":[["Host","…"],["Accept","…"],…]}` | A visitor request. The method is GET or HEAD only, and there is no body. |
| CLI → relay | `{"type":"res","id":N,"status":200,"headers":[["Content-Type","…"],…]}` | Response head. Header pairs may repeat a name, for example `Set-Cookie`. |
| relay → CLI | `{"type":"credit","id":N,"n":K}` | The visitor consumed K body frames, so K more may be sent. |
| either way | `{"type":"cancel","id":N}` | Abandon request N. The receiver discards it, and the CLI cancels the handler's context. |
| CLI → relay | `{"type":"reload"}` | The site rebuilt. The relay tells every live-reload viewer. |

Binary frames carry response bodies, from the CLI to the relay only:

```
[type u8][request id u32 big-endian][payload]
type 1 = body data (payload up to 64 KiB)
type 2 = end of body (empty payload)
```

### Flow control

- For each request, the CLI may send up to **8** body frames before it receives a credit.
- The relay grants credits as the visitor's connection drains, so a slow visitor cannot make the relay buffer a whole file. At most about 512 KiB is buffered per request.
- A response with a null-body status (101, 204, 205, 304), or a reply to a HEAD request, still ends with a type 2 frame.

### Request headers

The relay controls these headers on every request it sends to the CLI:

| Header | Treatment |
|---|---|
| `Host` | Set to the public host |
| `X-Forwarded-For` | Set from the visitor's IP. Any value the visitor sent is replaced. |
| `X-Forwarded-Proto`, `X-Forwarded-Host` | Set by the relay |
| Hop-by-hop headers, `cf-*`, `x-real-ip` | Removed |
| `Authorization` | Removed when share auth is on (the relay has already checked it) |

## Keepalive and lifetime

- **Pings.** The CLI sends WebSocket protocol pings every 25 seconds. Cloudflare answers them without waking the Durable Object, so an idle tunnel costs nothing. If a pong does not arrive within 15 seconds, the CLI reconnects.
- **Connection drops.** If the connection drops with any close code other than 1001, the relay keeps the tunnel's ID and resume token for 24 hours. A reconnect with `Sitegen-Resume` gets the same URL back.
- **Clean exit.** When the CLI quits, it closes the socket with **1001**, and the relay forgets the tunnel immediately.
- **Replaced connections.** A successful resume replaces any older socket for that tunnel. The relay closes the old socket with code 4000.

## Live reload for viewers

- **Tunnelled pages.** Pages served through the tunnel carry a script that opens `wss://<tunnel>.<domain>/__hotreload`. The relay accepts that socket itself, as a hibernatable WebSocket, and sends the text `updated` on each `reload`.
- **Non-upgrade requests.** A plain request to `/__hotreload` gets 204.
- **Local paths hidden.** The local `/__hotreload` SSE stream and `/__cms` are never reachable through the tunnel.
- **No auth on these sockets.** They are not covered by share auth, because browsers do not reliably send Basic credentials on WebSocket handshakes. They carry no content.

## Limits (per tunnel)

| Limit | Value | Response when exceeded |
|---|---|---|
| Request rate | 50/s, burst 100 | 429 |
| Requests in flight | 64 | 503 |
| Request header bytes | 16 KiB | 431 |
| Response body | 50 MB | Stream aborted |
| Time to response head | 30 s | 504 |
| Gap between body frames (or visitor reads) | 30 s | Stream aborted |
| Live-reload viewers | 100 | 429 |
| Methods | GET, HEAD | 405 |

## Tunnel IDs

- **New IDs** are 12 characters of lowercase base32 (`[a-z2-7]`), which is 60 random bits.
- **Legacy IDs.** The old Go relay issued 8 lowercase hex characters. Those hosts now get the offline page, and old CLIs can't connect: they spoke a raw TLS protocol on port 9443 that the Worker doesn't accept.
