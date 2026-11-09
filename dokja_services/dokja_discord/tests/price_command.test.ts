import test from "node:test";
import assert from "node:assert/strict";

import { buildPriceReply, createPriceCommand } from "../commands/price_command.js";
import type { DiscordEmbedSpec } from "../types.js";

const result = {
  matched: 1,
  products: [
    {
      product: "hd-example-2tb",
      title: "HD Example 2TB",
      assessments: [
        {
          merchant: "store-a", series: "merchant", price_kind: "pix", verdict: "real_discount",
          signals: ["all_time_low"], latest_on: "2026-11-08", latest_cents: 43990, median_30_cents: 50000,
          lowest_cents: 45000,
        },
        {
          merchant: "aggregator", series: "market_min", price_kind: "unknown", verdict: "inflated_before_sale",
          signals: ["raised_before_drop"], latest_on: "2026-11-07", latest_cents: 41000, median_30_cents: 0,
        },
      ],
    },
  ],
};

function embedsOf(reply: unknown): DiscordEmbedSpec[] {
  assert.equal(typeof reply, "object");
  return (reply as { embeds: DiscordEmbedSpec[] }).embeds;
}

test("each product is a card: the warning leads, colours the card and every source is a field", () => {
  const reply = buildPriceReply(result, "pt-BR", "hd");
  assert.equal((reply as { content: string }).content, "🔎 1 produto(s) para **hd**");
  const [card] = embedsOf(reply);
  assert.equal(card.title, "HD Example 2TB");
  // A markup outranks a discount elsewhere: the card warns first.
  assert.equal(card.color, 0xe67e22);
  assert.equal(card.description, "🟠 **Inflado antes da promoção** · menor preço histórico · subiu antes de cair");
  assert.equal(card.footer, "Atualizado em 08/11");
  assert.equal(card.fields?.length, 2);
  assert.equal(card.fields?.[0].name, "store-a · PIX");
  assert.match(card.fields?.[0].value ?? "", /^\*\*R\$\s?439,90\*\*\nmediana 30d: R\$\s?500,00\nmenor já visto: R\$\s?450,00\n🟢 Promoção real$/);
  // The merchant comes from the data; no store name is written in the adapter.
  assert.equal(card.fields?.[1].name, "Mínimo do mercado · aggregator");
  assert.equal(card.fields?.[1].inline, true);
});

test("the card speaks English when the locale is English", () => {
  const [card] = embedsOf(buildPriceReply(result, "en"));
  assert.match(card.description ?? "", /Inflated before the sale/);
  assert.doesNotMatch(JSON.stringify(card), /promoção/);
});

test("a search with no match answers in words, and a long one says what was left out", () => {
  assert.equal(buildPriceReply({ products: [], matched: 0 }, "pt-BR"), "Nenhum produto da watchlist corresponde a essa busca.");
  const reply = buildPriceReply({ ...result, matched: 14 }, "pt-BR");
  assert.match((reply as { content: string }).content, /…e mais 13 produto\(s\)\. Refine a busca\./);
});

test("cards stay within Discord's limits of 10 embeds and 6000 characters", () => {
  const products = Array.from({ length: 10 }, (_, index) => ({
    ...result.products[0],
    product: `hd-${index}`,
    title: `HD ${"Very long title ".repeat(14)}${index}`,
    assessments: Array.from({ length: 4 }, () => result.products[0].assessments[0]),
  }));
  const embeds = embedsOf(buildPriceReply({ products, matched: 10 }, "pt-BR"));
  const size = embeds.reduce((total, embed) => total + (embed.title?.length ?? 0) + (embed.description?.length ?? 0)
    + (embed.footer?.length ?? 0) + (embed.fields ?? []).reduce((sum, entry) => sum + entry.name.length + entry.value.length, 0), 0);
  assert.ok(embeds.length <= 10);
  assert.ok(size <= 6000, `size ${size}`);
});

test("preco sends a price search with its words and replies with cards", async () => {
  const originalFetch = globalThis.fetch;
  let sent: any;
  globalThis.fetch = (async (_input, init) => {
    sent = JSON.parse(String(init?.body ?? "{}"));
    return new Response(JSON.stringify({ status: "ok", price: result }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;
  try {
    const replies: Array<{ message: unknown; ephemeral?: boolean }> = [];
    await createPriceCommand("http://orch", 1000, "pt-BR").handle({
      userId: "u", channelId: "c", messageId: "m", guildId: "g",
      getString: (name: string) => ({ produto: "hd", marca: "wd" } as Record<string, string>)[name] ?? "",
      getNumber: () => undefined,
      reply: async (message, opts) => { replies.push({ message, ephemeral: opts?.ephemeral }); },
      updateReply: async () => {},
      showModal: async () => {},
    });
    assert.equal(sent.event_type, "price.search");
    assert.equal(sent.payload.query, "hd");
    assert.equal(sent.payload.brand, "wd");
    assert.equal(replies.length, 1);
    assert.equal(embedsOf(replies[0].message)[0].title, "HD Example 2TB");
    assert.match((replies[0].message as { content: string }).content, /\*\*hd wd\*\*/);
    assert.equal(replies[0].ephemeral, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("preco passes a refusal through instead of an empty result", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async () => new Response(JSON.stringify({ status: "ok", reply: "That is switched off right now (x)." }), {
    status: 200, headers: { "content-type": "application/json" },
  })) as typeof fetch;
  try {
    const replies: unknown[] = [];
    await createPriceCommand("http://orch", 1000, "pt-BR").handle({
      userId: "u", channelId: "c", messageId: "m", getString: () => "", getNumber: () => undefined,
      reply: async (message) => { replies.push(message); }, updateReply: async () => {}, showModal: async () => {},
    });
    assert.deepEqual(replies, ["That is switched off right now (x)."]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
