import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import test from "node:test";

import { startStatusServer, statusReply } from "../runtime/status_server.js";
import type { InterfaceState, RuntimeStatus } from "../runtime/whatsapp_runtime.js";

test("each state maps to a level and carries nothing else", () => {
  const levels: Record<InterfaceState, string> = {
    connected: "ok",
    connecting: "degraded",
    waiting_for_link: "error",
    logged_out: "error",
    replaced: "error",
    refused: "error",
    off: "error",
  };

  for (const [state, status] of Object.entries(levels)) {
    const reply = statusReply({ state: state as InterfaceState, detail: "a sentence" });
    assert.deepEqual(Object.keys(reply).sort(), ["detail", "state", "status"]);
    assert.equal(reply.status, status);
    assert.equal(reply.state, state);
  }
});

async function withServer(status: () => RuntimeStatus, run: (base: string) => Promise<void>) {
  const server = startStatusServer({ port: 0, status });
  await new Promise<void>((resolve) => server.once("listening", resolve));
  try {
    const { port } = server.address() as AddressInfo;
    await run(`http://127.0.0.1:${port}`);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

test("the listener answers the current state and a liveness check", async () => {
  let current: RuntimeStatus = { state: "waiting_for_link", detail: "waiting for the account to be linked" };

  await withServer(() => current, async (base) => {
    const first = await fetch(`${base}/status`);
    assert.equal(first.status, 200);
    assert.deepEqual(await first.json(), {
      status: "error",
      state: "waiting_for_link",
      detail: "waiting for the account to be linked",
    });

    current = { state: "connected", detail: "" };
    assert.deepEqual(await (await fetch(`${base}/status`)).json(), { status: "ok", state: "connected", detail: "" });

    assert.deepEqual(await (await fetch(`${base}/health`)).json(), { status: "ok" });
  });
});

test("any other path or method is not found", async () => {
  await withServer(() => ({ state: "connected", detail: "" }), async (base) => {
    assert.equal((await fetch(`${base}/deliver`)).status, 404);
    assert.equal((await fetch(`${base}/link`)).status, 404);
    assert.equal((await fetch(`${base}/status`, { method: "POST" })).status, 404);
  });
});
