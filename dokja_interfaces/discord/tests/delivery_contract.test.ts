import assert from "node:assert/strict";
import test from "node:test";

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
