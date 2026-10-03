import test from "node:test";
import assert from "node:assert/strict";

import { parseConfig, parseDuration } from "../src/main/config.js";

test("defaults to the local orchestrator, English and a 15 s request", () => {
  assert.deepEqual(parseConfig([], {}, "en-US"), {
    endpoint: "tcp://127.0.0.1:5558",
    locale: "en",
    defaultTimeoutMs: 15_000,
    maxTimeoutMs: 120_000,
  });
});

test("the endpoint comes from the flag, then the environment", () => {
  const env = { DOKJA_ORCH_REQUEST_ENDPOINT: "tcp://orchestrator.test:5558" };
  assert.equal(parseConfig([], env, "en").endpoint, "tcp://orchestrator.test:5558");
  assert.equal(
    parseConfig(["--request-endpoint=tcp://flag.test:1"], env, "en").endpoint,
    "tcp://flag.test:1",
  );
  assert.equal(
    parseConfig(["--request-endpoint", "tcp://flag.test:2"], env, "en").endpoint,
    "tcp://flag.test:2",
  );
});

test("the locale follows the flag, DOKJA_LANG, the locale variables and then the system", () => {
  assert.equal(parseConfig(["--lang=pt"], { DOKJA_LANG: "en" }, "en").locale, "pt");
  assert.equal(parseConfig([], { DOKJA_LANG: "pt-BR", LANG: "en_US.UTF-8" }, "en").locale, "pt");
  assert.equal(parseConfig([], { LC_ALL: "en_US.UTF-8", LANG: "pt_BR.UTF-8" }, "pt").locale, "en");
  assert.equal(parseConfig([], { LANG: "pt_BR.UTF-8" }, "en").locale, "pt");
  assert.equal(parseConfig([], {}, "pt-BR").locale, "pt");
  assert.equal(parseConfig([], { LANG: "C.UTF-8" }, "pt-BR").locale, "en");
  assert.equal(parseConfig(["--lang=fr"], {}, "pt-BR").locale, "en");
  assert.equal(parseConfig([], { DOKJA_LANG: "  " }, "en").locale, "en");
});

test("--timeout is the longest a request may wait", () => {
  assert.equal(parseConfig([], {}, "en").maxTimeoutMs, 120_000);
  assert.deepEqual(
    pick(parseConfig(["--timeout=30s"], {}, "en")),
    { maxTimeoutMs: 30_000, defaultTimeoutMs: 15_000 },
  );
  assert.deepEqual(
    pick(parseConfig(["--timeout=5s"], {}, "en")),
    { maxTimeoutMs: 5_000, defaultTimeoutMs: 5_000 },
  );
  assert.equal(parseConfig(["--timeout=soon"], {}, "en").maxTimeoutMs, 120_000);
});

test("flags from Electron and Chromium are ignored", () => {
  const config = parseConfig(["--no-sandbox", "--ozone-platform=wayland", "--lang=pt"], {}, "en");
  assert.equal(config.locale, "pt");
  assert.equal(config.endpoint, "tcp://127.0.0.1:5558");
});

test("a development server is accepted only on this machine", () => {
  assert.equal(
    parseConfig([], { DOKJA_DESKTOP_DEV_URL: "http://localhost:5173" }, "en").devServerUrl,
    "http://localhost:5173",
  );
  for (const url of ["https://assets.example/", "file:///opt/app/index.html", "javascript:alert(1)", "nonsense"]) {
    assert.equal(parseConfig([], { DOKJA_DESKTOP_DEV_URL: url }, "en").devServerUrl, undefined, url);
  }
});

test("parseDuration reads milliseconds, seconds and minutes", () => {
  assert.equal(parseDuration("1500"), 1500);
  assert.equal(parseDuration("1500ms"), 1500);
  assert.equal(parseDuration("30s"), 30_000);
  assert.equal(parseDuration("2m"), 120_000);
  assert.equal(parseDuration(" 1.5s "), 1500);
  for (const value of ["", "0", "-3s", "abc", "5h", undefined]) {
    assert.equal(parseDuration(value), undefined, String(value));
  }
});

function pick(config: ReturnType<typeof parseConfig>) {
  return { maxTimeoutMs: config.maxTimeoutMs, defaultTimeoutMs: config.defaultTimeoutMs };
}
