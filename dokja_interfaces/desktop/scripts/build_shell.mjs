import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

// The host daemon is bundled beside Electron. Building never starts it or a container.
if (process.platform !== "win32") {
  mkdirSync("dist/runtime", { recursive:true });
  const result = spawnSync("go", ["build","-o",resolve("dist/runtime/dokja-runtime"),"./cmd/runtime"], { cwd:"../../dokja_orch/runtime",stdio:"inherit" });
  if (result.error || result.status !== 0) throw result.error ?? new Error("runtime daemon build failed");
}

import { build } from "esbuild";

// The Electron main process runs in Node and the preload script in a sandbox, so each is bundled
// into one CommonJS file. zeromq is a native addon and stays outside the bundle.
const common = {
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node24",
  sourcemap: "linked",
  logLevel: "info",
};

await build({
  ...common,
  entryPoints: ["src/main/main.ts"],
  outfile: "dist/main/main.cjs",
  external: ["electron", "zeromq"],
});

await build({
  ...common,
  entryPoints: ["src/preload/preload.ts"],
  outfile: "dist/preload/preload.cjs",
  external: ["electron"],
});
