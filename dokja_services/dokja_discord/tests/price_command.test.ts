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

test("price sends a price search with its words and replies with cards", async () => {
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
      getString: (name: string) => ({ product: "hd", brand: "brandv" } as Record<string, string>)[name] ?? "",
      getNumber: () => undefined,
      reply: async (message, opts) => { replies.push({ message, ephemeral: opts?.ephemeral }); },
      updateReply: async () => {},
      showModal: async () => {},
    });
    assert.equal(sent.event_type, "price.search");
    assert.equal(sent.payload.query, "hd");
    assert.equal(sent.payload.brand, "brandv");
    assert.equal(replies.length, 1);
    assert.equal(embedsOf(replies[0].message)[0].title, "HD Example 2TB");
    assert.match((replies[0].message as { content: string }).content, /\*\*hd brandv\*\*/);
    assert.equal(replies[0].ephemeral, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("price passes a refusal through instead of an empty result", async () => {
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

test("each source links to its store page and the card title links to the cheapest offer", () => {
  const linked = {
    matched: 1,
    products: [{
      product: "pn:mz-example", title: "SSD Example",
      assessments: [
        { merchant: "store-a", series: "merchant", price_kind: "pix", verdict: "no_discount", signals: [], latest_on: "2026-11-08", latest_cents: 50000, url: "https://store-a.example/p/1" },
        { merchant: "store-b", series: "merchant", price_kind: "pix", verdict: "no_discount", signals: [], latest_on: "2026-11-08", latest_cents: 45000, url: "https://store-b.example/p/1" },
        { merchant: "store-c", series: "merchant", price_kind: "pix", verdict: "no_discount", signals: [], latest_on: "2026-11-08", latest_cents: 40000, url: "javascript:alert(1)" },
      ],
    }],
  };
  const [card] = embedsOf(buildPriceReply(linked, "pt-BR"));
  assert.equal(card.url, "https://store-b.example/p/1");
  assert.match(card.fields?.[0].value ?? "", /\[abrir na loja\]\(https:\/\/store-a\.example\/p\/1\)$/);
  assert.doesNotMatch(card.fields?.[2].value ?? "", /javascript/);
});

test("a search the owner starts to watch says so, even before its products arrive", () => {
  const added = { products: [], matched: 0, watch: { category: "ssd sata", brand: "brandx", status: "added" } };
  assert.match(String(buildPriceReply(added, "pt-BR")), /Passei a vigiar \*\*ssd sata · brandx\*\*/);
  const guest = { products: [], matched: 0, watch: { category: "ssd sata", brand: "brandx", status: "not_owner" } };
  assert.equal(buildPriceReply(guest, "pt-BR"), "Nenhum produto da watchlist corresponde a essa busca.");
  const withProducts = { ...result, watch: { category: "hd", status: "added" } };
  assert.match((buildPriceReply(withProducts, "en") as { content: string }).content, /^👁️ Now watching \*\*hd\*\*/);
});

test("price sends the store and suggests the stores already seen", async () => {
  const originalFetch = globalThis.fetch;
  const sent: any[] = [];
  globalThis.fetch = (async (_input, init) => {
    const body = JSON.parse(String(init?.body ?? "{}"));
    sent.push(body);
    const price = body.event_type === "price.merchants" ? { merchants: ["aggregator", "store-a", "store-b"] } : result;
    return new Response(JSON.stringify({ status: "ok", price }), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;
  try {
    const command = createPriceCommand("http://orch", 5000, "pt-BR");
    const choices = await command.autocomplete!({ option: "store", value: "STORE", userId: "u", channelId: "c" });
    assert.deepEqual(choices, [{ name: "store-a", value: "store-a" }, { name: "store-b", value: "store-b" }]);
    assert.deepEqual(await command.autocomplete!({ option: "brand", value: "", userId: "u", channelId: "c" }), []);

    await command.handle({
      userId: "u", channelId: "c", messageId: "m", guildId: "g",
      getString: (name: string) => ({ product: "hd", store: "store-a" } as Record<string, string>)[name] ?? "",
      getNumber: () => undefined, reply: async () => {}, updateReply: async () => {}, showModal: async () => {},
    });
    const search = sent.find((body) => body.event_type === "price.search");
    assert.equal(search.payload.merchant, "store-a");
    assert.equal(search.payload.query, "hd");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

import { buildCompareReply, createCompareCommand } from "../commands/price_command.js";

const compared = {
  found: true, looked_up: true,
  product: { product: "pn:xy650ss-240gt-r", title: "SSD Example 240GB", part_number: "XY650SS-240GT-R" },
  offers: [
    { merchant: "store-b", series: "merchant", price_kind: "unknown", verdict: "insufficient_history", signals: [], latest_cents: 11990, url: "https://store-b.example/p" },
    { merchant: "store-a", series: "merchant", price_kind: "pix", verdict: "real_discount", signals: [], latest_cents: 12990, url: "https://store-a.example/p" },
    { merchant: "aggregator", series: "market_min", price_kind: "unknown", verdict: "no_discount", signals: [], latest_cents: 11000, url: "https://market.example/p" },
  ],
  comment: "Compre agora, mortal.",
  link: { known: true, store: "store-z", store_name: "Store Z" },
};

test("a comparison is one card: every store cheapest first, Ningo's words, the part number and what was unreadable", () => {
  const reply = buildCompareReply(compared, "pt-BR") as { content: string; embeds: DiscordEmbedSpec[] };
  assert.match(reply.content, /^⚖️ Comparação de \*\*SSD Example 240GB\*\*/);
  assert.match(reply.content, /Não consigo ler a página da Store Z; comparei o mesmo produto nas outras lojas\./);
  const [card] = reply.embeds;
  assert.equal(card.description, "Compre agora, mortal.");
  assert.equal(card.url, "https://store-b.example/p");
  assert.equal(card.footer, "Código XY650SS-240GT-R");
  assert.deepEqual(card.fields?.map((entry) => entry.name), ["store-b", "store-a · PIX", "Mínimo do mercado · aggregator"]);
  // a store the link names that is among the offers is not called unreadable
  const read = buildCompareReply({ ...compared, link: { known: true, store: "store-a", store_name: "Store A" } }, "pt-BR") as { content: string };
  assert.doesNotMatch(read.content, /Não consigo ler/);
});

test("a comparison says when it found several, nothing, or an unknown link's store", () => {
  assert.match(String(buildCompareReply({ found: false, candidates: [{ product: "a", title: "SSD A" }, { product: "b", title: "SSD B" }] }, "pt-BR")), /Achei mais de um produto[\s\S]*• SSD A\n• SSD B/);
  assert.equal(buildCompareReply({ found: false, candidates: [] }, "pt-BR"), "Ainda não acompanho esse produto.");
  assert.equal(buildCompareReply({ found: false, candidates: [], looked_up: true }, "en"), "I looked and did not find that product at the stores I can read.");
  assert.equal(buildCompareReply({ found: false, candidates: [], link: { known: false } }, "pt-BR"), "Não reconheço a loja desse link.");
});

test("compare_product sends the words and the link, and asks for something when given nothing", async () => {
  const originalFetch = globalThis.fetch;
  const sent: any[] = [];
  globalThis.fetch = (async (_input, init) => {
    sent.push(JSON.parse(String(init?.body ?? "{}")));
    return new Response(JSON.stringify({ status: "ok", price: compared }), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;
  try {
    const replies: unknown[] = [];
    const run = (values: Record<string, string>) => createCompareCommand("http://orch", 1000, "pt-BR").handle({
      userId: "u", channelId: "c", messageId: "m", getString: (name: string) => values[name] ?? "", getNumber: () => undefined,
      reply: async (message) => { replies.push(message); }, updateReply: async () => {}, showModal: async () => {},
    });
    await run({});
    assert.equal(replies[0], "Mande o nome, o código ou o link do produto.");
    assert.equal(sent.length, 0);
    await run({ link: " https://store-z.example/produto/1/x " });
    assert.equal(sent[0].event_type, "price.compare");
    assert.equal(sent[0].payload.link, "https://store-z.example/produto/1/x");
    assert.equal((replies[1] as { embeds: DiscordEmbedSpec[] }).embeds[0].title, "SSD Example 240GB");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a new watch the orchestrator already collected says so, with or without products", () => {
  const product = { product: "hd-example-2tb", title: "HD Example 2TB", sources: [] };
  const found = buildPriceReply({ products: [product], matched: 1, watch: { category: "hd", brand: "", status: "added", collected: true } }, "pt-BR") as { content: string };
  assert.match(found.content, /^👁️ Passei a vigiar \*\*hd\*\* e já busquei agora\./);
  assert.doesNotMatch(found.content, /pergunte de novo/);
  assert.equal(buildPriceReply({ products: [], matched: 0, watch: { category: "hd", brand: "", status: "added", collected: true } }, "en"),
    "👁️ Now watching **hd**. I searched just now, but the stores I can read brought nothing.");
});
