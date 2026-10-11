import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import test from "node:test";

import { startStatusServer, statusReply, type ConversationControls, type LinkControls } from "../runtime/status_server.js";
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

async function withConversations(
  controls: ConversationControls | undefined,
  run: (base: string) => Promise<void>,
) {
  const server = startStatusServer({ port: 0, status: () => ({ state: "connected", detail: "" }), conversations: controls });
  await new Promise<void>((resolve) => server.once("listening", resolve));
  try {
    const { port } = server.address() as AddressInfo;
    await run(`http://127.0.0.1:${port}`);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

function fakeConversations(): ConversationControls & { asked: Array<[string, number | undefined]> } {
  const asked: Array<[string, number | undefined]> = [];
  return {
    asked,
    list: () => [
      { alias: "friend", kind: "direct" },
      { alias: "team", kind: "group" },
    ],
    retentionDays: 14,
    page: async (alias, before) => {
      asked.push([alias, before]);
      return {
        messages: [
          { seq: 7, id: "3EB0A", at: "2026-10-11T12:00:00.000Z", who: "owner", text: "hi" },
          { seq: 8, id: "3EB0B", at: "2026-10-11T12:01:00.000Z", who: "bot", text: "hello" },
        ],
        before: "7",
      };
    },
  };
}

test("the named conversations are listed by alias with the retention, and nothing else about them", async () => {
  await withConversations(fakeConversations(), async (base) => {
    const listed = await (await fetch(`${base}/conversations`)).json();

    assert.deepEqual(listed, {
      conversations: [
        { alias: "friend", kind: "direct", retention_days: 14 },
        { alias: "team", kind: "group", retention_days: 14 },
      ],
    });
  });
});

test("a page of history has the shape of a page of Discord messages, with labels for who wrote", async () => {
  const controls = fakeConversations();
  await withConversations(controls, async (base) => {
    const page = await (await fetch(`${base}/history?alias=friend&before=12`)).json();

    assert.deepEqual(page, {
      alias: "friend",
      messages: [
        { id: "7", author: "owner", bot: false, content: "hi", timestamp: "2026-10-11T12:00:00.000Z", edited: false, reply_to: "", attachments: [] },
        { id: "8", author: "bot", bot: true, content: "hello", timestamp: "2026-10-11T12:01:00.000Z", edited: false, reply_to: "", attachments: [] },
      ],
      before: "7",
    });
    assert.deepEqual(controls.asked, [["friend", 12]]);
  });
});

test("history refuses an alias that is not named and a cursor that is not a number, before reading", async () => {
  const controls = fakeConversations();
  await withConversations(controls, async (base) => {
    assert.equal((await fetch(`${base}/history?alias=stranger`)).status, 404);
    assert.equal((await fetch(`${base}/history?alias=../friend`)).status, 404);
    assert.equal((await fetch(`${base}/history`)).status, 404);
    assert.equal((await fetch(`${base}/history?alias=friend&before=1;drop`)).status, 400);
    assert.equal((await fetch(`${base}/history?alias=friend`, { method: "POST" })).status, 404);
  });
  assert.deepEqual(controls.asked, []);
});

test("without named conversations the routes do not exist", async () => {
  await withConversations(undefined, async (base) => {
    assert.equal((await fetch(`${base}/conversations`)).status, 404);
    assert.equal((await fetch(`${base}/history?alias=friend`)).status, 404);
  });
});

async function withDeliver(
  deliver: (alias: string, text: string) => Promise<"sent" | "not_named" | "not_connected" | "limited" | "failed">,
  run: (post: (body: unknown) => Promise<Response>) => Promise<void>,
) {
  const server = startStatusServer({ port: 0, status: () => ({ state: "connected", detail: "" }), deliver });
  await new Promise<void>((resolve) => server.once("listening", resolve));
  const { port } = server.address() as AddressInfo;
  const base = `http://127.0.0.1:${port}`;
  try {
    await run((body) => fetch(`${base}/deliver`, { method: "POST", body: typeof body === "string" ? body : JSON.stringify(body) }));
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

test("a delivery names a conversation by alias, and the answer says only whether it went out", async () => {
  const asked: Array<[string, string]> = [];
  await withDeliver(async (alias, text) => {
    asked.push([alias, text]);
    return "sent";
  }, async (post) => {
    const answer = await post({ alias: "friend", content: "  hello there  ", jid: "5511987654321@s.whatsapp.net" });

    assert.equal(answer.status, 200);
    assert.deepEqual(await answer.json(), { status: "ok", alias: "friend" });
  });

  assert.deepEqual(asked, [["friend", "hello there"]]);
});

test("every outcome that is not a send has its own status and a fixed sentence", async () => {
  for (const [outcome, code] of [["not_named", 404], ["not_connected", 409], ["limited", 429], ["failed", 502]] as const) {
    await withDeliver(async () => outcome, async (post) => {
      const answer = await post({ alias: "friend", content: "hello" });
      assert.equal(answer.status, code, outcome);
      const text = await answer.text();
      assert.ok(!text.includes("friend") && !text.includes("hello"), "nothing of the request is echoed");
    });
  }
});

test("a delivery with a bad alias, no text, too much text or a body that is not JSON is refused unread", async () => {
  const asked: string[] = [];
  await withDeliver(async (alias) => {
    asked.push(alias);
    return "sent";
  }, async (post) => {
    for (const body of [
      { alias: "../friend", content: "x" },
      { alias: "Friend", content: "x" },
      { alias: "friend", content: "   " },
      { alias: "friend", content: "x".repeat(2001) },
      { alias: "friend", content: "a\u0000b" },
      { alias: "friend" },
      "not json",
      "[]",
    ]) {
      assert.equal((await post(body)).status, 400, JSON.stringify(body).slice(0, 40));
    }
    assert.equal((await post("x".repeat(20_000))).status, 413);
  });

  assert.deepEqual(asked, []);
});

test("without a way to deliver the route does not exist", async () => {
  await withConversations(undefined, async (base) => {
    assert.equal((await fetch(`${base}/deliver`, { method: "POST", body: "{}" })).status, 404);
  });
});
