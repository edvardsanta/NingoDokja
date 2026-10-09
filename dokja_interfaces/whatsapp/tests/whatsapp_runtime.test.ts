import assert from "node:assert/strict";
import test from "node:test";

import type { WhatsAppMessageContext } from "../messages/message_handler.js";
import type { InboundMessage } from "../runtime/inbound.js";
import {
  createWhatsAppRuntime,
  disconnectReason,
  type AuthBundle,
  type SocketEvents,
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
  }
  async sendPresenceUpdate(type: "composing" | "paused", jid?: string) {
    this.presenceCalls.push([type, jid]);
  }
  end() {
    this.ended = true;
    this.close();
  }
}

type Script = (socket: FakeSocket, auth: AuthBundle) => void;

function harness(scripts: Script[], options: { linked?: boolean; handler?: (context: WhatsAppMessageContext) => Promise<void> } = {}) {
  const sockets: FakeSocket[] = [];
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
      newMessageId: () => "BOTID",
      sleep: async (ms) => {
        sleeps.push(ms);
      },
      log: (...args) => logs.push(args.join(" ")),
      error: (...args) => errors.push(args.join(" ")),
    },
  );
  return { runtime, sockets, saved, sleeps, qrs, logs, errors, failSaves: (error: Error) => (saveFailure = error) };
}

const open = (socket: FakeSocket) => socket.emit("connection.update", { connection: "open" });

test("the link code is drawn and the log only says that the link is waited for", async () => {
  const { runtime, qrs, logs } = harness([
    (socket, auth) => {
      (auth.state.creds as { me?: unknown }).me = undefined;
      socket.emit("connection.update", { qr: "SECRET-QR-PAYLOAD" });
      socket.close(disconnectReason.loggedOut);
    },
  ], { linked: false });

  await assert.rejects(runtime.run());

  assert.deepEqual(qrs, ["SECRET-QR-PAYLOAD"]);
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

test("a session logged out from the phone stops the service instead of retrying", async () => {
  const { runtime, sockets, sleeps } = harness([(socket) => socket.close(disconnectReason.loggedOut)]);

  await assert.rejects(runtime.run(), /logged out/);

  assert.equal(sockets.length, 1);
  assert.deepEqual(sleeps, []);
});

test("a session taken over by another connection stops the service instead of fighting for it", async () => {
  const { runtime, sockets } = harness([(socket) => socket.close(disconnectReason.connectionReplaced)]);

  await assert.rejects(runtime.run(), /Another connection/);

  assert.equal(sockets.length, 1);
});

test("a link that was never completed stops the service", async () => {
  const { runtime, sockets } = harness([(socket) => socket.close(408)], { linked: false });

  await assert.rejects(runtime.run(), /not linked in time/);

  assert.equal(sockets.length, 1);
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
