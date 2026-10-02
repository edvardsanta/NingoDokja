import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

// The renderer only talks to the main process over IPC, never to the network, so the built page
// says so. The development server needs inline scripts and a socket, so it gets no policy.
const POLICY = [
  "default-src 'none'",
  "script-src 'self'",
  "style-src 'self'",
  "img-src 'self' data:",
  "font-src 'self'",
  "base-uri 'none'",
  "form-action 'none'",
].join("; ");

function contentSecurityPolicy(): Plugin {
  return {
    name: "dokja-content-security-policy",
    apply: "build",
    transformIndexHtml: () => [
      {
        tag: "meta",
        attrs: { "http-equiv": "Content-Security-Policy", content: POLICY },
        injectTo: "head-prepend",
      },
    ],
  };
}

export default defineConfig({
  root: "src/renderer",
  base: "./",
  plugins: [react(), contentSecurityPolicy()],
  build: { outDir: "../../dist/renderer", emptyOutDir: true, target: "chrome150" },
});
