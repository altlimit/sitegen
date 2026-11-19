// sitegen share relay: routes `relay.<domain>/connect` WebSocket upgrades from
// the CLI and `<tunnel>.<domain>` visitor requests to one Tunnel Durable
// Object per tunnel ID.

import { CONNECT_HEADER, LEGACY_ID_RE, PROTOCOL_VERSION, TUNNEL_ID_RE, htmlPage, newTunnelId, offlinePage } from "./protocol";
import type { Tunnel } from "./tunnel";

export { Tunnel } from "./tunnel";

export interface Env {
	TUNNEL: DurableObjectNamespace<Tunnel>;
	/** Optional Workers Rate Limiting binding guarding new tunnel connections. */
	CONNECT_LIMITER?: RateLimit;
	/** Parent domain for tunnels, e.g. "sitegen.dev". Empty accepts any parent. */
	PUBLIC_DOMAIN?: string;
	/** Subdomain label the CLI connects to. Default "relay". */
	RELAY_LABEL?: string;
	/** "true" forwards old 8-hex-char tunnel hosts to the origin (the legacy VPS). */
	LEGACY_PASSTHROUGH?: string;
}

export default {
	async fetch(request: Request, env: Env): Promise<Response> {
		const url = new URL(request.url);
		const host = url.hostname.toLowerCase();
		const dot = host.indexOf(".");
		if (dot <= 0) return notFound();
		const label = host.slice(0, dot);
		const parent = host.slice(dot + 1);
		const domain = (env.PUBLIC_DOMAIN ?? "").toLowerCase();
		if (domain && parent !== domain) return notFound();

		if (label === (env.RELAY_LABEL || "relay")) {
			return connect(request, env, url, parent);
		}
		if (TUNNEL_ID_RE.test(label)) {
			const headers = new Headers(request.headers);
			headers.delete(CONNECT_HEADER);
			return stub(env, label).fetch(new Request(request, { headers }));
		}
		if (LEGACY_ID_RE.test(label) && env.LEGACY_PASSTHROUGH === "true") {
			// Old sitegen releases: let Cloudflare send this to the origin
			// behind the DNS record (the legacy VPS relay).
			return fetch(request);
		}
		return notFound();
	},
} satisfies ExportedHandler<Env>;

function stub(env: Env, id: string) {
	return env.TUNNEL.get(env.TUNNEL.idFromName(id));
}

function notFound(): Response {
	return offlinePage();
}

function text(status: number, body: string, headers: Record<string, string> = {}): Response {
	return new Response(body + "\n", { status, headers: { "content-type": "text/plain; charset=utf-8", ...headers } });
}

async function connect(request: Request, env: Env, url: URL, parent: string): Promise<Response> {
	if (url.pathname === "/" && request.headers.get("upgrade") === null) {
		return htmlPage(200, "sitegen share relay", 'This relay powers <code>sitegen -serve -share</code>. See <a href="https://github.com/altlimit/sitegen">github.com/altlimit/sitegen</a>.');
	}
	if (url.pathname !== "/connect") return text(404, "not found");
	if (request.headers.get("upgrade")?.toLowerCase() !== "websocket") {
		return text(400, "expected a WebSocket upgrade");
	}
	const protocol = request.headers.get("sitegen-protocol");
	if (protocol !== PROTOCOL_VERSION) {
		return text(426, `this relay speaks sitegen share protocol ${PROTOCOL_VERSION}; please upgrade sitegen`);
	}

	const resume = request.headers.get("sitegen-resume");
	// Only new tunnels count against the limit: a resume needs a valid token
	// and must keep working for a CLI on a flaky network.
	if (env.CONNECT_LIMITER && !resume) {
		const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
		const { success } = await env.CONNECT_LIMITER.limit({ key: `connect:${ip}` });
		if (!success) return text(429, "too many new share connections, try again in a minute", { "retry-after": "60" });
	}

	let id: string;
	if (resume) {
		id = resume.slice(0, resume.indexOf(":"));
		if (!TUNNEL_ID_RE.test(id)) return text(400, "malformed Sitegen-Resume header");
	} else {
		id = newTunnelId();
	}

	const port = url.port ? `:${url.port}` : "";
	const publicURL = `${url.protocol}//${id}.${parent}${port}`;
	const headers = new Headers(request.headers);
	headers.set(CONNECT_HEADER, JSON.stringify({ id, url: publicURL }));
	return stub(env, id).fetch(new Request(request, { headers }));
}
