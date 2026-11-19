import { env, evictDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import { exports } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FRAME_DATA, FRAME_END, PROTOCOL_VERSION, WINDOW } from "../src/protocol";

const RELAY = "https://relay.sitegen.dev/connect";

type Msg = { type: string; [k: string]: any };

/** A scripted stand-in for the sitegen CLI. */
class FakeCLI {
	ws!: WebSocket;
	ready!: { tunnel: string; url: string; token: string };
	private queue: Msg[] = [];
	private waiters: ((m: Msg) => void)[] = [];
	closed = false;

	static async connect(headers: Record<string, string> = {}): Promise<FakeCLI> {
		const res = await exports.default.fetch(RELAY, {
			headers: { upgrade: "websocket", "sitegen-protocol": PROTOCOL_VERSION, "sitegen-version": "test", ...headers },
		});
		if (res.status !== 101 || !res.webSocket) {
			throw Object.assign(new Error(`connect failed: ${res.status} ${await res.text()}`), { status: res.status });
		}
		const cli = new FakeCLI();
		cli.ws = res.webSocket;
		cli.ws.accept();
		cli.ws.addEventListener("message", (e) => {
			if (typeof e.data !== "string") return;
			const m = JSON.parse(e.data) as Msg;
			const w = cli.waiters.shift();
			if (w) w(m);
			else cli.queue.push(m);
		});
		cli.ws.addEventListener("close", () => (cli.closed = true));
		cli.ready = (await cli.next("ready")) as any;
		return cli;
	}

	host(): string {
		return new URL(this.ready.url).host;
	}

	/** Next message of `type`, skipping others (credits are tracked by the caller). */
	async next(type?: string, timeoutMs = 3000): Promise<Msg> {
		const deadline = Date.now() + timeoutMs;
		for (;;) {
			const m =
				this.queue.shift() ??
				(await new Promise<Msg>((resolve, reject) => {
					const t = setTimeout(() => reject(new Error(`timed out waiting for ${type ?? "message"}`)), Math.max(1, deadline - Date.now()));
					this.waiters.push((m) => {
						clearTimeout(t);
						resolve(m);
					});
				}));
			if (!type || m.type === type) return m;
		}
	}

	send(m: Msg) {
		this.ws.send(JSON.stringify(m));
	}

	frame(kind: number, id: number, data: Uint8Array = new Uint8Array()) {
		const buf = new Uint8Array(5 + data.length);
		buf[0] = kind;
		new DataView(buf.buffer).setUint32(1, id);
		buf.set(data, 5);
		this.ws.send(buf);
	}

	respond(id: number, status: number, headers: [string, string][], body?: string) {
		this.send({ type: "res", id, status, headers });
		if (body) this.frame(FRAME_DATA, id, new TextEncoder().encode(body));
		this.frame(FRAME_END, id);
	}
}

function visit(cli: FakeCLI, path = "/", init: RequestInit = {}) {
	return exports.default.fetch(`https://${cli.host()}${path}`, init);
}

const open: FakeCLI[] = [];
async function connect(headers?: Record<string, string>) {
	const cli = await FakeCLI.connect(headers);
	open.push(cli);
	return cli;
}

afterEach(() => {
	for (const cli of open.splice(0)) {
		try {
			cli.ws.close(1001, "test done");
		} catch {}
	}
	vi.restoreAllMocks();
});

describe("router", () => {
	it("serves a landing page on the relay host", async () => {
		const res = await exports.default.fetch("https://relay.sitegen.dev/");
		expect(res.status).toBe(200);
		expect(await res.text()).toContain("sitegen share relay");
	});

	it("shows the offline page for unknown tunnels and foreign hosts", async () => {
		for (const url of ["https://abcdefghijkl.sitegen.dev/", "https://0a1b2c3d.sitegen.dev/", "https://abcdefghijkl.example.com/"]) {
			const res = await exports.default.fetch(url);
			expect(res.status, url).toBe(404);
			expect(await res.text()).toContain("offline");
		}
	});

	it("rejects connects without the current protocol version", async () => {
		const res = await exports.default.fetch(RELAY, { headers: { upgrade: "websocket" } });
		expect(res.status).toBe(426);
		const old = await exports.default.fetch(RELAY, { headers: { upgrade: "websocket", "sitegen-protocol": "0" } });
		expect(old.status).toBe(426);
	});

	it("rejects plain HTTP on the connect path", async () => {
		const res = await exports.default.fetch(RELAY, { headers: { "sitegen-protocol": PROTOCOL_VERSION } });
		expect(res.status).toBe(400);
	});

	it("redirects www to the apex website", async () => {
		const res = await exports.default.fetch("https://www.sitegen.dev/docs/cli?x=1", { redirect: "manual" });
		expect(res.status).toBe(301);
		expect(res.headers.get("location")).toBe("https://sitegen.dev/docs/cli?x=1");
	});
});

describe("tunnel", () => {
	it("assigns a public URL on connect", async () => {
		const cli = await connect();
		expect(cli.ready.url).toMatch(/^https:\/\/[a-z2-7]{12}\.sitegen\.dev$/);
		expect(cli.ready.tunnel).toBe(cli.host().split(".")[0]);
		expect(cli.ready.token.length).toBeGreaterThan(20);
	});

	it("round-trips a visitor request through the CLI", async () => {
		const cli = await connect();
		const resP = visit(cli, "/about/?q=1", { headers: { accept: "text/html", "cf-connecting-ip": "198.51.100.7", "x-forwarded-for": "6.6.6.6" } });
		const req = await cli.next("req");
		expect(req.method).toBe("GET");
		expect(req.path).toBe("/about/?q=1");
		const h = new Map((req.headers as [string, string][]).map(([k, v]) => [k.toLowerCase(), v]));
		expect(h.get("host")).toBe(cli.host());
		expect(h.get("accept")).toBe("text/html");
		expect(h.get("x-forwarded-for")).toBe("198.51.100.7"); // spoofed value replaced
		expect([...h.keys()].some((k) => k.startsWith("cf-") || k === "x-sitegen-connect")).toBe(false);

		cli.respond(req.id, 200, [["Content-Type", "text/html"], ["Content-Length", "999"], ["Set-Cookie", "a=1"], ["Set-Cookie", "b=2"]], "<p>about</p>");
		const res = await resP;
		expect(res.status).toBe(200);
		expect(await res.text()).toBe("<p>about</p>");
		expect(res.headers.get("content-type")).toBe("text/html");
		expect(res.headers.get("content-length")).not.toBe("999");
		expect(res.headers.getSetCookie()).toEqual(["a=1", "b=2"]);
		expect(res.headers.get("x-robots-tag")).toBe("noindex");
	});

	it("is read-only", async () => {
		const cli = await connect();
		const res = await visit(cli, "/", { method: "POST", body: "x" });
		expect(res.status).toBe(405);
	});

	it("supports null-body statuses", async () => {
		const cli = await connect();
		const resP = visit(cli, "/old");
		const req = await cli.next("req");
		cli.respond(req.id, 304, [["ETag", '"v1"']]);
		const res = await resP;
		expect(res.status).toBe(304);
		expect(res.headers.get("etag")).toBe('"v1"');
	});

	it("returns 502 when the CLI aborts before responding", async () => {
		const cli = await connect();
		const resP = visit(cli, "/");
		const req = await cli.next("req");
		cli.send({ type: "cancel", id: req.id });
		expect((await resP).status).toBe(502);
	});

	it("streams large bodies with credit-based flow control", async () => {
		const cli = await connect();
		const resP = visit(cli, "/big.bin");
		const req = await cli.next("req");
		cli.send({ type: "res", id: req.id, status: 200, headers: [["Content-Type", "application/octet-stream"]] });
		const chunk = new Uint8Array(64 * 1024).fill(7);
		const total = WINDOW * 3;
		let sent = 0;
		let credits = WINDOW;
		const res = await resP;
		const reader = res.body!.getReader();
		let received = 0;
		const pump = (async () => {
			for (;;) {
				const { done, value } = await reader.read();
				if (done) return;
				received += value.length;
			}
		})();
		while (sent < total) {
			while (credits > 0 && sent < total) {
				cli.frame(FRAME_DATA, req.id, chunk);
				sent++;
				credits--;
			}
			if (sent < total) {
				const c = await cli.next("credit");
				expect(c.id).toBe(req.id);
				credits += c.n;
			}
		}
		cli.frame(FRAME_END, req.id);
		await pump;
		expect(received).toBe(total * chunk.length);
	});

	it("requires share auth when configured and does not forward it", async () => {
		const cli = await connect({ "sitegen-share-auth": btoa("alice:s3cret") });
		const denied = await visit(cli, "/");
		expect(denied.status).toBe(401);
		expect(denied.headers.get("www-authenticate")).toContain("Basic");
		expect((await visit(cli, "/", { headers: { authorization: "Basic " + btoa("alice:wrong") } })).status).toBe(401);

		const resP = visit(cli, "/", { headers: { authorization: "Basic " + btoa("alice:s3cret") } });
		const req = await cli.next("req");
		expect((req.headers as [string, string][]).some(([k]) => k.toLowerCase() === "authorization")).toBe(false);
		cli.respond(req.id, 200, [], "ok");
		expect(await (await resP).text()).toBe("ok");
	});

	it("broadcasts reloads to viewers over hibernatable sockets", async () => {
		const cli = await connect();
		const res = await visit(cli, "/__hotreload", { headers: { upgrade: "websocket" } });
		expect(res.status).toBe(101);
		const viewer = res.webSocket!;
		viewer.accept();
		const got = new Promise<string>((resolve) => viewer.addEventListener("message", (e: MessageEvent) => resolve(e.data as string)));
		cli.send({ type: "reload" });
		expect(await got).toBe("updated");
		viewer.close(1000);

		// The old SSE script gets a 204 so it stops retrying.
		expect((await visit(cli, "/__hotreload")).status).toBe(204);
	});

	it("keeps serving after the Durable Object hibernates", async () => {
		const cli = await connect();
		const stub = env.TUNNEL.get(env.TUNNEL.idFromName(cli.ready.tunnel));
		await evictDurableObject(stub);
		const resP = visit(cli, "/after-sleep");
		const req = await cli.next("req");
		expect(req.path).toBe("/after-sleep");
		cli.respond(req.id, 200, [], "awake");
		expect(await (await resP).text()).toBe("awake");
	});

	it("lets a dropped CLI resume the same URL, once, with its token", async () => {
		const cli = await connect();
		const { tunnel, token, url } = cli.ready;
		cli.ws.close(4001, "network blip");
		await vi.waitUntil(async () => (await exports.default.fetch(url + "/")).status === 404);

		await expect(FakeCLI.connect({ "sitegen-resume": `${tunnel}:wrong` })).rejects.toMatchObject({ status: 401 });

		const again = await connect({ "sitegen-resume": `${tunnel}:${token}` });
		expect(again.ready.url).toBe(url);
		expect(again.ready.token).not.toBe(token); // rotated

		const resP = visit(again, "/");
		const req = await again.next("req");
		again.respond(req.id, 200, [], "back");
		expect(await (await resP).text()).toBe("back");
	});

	it("forgets a tunnel after the resume window", async () => {
		const cli = await connect();
		const { tunnel, token } = cli.ready;
		const stub = env.TUNNEL.get(env.TUNNEL.idFromName(tunnel));
		cli.ws.close(4001, "gone");
		await vi.waitUntil(() => runDurableObjectAlarm(stub), { timeout: 3000, interval: 50 });
		await expect(FakeCLI.connect({ "sitegen-resume": `${tunnel}:${token}` })).rejects.toMatchObject({ status: 401 });
	});

	it("forgets a tunnel immediately when the CLI exits cleanly", async () => {
		const cli = await connect();
		const { tunnel, token } = cli.ready;
		cli.ws.close(1001, "client shutting down");
		await vi.waitUntil(
			async () => {
				try {
					const c = await FakeCLI.connect({ "sitegen-resume": `${tunnel}:${token}` });
					c.ws.close(1001);
					return false;
				} catch (e: any) {
					return e.status === 401;
				}
			},
			{ timeout: 3000, interval: 100 },
		);
	});
});
