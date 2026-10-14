import assert from "node:assert/strict";
import test from "node:test";

import type { Conversation } from "../config.js";
import { createDeskSender } from "../messages/desk_sender.js";
import { SendLimit } from "../runtime/send_limit.js";

const conversations: Conversation[] = [
  { alias: "friend", number: "5511987654321" },
  { alias: "team", group: "120363000000000001@g.us" },
];

function setup(overrides: { connected?: boolean; send?: (jid: string, text: string) => Promise<string | undefined>; limit?: SendLimit } = {}) {
  const sent: Array<[string, string]> = [];
  const recorded: Array<[string, string, string | undefined]> = [];
  const sender = createDeskSender({
    conversations,
    send: overrides.send ?? (async (jid, text) => {
      sent.push([jid, text]);
      return "3EB0SENT";
    }),
    connected: () => overrides.connected ?? true,
    limit: overrides.limit ?? new SendLimit(),
    record: async (alias, text, id) => void recorded.push([alias, text, id]),
  });
  return { sender, sent, recorded };
}

test("a direct conversation is reached through its number and a group through its id", async () => {
  const { sender, sent } = setup();

  assert.equal(await sender("friend", "hello"), "sent");
  assert.equal(await sender("team", "hello all"), "sent");

  assert.deepEqual(sent, [
    ["5511987654321@s.whatsapp.net", "hello"],
    ["120363000000000001@g.us", "hello all"],
  ]);
});

test("what went out is recorded with the id it went out with", async () => {
  const { sender, recorded } = setup();

  await sender("friend", "hello");

  assert.deepEqual(recorded, [["friend", "hello", "3EB0SENT"]]);
});

test("an alias that was not named is refused before anything is sent or counted", async () => {
  const limit = new SendLimit([{ count: 1, windowMs: 60_000 }]);
  const { sender, sent } = setup({ limit });

  for (const alias of ["stranger", "", "5511987654321", "120363000000000001@g.us"]) {
    assert.equal(await sender(alias, "hello"), "not_named", alias);
  }

  assert.deepEqual(sent, []);
  assert.equal(limit.take(), true, "a refused message took nothing from the budget");
});

test("nothing is sent while the account is not connected, and that costs nothing either", async () => {
  const limit = new SendLimit([{ count: 1, windowMs: 60_000 }]);
  const { sender, sent } = setup({ connected: false, limit });

  assert.equal(await sender("friend", "hello"), "not_connected");

  assert.deepEqual(sent, []);
  assert.equal(limit.take(), true);
});

test("the budget stops a burst", async () => {
  const { sender, sent } = setup({ limit: new SendLimit([{ count: 2, windowMs: 60_000 }]) });

  const outcomes = [await sender("friend", "1"), await sender("friend", "2"), await sender("friend", "3")];

  assert.deepEqual(outcomes, ["sent", "sent", "limited"]);
  assert.equal(sent.length, 2);
});

test("a send that fails is a failure and is not recorded", async () => {
  const { sender, recorded } = setup({
    send: async () => {
      throw new Error("socket closed: 5511987654321");
    },
  });

  assert.equal(await sender("friend", "hello"), "failed");
  assert.deepEqual(recorded, []);
});

test("a history that cannot be written does not turn a sent message into a failure", async () => {
  const sender = createDeskSender({
    conversations,
    send: async () => "ID",
    connected: () => true,
    limit: new SendLimit(),
    record: async () => {
      throw new Error("disk full");
    },
  });

  assert.equal(await sender("friend", "hello"), "sent");
});
