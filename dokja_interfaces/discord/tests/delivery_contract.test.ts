import assert from "node:assert/strict";
import test from "node:test";

import { parseWebhookMap } from "../config.js";
import { handleDiscordDeliveryRequest } from "../delivery/delivery_server.js";

test("delivery contract accepts content-only payload", async () => {
  let discordBody = "";
  const result = await handleDiscordDeliveryRequest(
    {
      channel_id: "channel-1",
      content: "hello discord",
    },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (input, init) => {
        const url = String(input);
        if (!url.includes("/channels/")) {
          throw new Error(`unexpected fetch ${url}`);
        }
        discordBody = String(init?.body ?? "");
        return new Response(JSON.stringify({ id: "discord-message-1" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.match(discordBody, /hello discord/);
  assert.equal(result.payload.status, "ok");
  assert.equal(result.payload.channel_id, "channel-1");
  assert.equal(result.payload.message_id, "discord-message-1");
  assert.equal(result.payload.has_attachment, false);
});

test("delivery contract accepts attachment-only payload", async () => {
  const fetchCalls: string[] = [];
  const result = await handleDiscordDeliveryRequest(
    {
      channel_id: "channel-2",
      attachment_url: "https://example.com/meme.jpg",
    },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (input) => {
        const url = String(input);
        fetchCalls.push(url);
        if (url === "https://example.com/meme.jpg") {
          return new Response(new Uint8Array([1, 2, 3]), {
            status: 200,
            headers: { "Content-Type": "image/jpeg" },
          });
        }
        if (url.includes("/channels/")) {
          return new Response(JSON.stringify({ id: "discord-message-2" }), { status: 200 });
        }
        throw new Error(`unexpected fetch ${url}`);
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.equal(fetchCalls[0], "https://example.com/meme.jpg");
  assert.equal(result.payload.status, "ok");
  assert.equal(result.payload.channel_id, "channel-2");
  assert.equal(result.payload.message_id, "discord-message-2");
  assert.equal(result.payload.has_attachment, true);
});

test("delivery contract rejects invalid payload", async () => {
  const result = await handleDiscordDeliveryRequest(
    {
      channel_id: "channel-3",
    },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async () => {
        throw new Error("should not call discord");
      },
    },
  );

  assert.equal(result.statusCode, 400);
  assert.equal(result.payload.status, "error");
  assert.match(String(result.payload.message), /content or attachment_url/);
});

test("delivery routes mapped channels through the webhook without bot auth", async () => {
  const calls: { url: string; headers: HeadersInit | undefined }[] = [];
  const result = await handleDiscordDeliveryRequest(
    { channel_id: "channel-9", content: "meme kkk" },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      webhooks: { "channel-9": "https://discord.test/api/webhooks/1/abc" },
      fetchImpl: async (input, init) => {
        calls.push({ url: String(input), headers: init?.headers });
        return new Response(JSON.stringify({ id: "webhook-message-1" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.url, "https://discord.test/api/webhooks/1/abc?wait=true");
  assert.equal(JSON.stringify(calls[0]?.headers).includes("Bot"), false);
  assert.equal(result.payload.message_id, "webhook-message-1");
});

test("delivery sends attachments through the webhook", async () => {
  const urls: string[] = [];
  const result = await handleDiscordDeliveryRequest(
    { channel_id: "channel-9", attachment_url: "https://example.com/meme.png" },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      webhooks: { "channel-9": "https://discord.test/api/webhooks/1/abc" },
      fetchImpl: async (input) => {
        const url = String(input);
        urls.push(url);
        if (url === "https://example.com/meme.png") {
          return new Response(new Uint8Array([1]), { status: 200, headers: { "Content-Type": "image/png" } });
        }
        return new Response(JSON.stringify({ id: "webhook-message-2" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.equal(urls[1], "https://discord.test/api/webhooks/1/abc?wait=true");
  assert.equal(result.payload.has_attachment, true);
});

test("parseWebhookMap splits on first equals and skips malformed entries", () => {
  assert.deepEqual(
    parseWebhookMap(" 1=https://d.test/api/webhooks/1/a?thread_id=5 , bad, 2=https://d.test/api/webhooks/2/b ,=x"),
    {
      "1": "https://d.test/api/webhooks/1/a?thread_id=5",
      "2": "https://d.test/api/webhooks/2/b",
    },
  );
  assert.deepEqual(parseWebhookMap(""), {});
});

test("delivery logs which route was used without leaking the webhook url", async () => {
  const logs: string[] = [];
  await handleDiscordDeliveryRequest(
    { channel_id: "channel-9", content: "meme kkk" },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      webhooks: { "channel-9": "https://discord.test/api/webhooks/1/secret-token" },
      log: (...args) => logs.push(args.join(" ")),
      fetchImpl: async () => new Response(JSON.stringify({ id: "m1" }), { status: 200 }),
    },
  );

  assert.match(logs.join("\n"), /"via":"webhook"/);
  assert.equal(logs.join("\n").includes("secret-token"), false);
});
