import test from "node:test";
import assert from "node:assert/strict";

import { isSameDocument, isTrustedSender, shouldBlockRequest } from "../src/main/guards.js";

test("the renderer may not load anything from the network", () => {
  for (const url of [
    "https://assets.example/logo.png",
    "http://assets.example/",
    "ws://assets.example/socket",
    "wss://assets.example/socket",
    "ftp://assets.example/file",
    "http://localhost:5173/",
    "not a url",
    "",
  ]) {
    assert.equal(shouldBlockRequest(url), true, url);
  }
});

test("local content is not blocked", () => {
  for (const url of [
    "file:///opt/app/dist/renderer/index.html",
    "file:///opt/app/dist/renderer/assets/index.js",
    "data:image/png;base64,AAAA",
    "blob:file:///0b1c",
    "devtools://devtools/bundled/inspector.html",
  ]) {
    assert.equal(shouldBlockRequest(url), false, url);
  }
});

test("only the configured development server is let through", () => {
  const dev = "http://localhost:5173";
  assert.equal(shouldBlockRequest("http://localhost:5173/src/main.tsx", dev), false);
  assert.equal(shouldBlockRequest("ws://localhost:5173/", dev), false);
  assert.equal(shouldBlockRequest("http://localhost:9999/", dev), true);
  assert.equal(shouldBlockRequest("https://assets.example/", dev), true);
});

test("IPC is trusted only from the page the app loaded", () => {
  const app = "file:///opt/app/dist/renderer/index.html";

  assert.equal(isTrustedSender(app, app), true);
  assert.equal(isTrustedSender(`${app}#cards`, app), true);
  assert.equal(isTrustedSender(`${app}?x=1`, app), true);

  assert.equal(isTrustedSender(undefined, app), false);
  assert.equal(isTrustedSender("", app), false);
  assert.equal(isTrustedSender("file:///opt/app/dist/renderer/other.html", app), false);
  assert.equal(isTrustedSender("file:///elsewhere/index.html", app), false);
  assert.equal(isTrustedSender("https://assets.example/index.html", app), false);
});

test("a development page is trusted by host and path", () => {
  const dev = "http://localhost:5173/";
  assert.equal(isSameDocument("http://localhost:5173/", dev), true);
  assert.equal(isSameDocument("http://localhost:5174/", dev), false);
  assert.equal(isSameDocument("http://localhost:5173/admin", dev), false);
});
