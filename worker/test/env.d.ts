import type { Env as RelayEnv } from "../src/index";

declare global {
	namespace Cloudflare {
		interface Env extends RelayEnv {}
		interface GlobalProps {
			mainModule: typeof import("../src/index");
			durableNamespaces: "Tunnel";
		}
	}
}
