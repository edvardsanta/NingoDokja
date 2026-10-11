import assert from "node:assert/strict";
import test from "node:test";

import type { WhatsAppMessageContext } from "../messages/message_handler.js";
import type { InboundMessage } from "../runtime/inbound.js";
import {
  createWhatsAppRuntime,
  disconnectReason,
  type AuthBundle,
  type SocketEvents,
  type WhatsAppRuntime,
  type WhatsAppSocketLike,
} from "../runtime/whatsapp_runtime.js";

type Listener = (update: never) => void;

class FakeSocket implements WhatsAppSocketLike {
  listeners: Record<string, Listener[]> = {};
  ended = false;
  sendCalls: Array<{ jid: string; text: string; options?: { quoted?: unknown; messageId?: string } }> = [];
  presenceCalls: Array<[string, string | undefined]> = [];
  user = { id: "5511900000001:4@s.whatsapp.net", lid: "99887766554433:4@lid" };
  ev = {
    on: <E extends keyof SocketEvents>(event: E, listener: (update: SocketEvents[E]) => void) => {
      (this.listeners[event] ??= []).push(listener as Listener);
    },
  };

  emit<E extends keyof SocketEvents>(event: E, update: SocketEvents[E]) {
    for (const listener of this.listeners[event] ?? []) {
      listener(update as never);
    }
  }
  close(code?: number) {
    this.emit("connection.update", {
      connection: "close",
      lastDisconnect: { error: code === undefined ? undefined : { output: { statusCode: code } } },
    });
  }
  async sendMessage(jid: string, content: { text: string }, options?: { quoted?: unknown; messageId?: string }) {
    this.sendCalls.push({ jid, text: content.text, options });
    // Like Baileys: the message as it went out, with the id it was given.
    return { key: { id: options?.messageId } };
  }
  async sendPresenceUpdate(type: "composing" | "paused", jid?: string) {
    this.presenceCalls.push([type, jid]);
  }
  loggedOut = false;
  // Like Baileys: logging out closes the connection with the "logged out" status.
  async logout() {
    this.loggedOut = true;
    this.close(disconnectReason.loggedOut);
  }
  end() {
    this.ended = true;
    this.close();
  }
}

type Script = (socket: FakeSocket, auth: AuthBundle) => void;

function harness(
  scripts: Script[],
  options: {
    linked?: boolean;
    handler?: (context: WhatsAppMessageContext) => Promise<void>;
    recorder?: Parameters<typeof createWhatsAppRuntime>[1]["recorder"];
    newMessageId?: ((userJid?: string) => string) | null;
  } = {},
) {
  const sockets: FakeSocket[] = [];
  const wiped: string[] = [];
  const saved: number[] = [];
  const sleeps: number[] = [];
  const qrs: string[] = [];
  const logs: string[] = [];
  const errors: string[] = [];
  let saveFailure: Error | null = null;
  const queue = [...scripts];

  const runtime = createWhatsAppRuntime(
    { orchestratorEndpoint: "http://o", orchestratorTimeoutMs: 1000, selfChat: true, allowedNumbers: [], allowedGroups: [], authDir: "/auth/test" },
    {
      loadAuth: async () => ({
        state: { creds: { me: options.linked === false ? undefined : { id: "me" } } },
        saveCreds: async () => {
          if (saveFailure) {
            throw saveFailure;
          }
          saved.push(saved.length);
        },
      }),
      wipeAuth: async (dir) => {
        wiped.push(dir);
      },
      connect: (auth) => {
        const socket = new FakeSocket();
        sockets.push(socket);
        const script = queue.shift();
        setImmediate(() => script?.(socket, auth));
        return socket;
      },
      renderQr: (qr) => {
        qrs.push(qr);
      },
      handler: options.handler,
      recorder: options.recorder,
      newMessageId: options.newMessageId === null ? undefined : (options.newMessageId ?? (() => "BOTID")),
      sleep: async (ms) => {
        sleeps.push(ms);
      },
      log: (...args) => logs.push(args.join(" ")),
      error: (...args) => errors.push(args.join(" ")),
    },
  );
  return { runtime, sockets, wiped, saved, sleeps, qrs, logs, errors, failSaves: (error: Error) => (saveFailure = error) };
}

const open = (socket: FakeSocket) => socket.emit("connection.update", { connection: "open" });

test("the link code is drawn and the log only says that the link is waited for", async () => {
  let seen = "";
  const { runtime, qrs, logs } = harness([
    (socket) => {
      socket.emit("connection.update", { qr: "SECRET-QR-PAYLOAD" });
      seen = runtime.status().state;
      runtime.stop();
    },
  ], { linked: false });

  await runtime.run();

  assert.deepEqual(qrs, ["SECRET-QR-PAYLOAD"]);
  assert.equal(seen, "waiting_for_link");
  assert.doesNotMatch(logs.join("\n"), /SECRET-QR-PAYLOAD/);
  assert.match(logs.join("\n"), /waiting for the link/);
});

test("a message that just arrived reaches the handler, history does not", async () => {
  const handled: WhatsAppMessageContext[] = [];
  const message: InboundMessage = {
    key: { id: "m1", remoteJid: "5511987654321@s.whatsapp.net", fromMe: false },
    message: { conversation: "hello" },
  };
  const { runtime } = harness([
    (socket) => {
      open(socket);
      socket.emit("messages.upsert", { type: "append", messages: [message] });
      socket.emit("messages.upsert", { type: "notify", messages: [{ ...message, key: { ...message.key, id: "m2" } }] });
      setTimeout(() => runtime.stop(), 20);
    },
  ], { handler: async (context) => void handled.push(context) });

  await runtime.run();

  assert.deepEqual(handled.map((context) => context.messageId), ["m2"]);
});

test("a reply goes out through the socket with its id remembered, and typing uses presence", async () => {
  const { runtime, sockets } = harness([
    (socket) => {
      open(socket);
      socket.emit("messages.upsert", {
        type: "notify",
        messages: [{ key: { id: "m1", remoteJid: "120363000000000001@g.us", participant: "5511987654321@s.whatsapp.net" }, message: { conversation: "hi" } }],
      });
      setTimeout(() => runtime.stop(), 20);
    },
  ], {
    handler: async (context) => {
      await context.typing(true);
      await context.reply("answer");
    },
  });

  await runtime.run();

  assert.deepEqual(sockets[0].presenceCalls, [["composing", "120363000000000001@g.us"]]);
  assert.equal(sockets[0].sendCalls[0].jid, "120363000000000001@g.us");
  assert.equal(sockets[0].sendCalls[0].text, "answer");
  assert.equal(sockets[0].sendCalls[0].options?.messageId, "BOTID");
  assert.ok(sockets[0].sendCalls[0].options?.quoted);
});

test("an unreadable message or a failing handler never stops the connection", async () => {
  const { runtime, errors, sockets } = harness([
    (socket) => {
      open(socket);
      socket.emit("messages.upsert", { type: "notify", messages: [{ key: {} }] });
      socket.emit("messages.upsert", {
        type: "notify",
        messages: [{ key: { id: "m1", remoteJid: "5511987654321@s.whatsapp.net" }, message: { conversation: "x" } }],
      });
      setTimeout(() => runtime.stop(), 20);
    },
  ], {
    handler: async () => {
      throw new Error("boom");
    },
  });

  await runtime.run();

  assert.equal(sockets.length, 1);
  assert.match(errors.join("\n"), /unreadable message/);
  assert.match(errors.join("\n"), /handler failed/);
});

test("the session is saved after every credentials update, one write at a time", async () => {
  const { runtime, saved } = harness([
    (socket) => {
      socket.emit("creds.update", {});
      socket.emit("creds.update", {});
      setTimeout(() => runtime.stop(), 20);
    },
  ]);

  await runtime.run();

  assert.equal(saved.length, 2);
});

test("a session that cannot be saved is reported and the connection goes on", async () => {
  const { runtime, errors, failSaves } = harness([
    (socket) => {
      failSaves(new Error("disk full"));
      socket.emit("creds.update", {});
      setTimeout(() => runtime.stop(), 20);
    },
  ]);

  await runtime.run();

  assert.match(errors.join("\n"), /could not save the session.*disk full/);
});

test("linking ends with a restart request, which reconnects at once without waiting", async () => {
  const { runtime, sockets, sleeps } = harness([
    (socket) => socket.close(disconnectReason.restartRequired),
    (socket) => {
      open(socket);
      runtime.stop();
    },
  ]);

  await runtime.run();

  assert.equal(sockets.length, 2);
  assert.deepEqual(sleeps, []);
});

test("a lost connection is retried with a growing wait that resets once it is open again", async () => {
  const { runtime, sockets, sleeps } = harness([
    (socket) => socket.close(408),
    (socket) => socket.close(408),
    (socket) => {
      open(socket);
      socket.close(408);
    },
    (socket) => socket.close(408),
    (socket) => {
      open(socket);
      runtime.stop();
    },
  ]);

  await runtime.run();

  assert.equal(sockets.length, 5);
  // 1s, 2s; the third connection opened, so the wait starts over: 1s, then 2s again.
  assert.deepEqual(sleeps, [1000, 2000, 1000, 2000]);
});

test("the wait between retries is capped", async () => {
  const closing: Script = (socket) => socket.close(408);
  const { runtime, sleeps } = harness([
    ...Array.from({ length: 9 }, () => closing),
    (socket) => {
      open(socket);
      runtime.stop();
    },
  ]);

  await runtime.run();

  assert.equal(Math.max(...sleeps), 60_000);
});

test("a session logged out from the phone is reported and the service keeps running", async () => {
  let seen: ReturnType<WhatsAppRuntime["status"]> | undefined;
  const { runtime, sockets, sleeps, logs } = harness([
    (socket) => {
      socket.close(disconnectReason.loggedOut);
      setTimeout(() => {
        seen = runtime.status();
        runtime.stop();
      }, 20);
    },
  ]);

  await runtime.run();

  assert.equal(seen?.state, "logged_out");
  assert.match(seen?.detail ?? "", /link again/);
  assert.equal(sockets.length, 1);
  assert.deepEqual(sleeps, []);
  assert.match(logs.join("\n"), /"whatsapp":"logged_out"/);
});

test("a session taken over by another connection is reported instead of fought for", async () => {
  let seen: ReturnType<WhatsAppRuntime["status"]> | undefined;
  const { runtime, sockets } = harness([
    (socket) => {
      socket.close(disconnectReason.connectionReplaced);
      setTimeout(() => {
        seen = runtime.status();
        runtime.stop();
      }, 20);
    },
  ]);

  await runtime.run();

  assert.equal(seen?.state, "replaced");
  assert.equal(sockets.length, 1);
});

test("a link code nobody scanned is followed by a new one, less and less often", async () => {
  const unscanned: Script = (socket) => {
    socket.emit("connection.update", { qr: "QR" });
    socket.close(408);
  };
  const { runtime, sockets, sleeps, logs } = harness([
    unscanned,
    unscanned,
    unscanned,
    (socket) => {
      socket.emit("connection.update", { qr: "QR" });
      runtime.stop();
    },
  ], { linked: false });

  await runtime.run();

  assert.equal(sockets.length, 4);
  assert.deepEqual(sleeps, [5000, 10_000, 20_000]);
  assert.match(logs.join("\n"), /"whatsapp":"link code expired","status":"408","newCodeInMs":5000/);
});

test("a client WhatsApp refuses before any link code is reported with the status, and can be retried", async () => {
  let seen: ReturnType<WhatsAppRuntime["status"]> | undefined;
  const refused: Script = (socket) => socket.close(405);
  const { runtime, sockets, sleeps } = harness([
    refused,
    refused,
    refused,
    refused,
    (socket) => {
      socket.close(405);
      setTimeout(() => {
        seen = runtime.status();
        runtime.restart();
      }, 20);
    },
    () => runtime.stop(),
  ], { linked: false });

  await runtime.run();

  assert.equal(seen?.state, "refused");
  assert.match(seen?.detail ?? "", /5 times .*status 405.*upgrade/);
  assert.equal(sockets.length, 6);
  assert.deepEqual(sleeps, [1000, 2000, 4000, 8000]);
});

test("restart does nothing while the service is not waiting", async () => {
  const { runtime, sockets } = harness([
    (socket) => {
      open(socket);
      runtime.restart();
      setTimeout(() => runtime.stop(), 10);
    },
  ]);

  await runtime.run();

  assert.equal(sockets.length, 1);
});

test("the state follows the connection and ends off", async () => {
  const seen: string[] = [];
  const { runtime } = harness([
    (socket) => {
      seen.push(runtime.status().state);
      open(socket);
      seen.push(runtime.status().state);
      runtime.stop();
    },
  ]);

  await runtime.run();
  seen.push(runtime.status().state);

  assert.deepEqual(seen, ["connecting", "connected", "off"]);
});

test("a few failures before the first link code are retried, as a network may not be ready yet", async () => {
  const { runtime, sockets } = harness([
    (socket) => socket.close(408),
    (socket) => socket.close(),
    (socket) => {
      socket.emit("connection.update", { qr: "QR" });
      runtime.stop();
    },
  ], { linked: false });

  await runtime.run();

  assert.equal(sockets.length, 3);
});

test("the reconnecting log tells one failure from another by its status", async () => {
  const { runtime, logs } = harness([
    (socket) => socket.close(408),
    (socket) => socket.close(),
    (socket) => {
      open(socket);
      runtime.stop();
    },
  ]);

  await runtime.run();

  const reconnects = logs.filter((line) => line.includes("connection lost")).map((line) => JSON.parse(line).status);
  assert.deepEqual(reconnects, ["408", "unknown"]);
});

test("message ids are built from the account's own id, or look like the official client's when none is given", async () => {
  const reply = (socket: FakeSocket) => {
    open(socket);
    socket.emit("messages.upsert", {
      type: "notify",
      messages: [{ key: { id: "m1", remoteJid: "5511987654321@s.whatsapp.net" }, message: { conversation: "hi" } }],
    });
    setTimeout(() => runtime.stop(), 20);
  };
  const handler = async (context: WhatsAppMessageContext) => void (await context.reply("answer"));
  const injected = harness([reply], { handler, newMessageId: (jid) => `ID-FOR-${jid}` });
  const fallback = harness([reply], { handler, newMessageId: null });
  const runtime = { stop: () => (injected.runtime.stop(), fallback.runtime.stop()) };

  await Promise.all([injected.runtime.run(), fallback.runtime.run()]);

  assert.equal(injected.sockets[0].sendCalls[0].options?.messageId, "ID-FOR-5511900000001:4@s.whatsapp.net");
  assert.match(fallback.sockets[0].sendCalls[0].options?.messageId ?? "", /^3EB0[0-9A-F]{18}$/);
});

test("a failure while loading or connecting is retried", async () => {
  let calls = 0;
  const sleeps: number[] = [];
  const errors: string[] = [];
  const socket = new FakeSocket();
  const runtime = createWhatsAppRuntime(
    { orchestratorEndpoint: "http://o", orchestratorTimeoutMs: 1000, selfChat: true, allowedNumbers: [], allowedGroups: [], authDir: "/auth/test" },
    {
      loadAuth: async () => {
        calls += 1;
        if (calls === 1) {
          throw new Error("unreadable folder");
        }
        return { state: { creds: { me: {} } }, saveCreds: async () => undefined };
      },
      wipeAuth: async () => undefined,
      connect: () => {
        setImmediate(() => runtime.stop());
        return socket;
      },
      renderQr: () => undefined,
      sleep: async (ms) => void sleeps.push(ms),
      error: (...args) => errors.push(args.join(" ")),
    },
  );

  await runtime.run();

  assert.equal(calls, 2);
  assert.deepEqual(sleeps, [1000]);
  assert.match(errors.join("\n"), /could not connect.*unreadable folder/);
});

test("stop ends the socket and lets run finish", async () => {
  const { runtime, sockets } = harness([(socket) => {
    open(socket);
    setTimeout(() => runtime.stop(), 5);
  }]);

  await runtime.run();

  assert.equal(sockets[0].ended, true);
});

test("the link code is offered only while the account waits for it, and goes when it is linked", async () => {
  const seen: Array<string | undefined> = [];
  const { runtime } = harness([
    (socket) => {
      seen.push(runtime.linkCode());
      socket.emit("connection.update", { qr: "CODE-1" });
      seen.push(runtime.linkCode());
      socket.emit("connection.update", { qr: "CODE-2" });
      seen.push(runtime.linkCode());
      open(socket);
      seen.push(runtime.linkCode());
      runtime.stop();
    },
  ], { linked: false });

  await runtime.run();

  assert.deepEqual(seen, [undefined, "CODE-1", "CODE-2", undefined]);
});

test("a session reset while waiting for the link erases the session and asks for a new code", async () => {
  const seen: Array<string | undefined> = [];
  const { runtime, sockets, wiped } = harness([
    (socket) => {
      socket.emit("connection.update", { qr: "CODE-1" });
      setTimeout(() => void runtime.resetSession(), 10);
    },
    (socket) => {
      socket.emit("connection.update", { qr: "CODE-2" });
      seen.push(runtime.linkCode());
      runtime.stop();
    },
  ], { linked: false });

  await runtime.run();

  assert.deepEqual(wiped, ["/auth/test"]);
  assert.equal(sockets.length, 2);
  assert.deepEqual(seen, ["CODE-2"]);
  assert.equal(sockets[0].loggedOut, false, "an account that was never linked has nothing to log out of");
});

test("a session reset after a logout starts over without logging out again", async () => {
  let seen: ReturnType<WhatsAppRuntime["status"]> | undefined;
  const { runtime, sockets, wiped } = harness([
    (socket) => {
      socket.close(disconnectReason.loggedOut);
      setTimeout(() => {
        seen = runtime.status();
        void runtime.resetSession();
      }, 10);
    },
    (socket) => {
      socket.emit("connection.update", { qr: "CODE-1" });
      runtime.stop();
    },
  ], { linked: false });

  await runtime.run();

  assert.equal(seen?.state, "logged_out");
  assert.deepEqual(wiped, ["/auth/test"]);
  assert.equal(sockets.length, 2);
  assert.equal(sockets[0].loggedOut, false);
});

test("a session reset while connected unlinks the device on WhatsApp's side first", async () => {
  const { runtime, sockets, wiped } = harness([
    (socket) => {
      open(socket);
      setTimeout(() => void runtime.resetSession(), 10);
    },
    (socket) => {
      socket.emit("connection.update", { qr: "CODE-1" });
      runtime.stop();
    },
  ]);

  await runtime.run();

  assert.equal(sockets[0].loggedOut, true);
  assert.deepEqual(wiped, ["/auth/test"]);
  assert.equal(sockets.length, 2);
});

test("a session that cannot be erased is reported and the loop goes on", async () => {
  const errors: string[] = [];
  const socket = new FakeSocket();
  let connections = 0;
  const runtime = createWhatsAppRuntime(
    { orchestratorEndpoint: "http://o", orchestratorTimeoutMs: 1000, selfChat: true, allowedNumbers: [], allowedGroups: [], authDir: "/auth/test" },
    {
      loadAuth: async () => ({ state: { creds: { me: undefined } }, saveCreds: async () => undefined }),
      wipeAuth: async () => {
        throw new Error("read-only folder");
      },
      connect: () => {
        connections += 1;
        if (connections === 1) {
          setTimeout(() => void runtime.resetSession(), 10);
        } else {
          setImmediate(() => runtime.stop());
        }
        return socket;
      },
      renderQr: () => undefined,
      error: (...args) => errors.push(args.join(" ")),
    },
  );

  await runtime.run();

  assert.equal(connections, 2);
  assert.match(errors.join("\n"), /could not erase the session.*read-only folder/);
});

test("the recorder hears every message that arrives and what the bot sends, with the id it went out with", async () => {
  const heard: Array<[string, string]> = [];
  const { runtime } = harness([
    (socket) => {
      open(socket);
      socket.emit("messages.upsert", {
        type: "notify",
        messages: [{ key: { id: "m1", remoteJid: "5511987654321@s.whatsapp.net" }, message: { conversation: "hello" } }],
      });
      setTimeout(() => runtime.stop(), 30);
    },
  ], {
    handler: async (context) => {
      await context.reply("an answer");
    },
    recorder: {
      inbound: async (context) => void heard.push(["in", context.text]),
      outbound: async (_context, text, id) => void heard.push(["out", `${text}|${id}`]),
    },
  });

  await runtime.run();

  assert.deepEqual(heard, [["in", "hello"], ["out", "an answer|BOTID"]]);
});
