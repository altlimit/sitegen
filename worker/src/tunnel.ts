// Tunnel is one Durable Object per share tunnel. It holds the CLI's
// WebSocket (hibernatable, so an idle tunnel is evicted from memory and
// costs nothing) and multiplexes visitor requests over it.

import { DurableObject } from "cloudflare:workers";
import type { Env } from "./index";
import {
	CONNECT_HEADER,
	FRAME_DATA,
	FRAME_END,
	FRAME_HEADER_SIZE,
	HOP_BY_HOP,
	WINDOW,
	digestsEqual,
	newToken,
	offlinePage,
	sha256Hex,
	type ClientMessage,
	type RelayMessage,
} from "./protocol";

// Limits. Keep docs/SHARE_PROTOCOL.md in step.
const RATE_PER_SEC = 50;
const RATE_BURST = 100;
const MAX_INFLIGHT = 64;
const MAX_RELOAD_SOCKETS = 100;
const MAX_REQUEST_HEADER_BYTES = 16 * 1024;
const MAX_RESPONSE_BYTES = 50 * 1024 * 1024;
const HEAD_TIMEOUT_MS = 30_000;
const IDLE_TIMEOUT_MS = 30_000;
/** How long a disconnected tunnel's ID and resume token are kept. */
const RESUME_TTL_MS = 24 * 60 * 60 * 1000;

const NULL_BODY_STATUS = new Set([101, 204, 205, 304]);

interface Meta {
	id: string;
	url: string;
	tokenHash: string;
	authHash?: string;
	version?: string;
}

interface Pending {
	id: number;
	/** Set until the response head arrives. */
	resolve?: (r: Response) => void;
	controller?: ReadableStreamDefaultController<Uint8Array>;
	nullBody: boolean;
	timer?: ReturnType<typeof setTimeout>;
	bytes: number;
	enqueued: number;
	granted: number;
}

export class Tunnel extends DurableObject<Env> {
	// In-memory state is lost when the object hibernates. That is fine:
	// nothing is in flight while hibernated, and meta is reloaded from storage.
	private meta: Meta | null | undefined;
	private pending = new Map<number, Pending>();
	private nextId: number;
	private tokens = RATE_BURST;
	private refilledAt = Date.now();

	constructor(ctx: DurableObjectState, env: Env) {
		super(ctx, env);
		// Random start so IDs from before an eviction are never reused.
		this.nextId = (crypto.getRandomValues(new Uint32Array(1))[0] >>> 1) + 1;
	}

	async fetch(request: Request): Promise<Response> {
		const connect = request.headers.get(CONNECT_HEADER);
		if (connect) return this.handleConnect(request, JSON.parse(connect) as { id: string; url: string });
		return this.visit(request);
	}

	// ---- CLI side -------------------------------------------------------

	private async handleConnect(request: Request, params: { id: string; url: string }): Promise<Response> {
		const meta = await this.loadMeta();
		const resume = request.headers.get("sitegen-resume");
		if (resume) {
			const token = resume.slice(resume.indexOf(":") + 1);
			if (!meta || !digestsEqual(await sha256Hex(token), meta.tokenHash)) {
				return plain(401, "unknown or expired tunnel");
			}
			// A reconnect after a half-open connection: drop the stale socket.
			this.failAll("tunnel reconnected");
			for (const ws of this.ctx.getWebSockets("tunnel")) {
				try {
					ws.close(4000, "replaced by a new connection");
				} catch {}
			}
		} else if (meta || this.ctx.getWebSockets("tunnel").length > 0) {
			return plain(409, "tunnel id in use");
		}

		let authHash: string | undefined;
		const auth = request.headers.get("sitegen-share-auth");
		if (auth) {
			let decoded: string;
			try {
				decoded = atob(auth);
			} catch {
				return plain(400, "Sitegen-Share-Auth must be base64");
			}
			if (!decoded.includes(":")) return plain(400, 'share auth must be "user:pass"');
			authHash = await sha256Hex(decoded);
		}

		const token = newToken();
		const next: Meta = {
			id: params.id,
			url: params.url,
			tokenHash: await sha256Hex(token),
			authHash,
			version: request.headers.get("sitegen-version") ?? undefined,
		};
		await this.ctx.storage.put("meta", next);
		await this.ctx.storage.deleteAlarm();
		this.meta = next;

		const [client, server] = Object.values(new WebSocketPair());
		this.ctx.acceptWebSocket(server, ["tunnel"]);
		server.send(json({ type: "ready", tunnel: next.id, url: next.url, token }));
		return new Response(null, { status: 101, webSocket: client });
	}

	async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
		const tags = this.ctx.getTags(ws);
		if (tags.includes("reload")) {
			// Viewers only listen; anything they send is unexpected.
			ws.close(1008, "unexpected message");
			return;
		}
		if (!tags.includes("tunnel")) return;

		if (typeof message === "string") {
			let m: ClientMessage;
			try {
				m = JSON.parse(message);
			} catch {
				return;
			}
			switch (m.type) {
				case "res":
					this.onHead(ws, m);
					break;
				case "cancel":
					this.onCancel(m.id);
					break;
				case "reload":
					this.broadcastReload();
					break;
			}
			return;
		}
		this.onFrame(ws, new Uint8Array(message));
	}

	async webSocketClose(ws: WebSocket, code: number, reason: string): Promise<void> {
		try {
			ws.close(1000, "closing");
		} catch {}
		if (!this.ctx.getTags(ws).includes("tunnel")) return;
		if (this.openTunnel(ws)) return; // already replaced by a reconnect
		this.failAll("tunnel disconnected");
		if (code === 1001) {
			// The CLI exited on purpose; there is nothing to resume.
			await this.cleanup();
		} else {
			await this.ctx.storage.setAlarm(Date.now() + RESUME_TTL_MS);
		}
	}

	async webSocketError(ws: WebSocket): Promise<void> {
		if (!this.ctx.getTags(ws).includes("tunnel") || this.openTunnel(ws)) return;
		this.failAll("tunnel error");
		await this.ctx.storage.setAlarm(Date.now() + RESUME_TTL_MS);
	}

	async alarm(): Promise<void> {
		if (this.openTunnel()) return;
		await this.cleanup();
	}

	private async cleanup(): Promise<void> {
		for (const ws of this.ctx.getWebSockets("reload")) {
			try {
				ws.close(1001, "share ended");
			} catch {}
		}
		await this.ctx.storage.deleteAlarm();
		await this.ctx.storage.deleteAll();
		this.meta = null;
	}

	private openTunnel(except?: WebSocket): WebSocket | undefined {
		return this.ctx.getWebSockets("tunnel").find((ws) => ws !== except && ws.readyState === WebSocket.READY_STATE_OPEN);
	}

	private async loadMeta(): Promise<Meta | null> {
		if (this.meta === undefined) this.meta = (await this.ctx.storage.get<Meta>("meta")) ?? null;
		return this.meta;
	}

	// ---- Visitor side ---------------------------------------------------

	private async visit(request: Request): Promise<Response> {
		const tunnel = this.openTunnel();
		const meta = tunnel ? await this.loadMeta() : null;
		if (!tunnel || !meta) return offlinePage();

		const url = new URL(request.url);
		if (url.pathname === "/__hotreload") return this.reloadSocket(request);

		if (request.method !== "GET" && request.method !== "HEAD") {
			return plain(405, "this preview is read-only", { allow: "GET, HEAD" });
		}
		if (meta.authHash && !(await this.authorized(request, meta.authHash))) {
			return plain(401, "authentication required", { "www-authenticate": 'Basic realm="sitegen share", charset="UTF-8"' });
		}
		if (!this.takeToken()) return plain(429, "too many requests", { "retry-after": "1" });
		if (this.pending.size >= MAX_INFLIGHT) return plain(503, "this preview is busy, try again", { "retry-after": "1" });

		const headers: [string, string][] = [["Host", url.host]];
		let size = 0;
		for (const [k, v] of request.headers) {
			if (HOP_BY_HOP.has(k) || k.startsWith("cf-") || k.startsWith("x-forwarded-") || k === "x-real-ip" || k === "host" || k === CONNECT_HEADER) continue;
			if (k === "authorization" && meta.authHash) continue; // consumed by the relay
			headers.push([k, v]);
			size += k.length + v.length;
		}
		if (size > MAX_REQUEST_HEADER_BYTES) return plain(431, "request headers too large");
		const ip = request.headers.get("cf-connecting-ip");
		if (ip) headers.push(["X-Forwarded-For", ip]);
		headers.push(["X-Forwarded-Proto", url.protocol.replace(":", "")], ["X-Forwarded-Host", url.host]);

		const id = this.allocId();
		const response = new Promise<Response>((resolve) => {
			const p: Pending = { id, resolve, nullBody: false, bytes: 0, enqueued: 0, granted: 0 };
			p.timer = setTimeout(() => {
				this.send(tunnel, { type: "cancel", id });
				this.finish(p);
				resolve(plain(504, "the sitegen dev server did not respond in time"));
			}, HEAD_TIMEOUT_MS);
			this.pending.set(id, p);
		});
		if (!this.send(tunnel, { type: "req", id, method: request.method, path: url.pathname + url.search, headers })) {
			const p = this.pending.get(id);
			if (p) {
				this.finish(p);
				p.resolve?.(plain(502, "the sitegen dev server is unreachable"));
			}
		}
		return response;
	}

	private reloadSocket(request: Request): Response {
		if (request.headers.get("upgrade")?.toLowerCase() !== "websocket") {
			// An old SSE-based script: tell it there is nothing here.
			return new Response(null, { status: 204 });
		}
		if (this.ctx.getWebSockets("reload").length >= MAX_RELOAD_SOCKETS) return plain(429, "too many viewers");
		const [client, server] = Object.values(new WebSocketPair());
		this.ctx.acceptWebSocket(server, ["reload"]);
		return new Response(null, { status: 101, webSocket: client });
	}

	private broadcastReload(): void {
		for (const ws of this.ctx.getWebSockets("reload")) {
			try {
				ws.send("updated");
			} catch {}
		}
	}

	private async authorized(request: Request, authHash: string): Promise<boolean> {
		const h = request.headers.get("authorization") ?? "";
		if (!/^basic /i.test(h)) return false;
		let decoded: string;
		try {
			decoded = atob(h.slice(6).trim());
		} catch {
			return false;
		}
		return digestsEqual(await sha256Hex(decoded), authHash);
	}

	private takeToken(): boolean {
		const now = Date.now();
		this.tokens = Math.min(RATE_BURST, this.tokens + ((now - this.refilledAt) / 1000) * RATE_PER_SEC);
		this.refilledAt = now;
		if (this.tokens < 1) return false;
		this.tokens -= 1;
		return true;
	}

	private allocId(): number {
		do {
			this.nextId = this.nextId >= 0xffffffff ? 1 : this.nextId + 1;
		} while (this.pending.has(this.nextId));
		return this.nextId;
	}

	// ---- Responses from the CLI -----------------------------------------

	private onHead(ws: WebSocket, m: Extract<ClientMessage, { type: "res" }>): void {
		const p = this.pending.get(m.id);
		if (!p?.resolve) return;
		const resolve = p.resolve;
		p.resolve = undefined;
		if (p.timer !== undefined) clearTimeout(p.timer);

		if (!Number.isInteger(m.status) || m.status < 200 || m.status > 599) {
			this.send(ws, { type: "cancel", id: p.id });
			this.finish(p);
			resolve(plain(502, "the sitegen dev server sent an invalid response"));
			return;
		}

		const headers = new Headers();
		for (const [k, v] of m.headers ?? []) {
			const lk = k.toLowerCase();
			// The body is re-streamed, so the origin's length/encoding framing does not apply.
			if (HOP_BY_HOP.has(lk) || lk === "content-length") continue;
			try {
				headers.append(k, v);
			} catch {}
		}
		headers.set("x-robots-tag", "noindex");

		if (NULL_BODY_STATUS.has(m.status)) {
			p.nullBody = true;
			this.armIdle(ws, p);
			resolve(new Response(null, { status: m.status, headers }));
			return;
		}

		const stream = new ReadableStream<Uint8Array>(
			{
				start: (controller) => {
					p.controller = controller;
				},
				pull: (controller) => {
					// Credit back every chunk the visitor has consumed.
					const queued = WINDOW - (controller.desiredSize ?? 0);
					const delta = p.enqueued - queued - p.granted;
					if (delta > 0 && this.pending.has(p.id)) {
						p.granted += delta;
						this.send(ws, { type: "credit", id: p.id, n: delta });
						this.armIdle(ws, p);
					}
				},
				cancel: () => {
					if (this.pending.has(p.id)) {
						this.send(ws, { type: "cancel", id: p.id });
						this.finish(p);
					}
				},
			},
			{ highWaterMark: WINDOW, size: () => 1 },
		);
		this.armIdle(ws, p);
		resolve(new Response(stream, { status: m.status, headers }));
	}

	private onFrame(ws: WebSocket, buf: Uint8Array): void {
		if (buf.length < FRAME_HEADER_SIZE) return;
		const kind = buf[0];
		const id = new DataView(buf.buffer, buf.byteOffset, buf.byteLength).getUint32(1);
		const p = this.pending.get(id);
		if (!p || p.resolve) {
			// Unknown (cancelled or timed out) request: tell the CLI to stop.
			if (kind === FRAME_DATA) this.send(ws, { type: "cancel", id });
			return;
		}
		if (kind === FRAME_END) {
			try {
				p.controller?.close();
			} catch {}
			this.finish(p);
			return;
		}
		if (kind !== FRAME_DATA) return;

		if (p.nullBody || !p.controller) {
			this.send(ws, { type: "credit", id, n: 1 });
			return;
		}
		p.bytes += buf.length - FRAME_HEADER_SIZE;
		if (p.bytes > MAX_RESPONSE_BYTES) {
			this.send(ws, { type: "cancel", id });
			p.controller.error(new Error("response exceeds the share size limit"));
			this.finish(p);
			return;
		}
		p.controller.enqueue(buf.slice(FRAME_HEADER_SIZE));
		p.enqueued++;
		this.armIdle(ws, p);
	}

	private onCancel(id: number): void {
		const p = this.pending.get(id);
		if (!p) return;
		if (p.resolve) p.resolve(plain(502, "the sitegen dev server aborted the response"));
		else {
			try {
				p.controller?.error(new Error("aborted by the sitegen dev server"));
			} catch {}
		}
		this.finish(p);
	}

	private armIdle(ws: WebSocket, p: Pending): void {
		if (p.timer !== undefined) clearTimeout(p.timer);
		p.timer = setTimeout(() => {
			if (!this.pending.has(p.id)) return;
			this.send(ws, { type: "cancel", id: p.id });
			try {
				p.controller?.error(new Error("response stalled"));
			} catch {}
			this.finish(p);
		}, IDLE_TIMEOUT_MS);
	}

	private finish(p: Pending): void {
		if (p.timer !== undefined) clearTimeout(p.timer);
		this.pending.delete(p.id);
	}

	private failAll(reason: string): void {
		for (const p of [...this.pending.values()]) {
			if (p.resolve) p.resolve(plain(502, `the sitegen dev server went away (${reason})`));
			else {
				try {
					p.controller?.error(new Error(reason));
				} catch {}
			}
			this.finish(p);
		}
	}

	private send(ws: WebSocket, m: RelayMessage): boolean {
		try {
			ws.send(json(m));
			return true;
		} catch {
			return false;
		}
	}
}

function json(m: RelayMessage): string {
	return JSON.stringify(m);
}

function plain(status: number, body: string, headers: Record<string, string> = {}): Response {
	return new Response(body + "\n", { status, headers: { "content-type": "text/plain; charset=utf-8", "x-robots-tag": "noindex", ...headers } });
}
