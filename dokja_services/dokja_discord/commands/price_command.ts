import { ApplicationCommandOptionType } from "discord-api-types/v10";

import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordCommandContext, DiscordEmbedSpec, DiscordReply, DiscordSlashCommand } from "../types.js";

// Discord waits about three seconds for autocomplete suggestions.
const AUTOCOMPLETE_TIMEOUT_MS = 2000;

// Discord allows 10 embeds per message and 6000 characters across them.
const MAX_EMBEDS = 10;
const MAX_EMBED_TEXT = 5800;

type Catalog = Record<string, string>;

// Every word the reply shows comes from here, keyed by the codes the Price domain returns.
const messages: Record<string, Catalog> = {
  "pt-BR": {
    header: "🔎 {count} produto(s) encontrados",
    header_for: "🔎 {count} produto(s) para **{search}**",
    none: "Nenhum produto da watchlist corresponde a essa busca.",
    more: "…e mais {count} produto(s). Refine a busca.",
    failed: "Não consegui consultar os preços agora.",
    market: "Mínimo do mercado",
    pix: "PIX",
    card: "cartão",
    unknown: "",
    median: "mediana 30d",
    lowest: "menor já visto",
    updated: "Atualizado em {date}",
    insufficient_history: "Histórico insuficiente",
    stale: "Sem dado recente",
    real_discount: "Promoção real",
    inflated_before_sale: "Inflado antes da promoção",
    fake_discount: "Desconto anunciado falso",
    no_discount: "Sem desconto",
    all_time_low: "menor preço histórico",
    list_above_observed_max: "preço \"de\" acima do que já foi cobrado",
    raised_before_drop: "subiu antes de cair",
    open: "abrir na loja",
    watch_added: "👁️ Passei a vigiar **{search}**. Os produtos chegam em instantes: pergunte de novo daqui a pouco.",
    watch_full: "A lista de buscas vigiadas está cheia; remova uma para vigiar **{search}**.",
    compare_header: "⚖️ Comparação de **{title}**",
    compare_part: "Código {part}",
    compare_candidates: "Achei mais de um produto. Mande o código ou o link de um deles:",
    compare_unknown: "Ainda não acompanho esse produto.",
    compare_not_found: "Procurei e não achei esse produto nas lojas que consigo ler.",
    compare_unreadable: "Não consigo ler a página da {store}; comparei o mesmo produto nas outras lojas.",
    compare_bad_link: "Não reconheço a loja desse link.",
    compare_need: "Mande o nome, o código ou o link do produto.",
    compare_failed: "Não consegui comparar agora.",
  },
  en: {
    header: "🔎 {count} product(s) found",
    header_for: "🔎 {count} product(s) for **{search}**",
    none: "No watched product matches that search.",
    more: "…and {count} more product(s). Narrow the search.",
    failed: "I could not check prices right now.",
    market: "Market low",
    pix: "PIX",
    card: "card",
    unknown: "",
    median: "30d median",
    lowest: "lowest seen",
    updated: "Updated {date}",
    insufficient_history: "Not enough history",
    stale: "No recent data",
    real_discount: "Real discount",
    inflated_before_sale: "Inflated before the sale",
    fake_discount: "Fake advertised discount",
    no_discount: "No discount",
    all_time_low: "all-time low",
    list_above_observed_max: "\"from\" price above anything charged",
    raised_before_drop: "rose before dropping",
    open: "open at the store",
    watch_added: "👁️ Now watching **{search}**. Its products arrive in a moment: ask again shortly.",
    watch_full: "The watched searches are full; remove one to watch **{search}**.",
    compare_header: "⚖️ Comparing **{title}**",
    compare_part: "Part {part}",
    compare_candidates: "More than one product matches. Send the part number or link of one:",
    compare_unknown: "I do not follow that product yet.",
    compare_not_found: "I looked and did not find that product at the stores I can read.",
    compare_unreadable: "I cannot read {store}'s page; I compared the same product at the other stores.",
    compare_bad_link: "I do not know that link's store.",
    compare_need: "Send the product's name, part number or link.",
    compare_failed: "I could not compare right now.",
  },
};

// How each verdict looks, and which one speaks for the product: a warning first, then a
// real discount, then the rest.
const looks: Record<string, { icon: string; color: number; rank: number }> = {
  fake_discount: { icon: "🔴", color: 0xe74c3c, rank: 0 },
  inflated_before_sale: { icon: "🟠", color: 0xe67e22, rank: 1 },
  real_discount: { icon: "🟢", color: 0x2ecc71, rank: 2 },
  no_discount: { icon: "⚪", color: 0x95a5a6, rank: 3 },
  stale: { icon: "⏸️", color: 0x7f8c8d, rank: 4 },
  insufficient_history: { icon: "⏳", color: 0x7f8c8d, rank: 5 },
};
const unknownLook = { icon: "•", color: 0x7f8c8d, rank: 9 };

function catalog(locale = "pt-BR"): Catalog {
  return messages[locale] ?? messages[locale.split("-")[0]] ?? messages.en;
}

function text(words: Catalog, key: string, values: Record<string, string | number> = {}): string {
  let value = words[key] ?? key;
  for (const [name, replacement] of Object.entries(values)) {
    value = value.replace(`{${name}}`, String(replacement));
  }
  return value;
}

function money(cents: unknown, locale: string): string {
  const value = typeof cents === "number" ? cents / 100 : 0;
  return value.toLocaleString(locale, { style: "currency", currency: "BRL" });
}

function day(iso: unknown): string {
  const [year, month, date] = String(iso ?? "").split("-");
  return year && month && date ? `${date}/${month}` : "";
}

type Assessment = Record<string, unknown>;

function looksOf(verdict: unknown) {
  return looks[String(verdict)] ?? unknownLook;
}

function field(row: Assessment, words: Catalog, locale: string) {
  // The merchant is a key the owner's collector chose; the code names no store.
  const name = row.series === "market_min"
    ? `${text(words, "market")} · ${String(row.merchant)}`
    : [String(row.merchant), text(words, String(row.price_kind))].filter(Boolean).join(" · ");
  const lines = [`**${money(row.latest_cents, locale)}**`];
  if (typeof row.median_30_cents === "number" && row.median_30_cents > 0) {
    lines.push(`${text(words, "median")}: ${money(row.median_30_cents, locale)}`);
  }
  if (typeof row.lowest_cents === "number" && row.lowest_cents > 0) {
    lines.push(`${text(words, "lowest")}: ${money(row.lowest_cents, locale)}`);
  }
  const look = looksOf(row.verdict);
  lines.push(`${look.icon} ${text(words, String(row.verdict))}`);
  const page = safeUrl(row.url);
  if (page) lines.push(`[${text(words, "open")}](${page})`);
  return { name, value: lines.join("\n"), inline: true };
}

// A store page for a link: an http(s) address without characters that would break the markdown.
function safeUrl(value: unknown): string {
  const address = typeof value === "string" ? value.trim() : "";
  return /^https?:\/\/[^\s()<>]+$/.test(address) ? address : "";
}

function embedFor(product: Record<string, unknown>, words: Catalog, locale: string): DiscordEmbedSpec {
  const rows = Array.isArray(product.assessments) ? (product.assessments as Assessment[]) : [];
  const lead = [...rows].sort((a, b) => looksOf(a.verdict).rank - looksOf(b.verdict).rank)[0];
  const look = looksOf(lead?.verdict);
  const signals = [...new Set(rows.flatMap((row) => (Array.isArray(row.signals) ? row.signals.map(String) : [])))]
    .map((signal) => text(words, signal));
  const latest = rows.map((row) => String(row.latest_on ?? "")).sort().at(-1);
  const cheapest = [...rows].filter((row) => safeUrl(row.url))
    .sort((a, b) => Number(a.latest_cents ?? 0) - Number(b.latest_cents ?? 0))[0];
  return {
    title: String(product.title ?? product.product),
    ...(cheapest ? { url: safeUrl(cheapest.url) } : {}),
    description: [`${look.icon} **${text(words, String(lead?.verdict ?? ""))}**`, ...signals].join(" · "),
    color: look.color,
    fields: rows.map((row) => field(row, words, locale)),
    ...(latest ? { footer: text(words, "updated", { date: day(latest) }) } : {}),
  };
}

function embedLength(embed: DiscordEmbedSpec): number {
  return (embed.title?.length ?? 0) + (embed.description?.length ?? 0) + (embed.footer?.length ?? 0)
    + (embed.fields ?? []).reduce((total, entry) => total + entry.name.length + entry.value.length, 0);
}

// buildPriceReply renders the bridge's price result as one card per product; exported for tests.
export function buildPriceReply(price: unknown, locale = "pt-BR", search = ""): DiscordReply {
  const words = catalog(locale);
  const result = (price ?? {}) as { products?: Array<Record<string, unknown>>; matched?: number; watch?: { category?: string; brand?: string; status?: string } };
  const products = Array.isArray(result.products) ? result.products : [];
  // What the search did to the watched searches: only a new watch or a full list is worth saying.
  const watched = [result.watch?.category, result.watch?.brand].filter((part) => part?.trim()).join(" · ");
  const notice = result.watch?.status === "added" ? text(words, "watch_added", { search: watched })
    : result.watch?.status === "full" ? text(words, "watch_full", { search: watched }) : "";
  if (products.length === 0) {
    return notice ? `${notice}\n${text(words, "none")}` : text(words, "none");
  }
  const matched = Math.max(result.matched ?? products.length, products.length);
  const embeds: DiscordEmbedSpec[] = [];
  let used = 0;
  for (const product of products) {
    const embed = embedFor(product, words, locale);
    if (embeds.length === MAX_EMBEDS || used + embedLength(embed) > MAX_EMBED_TEXT) {
      break;
    }
    embeds.push(embed);
    used += embedLength(embed);
  }
  const header = search.trim()
    ? text(words, "header_for", { count: matched, search: search.trim() })
    : text(words, "header", { count: matched });
  const left = matched - embeds.length;
  const lines = [notice, header, left > 0 ? text(words, "more", { count: left }) : ""].filter(Boolean);
  return { content: lines.join("\n"), embeds };
}

export function createPriceCommand(orchestratorEndpoint: string, orchestratorTimeoutMs: number, locale?: string): DiscordSlashCommand {
  return {
    name: "preco",
    description: "Check watched product prices: is it a real discount?",
    options: [
      {
        name: "produto",
        description: "Words to find, such as hd, ssd or video card",
        type: ApplicationCommandOptionType.String,
        required: false,
      },
      {
        name: "marca",
        description: "Brand name",
        type: ApplicationCommandOptionType.String,
        required: false,
      },
      {
        name: "loja",
        description: "Only this store; suggestions list the stores already seen",
        type: ApplicationCommandOptionType.String,
        required: false,
        autocomplete: true,
      },
    ],
    autocomplete: async (context) => {
      if (context.option !== "loja") return [];
      // Discord waits about three seconds for suggestions.
      const response = await dispatchToOrchestratorResponse(orchestratorEndpoint, Math.min(orchestratorTimeoutMs, AUTOCOMPLETE_TIMEOUT_MS), buildBridgePayload({
        eventType: "price.merchants", userId: context.userId, channelId: context.channelId, messageId: "autocomplete", guildId: context.guildId,
      }));
      const typed = context.value.trim().toLowerCase();
      const merchants = (response.price as { merchants?: unknown } | undefined)?.merchants;
      return (Array.isArray(merchants) ? merchants : [])
        .map(String)
        .filter((merchant) => merchant.toLowerCase().includes(typed))
        .map((merchant) => ({ name: merchant, value: merchant }));
    },
    handle: async (context: DiscordCommandContext) => {
      try {
        const query = context.getString("produto");
        const brand = context.getString("marca");
        const merchant = context.getString("loja");
        const response = await dispatchToOrchestratorResponse(
          orchestratorEndpoint,
          orchestratorTimeoutMs,
          buildBridgePayload({
            eventType: "price.search",
            query,
            brand,
            merchant,
            userId: context.userId,
            channelId: context.channelId,
            messageId: context.messageId,
            guildId: context.guildId,
          }),
        );
        // A switched-off or refused request carries a reply and no price result.
        const reply = response.price === undefined && response.reply.trim()
          ? response.reply
          : buildPriceReply(response.price, locale, [query, brand, merchant].filter((part) => part?.trim()).join(" "));
        await context.reply(reply, { ephemeral: true });
      } catch (error) {
        console.error("[dokja-discord] command preco failed", error);
        await context.reply(text(catalog(locale), "failed"), { ephemeral: true });
      }
    },
  };
}

// buildCompareReply renders a price.compare result: one card with every store, cheapest first,
// Ningo's lines when there are any, and what kept a store or a product out. Exported for tests.
export function buildCompareReply(compare: unknown, locale = "pt-BR"): DiscordReply {
  const words = catalog(locale);
  const result = (compare ?? {}) as {
    found?: boolean; product?: Record<string, unknown>; offers?: Assessment[]; candidates?: Array<Record<string, unknown>>;
    comment?: string; looked_up?: boolean; link?: { known?: boolean; store?: string; store_name?: string };
  };
  if (result.link && result.link.known === false) return text(words, "compare_bad_link");
  if (!result.found) {
    const candidates = Array.isArray(result.candidates) ? result.candidates : [];
    if (candidates.length > 0) {
      return [text(words, "compare_candidates"), ...candidates.map((candidate) => `• ${String(candidate.title ?? candidate.product)}`)].join("\n");
    }
    return text(words, result.looked_up ? "compare_not_found" : "compare_unknown");
  }
  const product = result.product ?? {};
  const offers = Array.isArray(result.offers) ? result.offers : [];
  const lead = [...offers].sort((a, b) => looksOf(a.verdict).rank - looksOf(b.verdict).rank)[0];
  const cheapest = offers.find((offer) => offer.series !== "market_min" && safeUrl(offer.url));
  const part = String(product.part_number ?? "");
  const store = result.link?.store;
  const notes = store && !offers.some((offer) => offer.merchant === store)
    ? [text(words, "compare_unreadable", { store: result.link?.store_name || store })] : [];
  const embed: DiscordEmbedSpec = {
    title: String(product.title ?? product.product ?? ""),
    ...(cheapest ? { url: safeUrl(cheapest.url) } : {}),
    description: typeof result.comment === "string" && result.comment.trim() ? result.comment.trim() : undefined,
    color: looksOf(lead?.verdict).color,
    fields: offers.map((offer) => field(offer, words, locale)),
    ...(part ? { footer: text(words, "compare_part", { part }) } : {}),
  };
  const header = text(words, "compare_header", { title: String(product.title ?? product.product ?? "") });
  return { content: [header, ...notes].join("\n"), embeds: [embed] };
}

export function createCompareCommand(orchestratorEndpoint: string, orchestratorTimeoutMs: number, locale?: string): DiscordSlashCommand {
  return {
    name: "comparar_produto",
    description: "Compare one product across stores, by name, part number or link",
    options: [
      { name: "produto", description: "Name or part number, such as ssd 1tb", type: ApplicationCommandOptionType.String, required: false },
      { name: "link", description: "A product link from a store; it is read, never opened", type: ApplicationCommandOptionType.String, required: false },
    ],
    handle: async (context: DiscordCommandContext) => {
      const words = catalog(locale);
      const query = context.getString("produto").trim();
      const link = context.getString("link").trim();
      if (!query && !link) {
        await context.reply(text(words, "compare_need"), { ephemeral: true });
        return;
      }
      try {
        const response = await dispatchToOrchestratorResponse(orchestratorEndpoint, orchestratorTimeoutMs, buildBridgePayload({
          eventType: "price.compare", query, link,
          userId: context.userId, channelId: context.channelId, messageId: context.messageId, guildId: context.guildId,
        }));
        const reply = response.price === undefined && response.reply.trim() ? response.reply : buildCompareReply(response.price, locale);
        await context.reply(reply, { ephemeral: true });
      } catch (error) {
        console.error("[dokja-discord] command comparar_produto failed", error);
        await context.reply(text(words, "compare_failed"), { ephemeral: true });
      }
    },
  };
}
