import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import test from "node:test";

import { startStatusServer, statusReply, type LinkControls } from "../runtime/status_server.js";
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

test("any other path or method is not found, and the link routes do not exist without link controls", async () => {
  await withServer(() => ({ state: "connected", detail: "" }), async (base) => {
    assert.equal((await fetch(`${base}/deliver`)).status, 404);
    assert.equal((await fetch(`${base}/link`)).status, 404);
    assert.equal((await fetch(`${base}/link/reset`, { method: "POST" })).status, 404);
    assert.equal((await fetch(`${base}/status`, { method: "POST" })).status, 404);
  });
});

async function withLink(
  state: () => RuntimeStatus,
  link: LinkControls,
  run: (base: string) => Promise<void>,
) {
  const server = startStatusServer({ port: 0, status: state, link });
  await new Promise<void>((resolve) => server.once("listening", resolve));
  try {
    const { port } = server.address() as AddressInfo;
    await run(`http://127.0.0.1:${port}`);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

test("the link route gives the code as rows of modules only while the account waits for it", async () => {
  let current: RuntimeStatus = { state: "waiting_for_link", detail: "" };
  let code: string | undefined = "2@ref,noise,identity,adv";
  await withLink(() => current, { code: () => code, start: () => undefined, reset: async () => undefined }, async (base) => {
    const waiting = (await (await fetch(`${base}/link`)).json()) as { state: string; matrix?: string[] };
    assert.equal(waiting.state, "waiting_for_link");
    assert.ok(waiting.matrix && waiting.matrix.length >= 21);
    assert.ok(waiting.matrix.every((row) => /^[01]+$/.test(row) && row.length === waiting.matrix!.length));
    assert.ok(!JSON.stringify(waiting).includes("noise"), "the code itself never leaves, only the rows");

    code = undefined;
    assert.deepEqual(await (await fetch(`${base}/link`)).json(), { state: "waiting_for_link" });

    current = { state: "connected", detail: "" };
    code = "2@stale";
    assert.deepEqual(await (await fetch(`${base}/link`)).json(), { state: "connected" });
  });
});

test("starting a link and resetting the session call the controls and answer the state", async () => {
  const calls: string[] = [];
  await withLink(
    () => ({ state: "connecting", detail: "" }),
    {
      code: () => "2@code",
      start: () => void calls.push("start"),
      reset: async () => void calls.push("reset"),
    },
    async (base) => {
      assert.deepEqual(await (await fetch(`${base}/link/start`, { method: "POST" })).json(), { state: "connecting" });
      assert.deepEqual(await (await fetch(`${base}/link/reset`, { method: "POST" })).json(), { state: "connecting" });
      assert.equal((await fetch(`${base}/link/start`)).status, 404, "a change is never a GET");
      assert.equal((await fetch(`${base}/link/reset`)).status, 404);
    },
  );

  assert.deepEqual(calls, ["start", "reset"]);
});

test("a reset that fails answers an error without saying why", async () => {
  await withLink(
    () => ({ state: "connected", detail: "" }),
    {
      code: () => undefined,
      start: () => undefined,
      reset: async () => {
        throw new Error("EACCES: /auth/default/creds.json");
      },
    },
    async (base) => {
      const answer = await fetch(`${base}/link/reset`, { method: "POST" });
      assert.equal(answer.status, 500);
      assert.ok(!(await answer.text()).includes("creds.json"));
    },
  );
});
