import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

export default defineConfig({
	plugins: [
		cloudflareTest({
			wrangler: { configPath: "./wrangler.jsonc" },
			miniflare: {
				// Miniflare enforces the limiter locally; tests open many tunnels.
				ratelimits: { CONNECT_LIMITER: { namespace_id: "7421", simple: { limit: 10000, period: 60 } } },
				bindings: { LEGACY_PASSTHROUGH: "true" },
			},
		}),
	],
});
