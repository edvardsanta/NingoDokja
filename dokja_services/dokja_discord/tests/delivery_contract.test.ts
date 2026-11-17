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
        return new Response(JSON.stringify({ id: "discord-message-1", guild_id: "guild-1" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.match(discordBody, /hello discord/);
  assert.equal(result.payload.status, "ok");
  assert.equal(result.payload.channel_id, "channel-1");
  assert.equal(result.payload.message_id, "discord-message-1");
  assert.equal(result.payload.guild_id, "guild-1");
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
          return new Response(JSON.stringify({ id: "discord-message-2", guild_id: "guild-1" }), { status: 200 });
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
  assert.equal(result.payload.guild_id, "guild-1");
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
  assert.match(String(result.payload.message), /content, attachment_url or embeds/);
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
        return new Response(JSON.stringify({ id: "webhook-message-1", guild_id: "guild-9" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.equal(calls.length, 1);
  assert.equal(calls[0]?.url, "https://discord.test/api/webhooks/1/abc?wait=true");
  assert.equal(JSON.stringify(calls[0]?.headers).includes("Bot"), false);
  assert.equal(result.payload.message_id, "webhook-message-1");
  assert.equal(result.payload.guild_id, "guild-9");
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

test("delivery downloads protected media with browser headers", async () => {
  let downloadHeaders = new Headers();
  const mediaUrl = "https://media.example/videos/clip.mp4";
  const result = await handleDiscordDeliveryRequest(
    { channel_id: "channel-9", attachment_url: mediaUrl },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (input, init) => {
        if (String(input) === mediaUrl) {
          downloadHeaders = new Headers(init?.headers);
          return new Response(new Uint8Array([1]), {
            status: 200,
            headers: { "Content-Type": "video/mp4" },
          });
        }
        return new Response(JSON.stringify({ id: "discord-message-video" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.match(downloadHeaders.get("User-Agent") ?? "", /Mozilla/);
  assert.equal(downloadHeaders.get("Referer"), "https://media.example/");
});

test("delivery attaches bytes it was given instead of fetching the url again", async () => {
  const fetchedUrls: string[] = [];
  let uploadedFilename = "";
  const bytes = Buffer.from("video-bytes");
  const result = await handleDiscordDeliveryRequest(
    {
      channel_id: "channel-9",
      attachment_url: "https://media.example/videos/clip.mp4",
      attachment_data_b64: bytes.toString("base64"),
      attachment_content_type: "video/mp4",
    },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (input, init) => {
        fetchedUrls.push(String(input));
        const form = init?.body as FormData;
        const payload = JSON.parse(String(form.get("payload_json")));
        uploadedFilename = payload.attachments[0].filename;
        const file = form.get("files[0]") as Blob;
        assert.equal(await file.text(), "video-bytes");
        return new Response(JSON.stringify({ id: "discord-message-bytes" }), { status: 200 });
      },
    },
  );

  assert.equal(result.statusCode, 200);
  assert.deepEqual(fetchedUrls, ["https://discord.test/api/v10/channels/channel-9/messages"]);
  assert.equal(uploadedFilename, "clip.mp4");
});

test("delivery posts a reply reference on the bot path and ignores it on webhooks", async () => {
  let botBody = "";
  const bot = await handleDiscordDeliveryRequest(
    { channel_id: "channel-1", content: "voltei com a leitura", reply_to_message_id: "original-1" },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (input, init) => {
        botBody = String(init?.body ?? "");
        return new Response(JSON.stringify({ id: "m-reply" }), { status: 200 });
      },
    },
  );
  assert.equal(bot.statusCode, 200);
  assert.equal(JSON.parse(botBody).message_reference.message_id, "original-1");

  let webhookBody = "";
  const webhook = await handleDiscordDeliveryRequest(
    { channel_id: "channel-9", content: "voltei com a leitura", reply_to_message_id: "original-1" },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      webhooks: { "channel-9": "https://discord.test/api/webhooks/1/abc" },
      fetchImpl: async (_input, init) => {
        webhookBody = String(init?.body ?? "");
        return new Response(JSON.stringify({ id: "m-webhook" }), { status: 200 });
      },
    },
  );
  assert.equal(webhook.statusCode, 200);
  assert.equal(JSON.parse(webhookBody).message_reference, undefined);
});

test("an unknown reply reference falls back to a plain message instead of failing", async () => {
  const bodies: string[] = [];
  const result = await handleDiscordDeliveryRequest(
    { channel_id: "channel-1", content: "voltei com a leitura", reply_to_message_id: "interaction-id" },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (_input, init) => {
        bodies.push(String(init?.body ?? ""));
        if (bodies.length === 1) {
          return new Response(JSON.stringify({ message: "Invalid Form Body", code: 50035, errors: { message_reference: { _errors: [{ code: "MESSAGE_REFERENCE_UNKNOWN_MESSAGE", message: "Unknown message" }] } } }), { status: 400 });
        }
        return new Response(JSON.stringify({ id: "m-plain" }), { status: 200 });
      },
    },
  );
  assert.equal(result.statusCode, 200);
  assert.equal(result.payload.message_id, "m-plain");
  assert.equal(bodies.length, 2);
  assert.equal(JSON.parse(bodies[0]).message_reference.message_id, "interaction-id");
  assert.equal(JSON.parse(bodies[1]).message_reference, undefined);
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

test("delivery renders bounded embeds on the bot and webhook paths", async () => {
  let botBody = "";
  const bot = await handleDiscordDeliveryRequest(
    {
      channel_id: "channel-1",
      embeds: [{
        title: "Chegou preço para ryzen 5 5600",
        description: "• 5600XT — kabum R$ 1.059,99",
        footer: "Veredito completo: /preco produto:ryzen 5 5600",
        color: 0x00b8a9,
        url: "https://example.com/offer",
        fields: [{ name: "Mercado", value: "mín. R$ 900,00", inline: true }],
      }],
    },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      fetchImpl: async (_input, init) => {
        botBody = String(init?.body ?? "");
        return new Response(JSON.stringify({ id: "m-embed" }), { status: 200 });
      },
    },
  );
  assert.equal(bot.statusCode, 200);
  const sent = JSON.parse(botBody);
  assert.equal(sent.embeds[0].title, "Chegou preço para ryzen 5 5600");
  assert.equal(sent.embeds[0].footer.text, "Veredito completo: /preco produto:ryzen 5 5600");
  assert.equal(sent.embeds[0].color, 0x00b8a9);
  assert.equal(sent.embeds[0].fields[0].name, "Mercado");

  let webhookPayload = "";
  const webhook = await handleDiscordDeliveryRequest(
    { channel_id: "channel-9", embeds: [{ title: "título", description: "corpo" }] },
    {
      apiBaseUrl: "https://discord.test/api/v10",
      token: "token",
      webhooks: { "channel-9": "https://discord.test/api/webhooks/1/abc" },
      fetchImpl: async (_input, init) => {
        webhookPayload = String(init?.body ?? "");
        return new Response(JSON.stringify({ id: "m-webhook-embed" }), { status: 200 });
      },
    },
  );
  assert.equal(webhook.statusCode, 200);
  assert.equal(JSON.parse(webhookPayload).embeds[0].title, "título");
});
