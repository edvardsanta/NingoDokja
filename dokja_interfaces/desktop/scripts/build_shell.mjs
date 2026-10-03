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
