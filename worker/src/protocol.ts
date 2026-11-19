// Shared constants and helpers for the sitegen share relay protocol v1.
// The wire format is documented in docs/SHARE_PROTOCOL.md; the Go side lives
// in pkg/share/client.go. Keep the three in sync.

export const PROTOCOL_VERSION = "1";

/** Internal header carrying connect parameters from the router to the DO. */
export const CONNECT_HEADER = "x-sitegen-connect";

/** Binary frame types (client -> relay): [type u8][request id u32 BE][payload]. */
export const FRAME_DATA = 1;
export const FRAME_END = 2;
export const FRAME_HEADER_SIZE = 5;

/** Body frames the client may send per request before it needs a credit. */
export const WINDOW = 8;

/** New tunnel IDs: 12 chars of lowercase RFC 4648 base32 (60 random bits). */
export const TUNNEL_ID_RE = /^[a-z2-7]{12}$/;
/** IDs issued by the old Go relay (8 hex chars), passed through during the overlap. */
export const LEGACY_ID_RE = /^[0-9a-f]{8}$/;

const B32 = "abcdefghijklmnopqrstuvwxyz234567";

export function newTunnelId(): string {
	const bytes = crypto.getRandomValues(new Uint8Array(12));
	let id = "";
	for (const b of bytes) id += B32[b & 31];
	return id;
}

export function newToken(): string {
	const bytes = crypto.getRandomValues(new Uint8Array(32));
	return btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export async function sha256Hex(s: string): Promise<string> {
	const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
	return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/** Constant-time comparison of two equal-length hex digests. */
export function digestsEqual(a: string, b: string): boolean {
	if (a.length !== b.length) return false;
	const enc = new TextEncoder();
	return crypto.subtle.timingSafeEqual(enc.encode(a), enc.encode(b));
}

/** Messages exchanged as WebSocket text frames. */
export type ClientMessage =
	| { type: "res"; id: number; status: number; headers?: [string, string][] }
	| { type: "cancel"; id: number }
	| { type: "reload" };

export type RelayMessage =
	| { type: "ready"; tunnel: string; url: string; token: string }
	| { type: "req"; id: number; method: string; path: string; headers: [string, string][] }
	| { type: "credit"; id: number; n: number }
	| { type: "cancel"; id: number };

/** Headers that describe a single hop and must not be forwarded. */
export const HOP_BY_HOP = new Set([
	"connection",
	"keep-alive",
	"proxy-authenticate",
	"proxy-authorization",
	"proxy-connection",
	"te",
	"trailer",
	"transfer-encoding",
	"upgrade",
]);

export function htmlPage(status: number, title: string, message: string): Response {
	const body = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>${title}</title><style>body{font:16px/1.5 system-ui,sans-serif;margin:0;min-height:100vh;display:grid;place-items:center;background:#f6f6f7;color:#222}@media(prefers-color-scheme:dark){body{background:#111214;color:#ddd}}main{max-width:32rem;padding:2rem}h1{font-size:1.25rem;margin:0 0 .5rem}p{margin:0;opacity:.8}</style></head><body><main><h1>${title}</h1><p>${message}</p></main></body></html>`;
	return new Response(body, {
		status,
		headers: { "content-type": "text/html; charset=utf-8", "cache-control": "no-store", "x-robots-tag": "noindex" },
	});
}

export function offlinePage(): Response {
	return htmlPage(404, "This preview is offline", "The sitegen share link you followed is not connected right now. If you own it, run <code>sitegen -serve -share</code> again.");
}
