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
    market_history_note: "trecho baseado no histórico existente do Zoom/Buscapé",
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
    watch_collected: "👁️ Passei a vigiar **{search}** e já busquei agora.",
    watch_collected_none: "👁️ Passei a vigiar **{search}** e já busquei agora, mas as lojas que consigo ler não trouxeram nada.",
    watch_full: "A lista de buscas vigiadas está cheia; remova uma para vigiar **{search}**.",
    compare_header: "⚖️ {title}",
    compare_offers: "Preços nas lojas",
    compare_sources: "Fontes técnicas coletadas",
    compare_unit_score: "pontos",
    compare_unit_percent_index: "índice (%)",
    compare_unit_seconds: "s",
    compare_benchmark: "Teste do modelo — {test}: referência {reference} → opção {candidate} {unit} ({difference}%)",
    compare_source_published: "publicado em {date}",
    compare_source_updated: "atualizado em {date}",
    compare_source_collected: "coletado em {date}",
    compare_no_sources: "Sem fontes técnicas coletadas; a análise do modelo é uma estimativa.",
    compare_context: "Uso: {use} · Equipamento atual: {equipment}",
    compare_unspecified: "não informado",
    compare_budget: "Orçamento de {price}",
    compare_reference: "Encontrei {count} versões. Usei esta como referência pelo menor preço disponível entre elas.",
    compare_reference_unpriced: "Encontrei {count} versões e escolhi esta como referência para seguir com a comparação.",
    compare_unconfirmed: "A equivalência técnica destes candidatos ainda não foi confirmada.",
    compare_specs_note: "Núcleos e threads iguais não garantem o mesmo desempenho.",
    compare_platform_note: "Socket diferente exige placa-mãe compatível.",
    compare_more_rows: "…e mais {count} oferta(s).",
    shortlist_performance: "Desempenho próximo ou superior",
    shortlist_economy: "Para economizar",
    shortlist_different_brand: "Outra marca",
    shortlist_advantage: "Vantagem específica",
    shortlist_alternative: "Outra opção",
    compare_estimated: "Análise estimada pelo modelo.",
    compare_above_budget: "acima do orçamento",
    compare_part: "Código {part}",
    compare_candidates: "Achei mais de um produto. Mande o código ou o link de um deles:",
    compare_unknown: "Ainda não acompanho esse produto.",
    compare_not_found: "Procurei e não achei esse produto nas lojas que consigo ler.",
    compare_unreadable: "Não consigo ler a página da {store}; comparei o mesmo produto nas outras lojas.",
    compare_over_budget: "A oferta encontrada ultrapassa o orçamento informado; procurei uma alternativa, mas esta comparação não encontrou uma opção compatível.",
    compare_alternatives: "Alternativas",
    compare_over_budget_alternatives: "A oferta encontrada ultrapassa o orçamento informado; veja os candidatos abaixo.",
    compare_buy: "🛒 Melhor compra agora: **{store}** por {price}.",
    compare_buy_low: "🛒 Mais barato agora: **{store}** por {price}, mas o histórico ainda é curto para dizer se é promoção.",
    compare_no_price: "Ainda não há um preço atual para recomendar.",
    alternative_technically_superior: "tecnicamente superior",
    alternative_technically_comparable: "tecnicamente equivalente",
    alternative_same_category_candidate: "a avaliar",
    alternative_similar_cpu_specs: "mesmos núcleos e threads",
    alternative_same_model_variant: "variante do mesmo modelo de GPU",
    model_comparable: "equivalência estimada pelo modelo",
    model_superior: "superioridade estimada pelo modelo",
    model_inferior: "desempenho inferior estimado pelo modelo",
    model_unknown: "análise técnica inconclusiva",
    alternative_reason_different_cpu_socket: "outro socket",
    alternative_reason_shared_cpu_benchmark: "benchmark comparável",
    alternative_reason_lower_cpu_benchmark: "desempenho inferior no benchmark",
    alternative_reason_lower_current_price: "mais barato",
    alternative_reason_within_budget: "dentro do orçamento",
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
    market_history_note: "excerpt based on existing Zoom/Buscapé history",
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
    watch_collected: "👁️ Now watching **{search}**, and I searched just now.",
    watch_collected_none: "👁️ Now watching **{search}**. I searched just now, but the stores I can read brought nothing.",
    watch_full: "The watched searches are full; remove one to watch **{search}**.",
    compare_header: "⚖️ {title}",
    compare_offers: "Store prices",
    compare_sources: "Collected technical sources",
    compare_unit_score: "points",
    compare_unit_percent_index: "index (%)",
    compare_unit_seconds: "s",
    compare_benchmark: "Model benchmark — {test}: reference {reference} → option {candidate} {unit} ({difference}%)",
    compare_source_published: "published {date}",
    compare_source_updated: "updated {date}",
    compare_source_collected: "collected {date}",
    compare_no_sources: "No collected technical sources; the model analysis is an estimate.",
    compare_context: "Use: {use} · Current equipment: {equipment}",
    compare_unspecified: "not provided",
    compare_budget: "Budget {price}",
    compare_reference: "Found {count} variants. This reference has the lowest available price among them.",
    compare_reference_unpriced: "Found {count} variants and selected this reference to continue the comparison.",
    compare_unconfirmed: "Technical equivalence of these candidates is still unconfirmed.",
    compare_specs_note: "Matching core and thread counts do not guarantee equal performance.",
    compare_platform_note: "A different socket requires a compatible motherboard.",
    compare_more_rows: "…and {count} more offer(s).",
    shortlist_performance: "Similar or higher performance",
    shortlist_economy: "Lower-cost option",
    shortlist_different_brand: "Another brand",
    shortlist_advantage: "Specific advantage",
    shortlist_alternative: "Another option",
    compare_estimated: "Model-estimated analysis.",
    compare_above_budget: "above budget",
    compare_part: "Part {part}",
    compare_candidates: "More than one product matches. Send the part number or link of one:",
    compare_unknown: "I do not follow that product yet.",
    compare_not_found: "I looked and did not find that product at the stores I can read.",
    compare_unreadable: "I cannot read {store}'s page; I compared the same product at the other stores.",
    compare_over_budget: "The offer found exceeds the stated budget; an alternative is needed, but this comparison found no compatible option.",
    compare_alternatives: "Alternatives",
    compare_over_budget_alternatives: "The offer found exceeds the stated budget; see the candidates below.",
    compare_buy: "🛒 Best buy now: **{store}** at {price}.",
    compare_buy_low: "🛒 Cheapest now: **{store}** at {price}, but the history is still too short to call it a deal.",
    compare_no_price: "There is no current price to recommend yet.",
    alternative_technically_superior: "technically superior",
    alternative_technically_comparable: "technically comparable",
    alternative_same_category_candidate: "unconfirmed",
    alternative_similar_cpu_specs: "matching cores and threads",
    alternative_same_model_variant: "variant of the same GPU model",
    model_comparable: "model-estimated equivalence",
    model_superior: "model-estimated superiority",
    model_inferior: "model-estimated lower performance",
    model_unknown: "inconclusive technical analysis",
    alternative_reason_different_cpu_socket: "different socket",
    alternative_reason_shared_cpu_benchmark: "comparable benchmark",
    alternative_reason_lower_cpu_benchmark: "lower benchmark performance",
    alternative_reason_lower_current_price: "lower price",
    alternative_reason_within_budget: "within budget",
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
  if (row.series === "market_min") {
    lines.push(`_${text(words, "market_history_note")}_`);
  }
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
  const result = (price ?? {}) as {
    products?: Array<Record<string, unknown>>; matched?: number;
    watch?: { category?: string; brand?: string; status?: string; collected?: boolean };
  };
  const products = Array.isArray(result.products) ? result.products : [];
  // What the search did to the watched searches: only a new watch or a full list is worth saying.
  // A new watch is collected at once; when the reply waited for that, it says so.
  const watched = [result.watch?.category, result.watch?.brand].filter((part) => part?.trim()).join(" · ");
  const added = result.watch?.collected === true
    ? (products.length > 0 ? "watch_collected" : "watch_collected_none") : "watch_added";
  const notice = result.watch?.status === "added" ? text(words, added, { search: watched })
    : result.watch?.status === "full" ? text(words, "watch_full", { search: watched }) : "";
  if (products.length === 0 && result.watch?.collected === true && result.watch?.status === "added") {
    return notice;
  }
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
    name: "price",
    description: "Check watched product prices: is it a real discount?",
    options: [
      {
        name: "product",
        description: "Words to find, such as hd, ssd or video card",
        type: ApplicationCommandOptionType.String,
        required: false,
      },
      {
        name: "brand",
        description: "Brand name",
        type: ApplicationCommandOptionType.String,
        required: false,
      },
      {
        name: "store",
        description: "Only this store; suggestions list the stores already seen",
        type: ApplicationCommandOptionType.String,
        required: false,
        autocomplete: true,
      },
    ],
    autocomplete: async (context) => {
      if (context.option !== "store") return [];
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
        const query = context.getString("product");
        const brand = context.getString("brand");
        const merchant = context.getString("store");
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
    alternatives?: Array<Record<string, unknown>>;
    reference_selection?: {matches?: number; reason?: string};
 comparison_context?:{intended_use?:string;current_equipment?:string};technical_sources?:Array<Record<string,unknown>>;
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
  const decision = (result as {
    decision?: { action?: string; selected_merchant?: string; selected_cents?: number; confidence?: string; reason_codes?: string[]; budget_cents?: number };
  }).decision;
  const store = result.link?.store;
  const notes = store && !offers.some((offer) => offer.merchant === store)
    ? [text(words, "compare_unreadable", { store: result.link?.store_name || store })] : [];
  const decisionNote = decisionLine(decision, offers, words, locale, Array.isArray(result.alternatives) && result.alternatives.length > 0);
  const alternatives = Array.isArray(result.alternatives) ? result.alternatives : [];
  const alternativeLines = alternatives.map((alternative) => {
    const classificationKey = `alternative_${String(alternative.classification ?? "")}`;
    const review = alternative.model_assessment as { relationship?: string; explanation?: string; advantage?: string } | undefined;
    const modelKey = `model_${String(review?.relationship ?? "")}`;
    const classification = text(words, classificationKey in words ? classificationKey : "alternative_same_category_candidate");
    const modelNote = review && modelKey in words ? text(words, modelKey) : "";
    // price facts are reasons beside the technical classification
    const reasons = (Array.isArray(alternative.reason_codes) ? alternative.reason_codes : [])
      .filter((code) => code !== "lower_current_price" && code !== "within_budget")
      .map((code) => `alternative_reason_${String(code)}`)
      .filter((key) => key in words)
      .map((key) => text(words, key));
    const price = typeof alternative.price_cents === "number" ? ` · ${money(alternative.price_cents, locale)}` : "";
    const title = comparisonTitle(alternative.title ?? alternative.product);
    const address = safeUrl(alternative.url);
    const page = address.length <= 300 ? address : "";
    const name = page ? `[${title}](${page})` : title;
    const explanation = typeof review?.explanation === "string" ? shortExplanation(review.explanation, alternatives.length > 6 ? 80 : 160) : "";
    const roleKey = `shortlist_${String(alternative.selection_reason ?? "")}`;
    const role = alternative.selection_reason === "alternative" ? "" : alternative.selection_reason === "advantage" && review?.advantage
      ? review.advantage.slice(0, 60) : roleKey in words ? text(words, roleKey) : "";
    const above = alternative.over_budget === true ? text(words, "compare_above_budget") : "";
    const metrics=Array.isArray(alternative.benchmark_comparisons)?alternative.benchmark_comparisons:[];
    const metric=metrics[0] as Record<string,unknown>|undefined;
    const number=(value:number)=>value.toLocaleString(locale,{maximumSignificantDigits:5});
    const benchmark=metric&&typeof metric.reference_value==="number"&&typeof metric.candidate_value==="number"&&typeof metric.difference_basis_points==="number" ? text(words,"compare_benchmark",{test:shortExplanation(String(metric.test??""),75),reference:number(metric.reference_value),candidate:number(metric.candidate_value),unit: ["score","percent_index","seconds"].includes(String(metric.unit))?text(words,`compare_unit_${metric.unit}`):String(metric.unit??""),difference:(metric.difference_basis_points>0?"+":"")+number(metric.difference_basis_points/100)}):"";
    return [`• ${name}${price}`, [role || modelNote || classification, ...reasons, above].filter(Boolean).join(" · "), benchmark, explanation].filter(Boolean).join("\n");
  });
  const fields: NonNullable<DiscordEmbedSpec["fields"]> = [];
  if (offers.length > 0) {
    fields.push(...comparisonFields(text(words, "compare_offers"), offers.map((offer) => {
      const name = (offer.series === "market_min"
        ? text(words, "market")
        : String(offer.merchant_name ?? offer.merchant)).slice(0, 80);
      const payment = text(words, String(offer.price_kind ?? "unknown"));
      const price = money(offer.latest_cents, locale);
      const address = safeUrl(offer.url);
      const page = address.length <= 300 ? address : "";
      const amount = page ? `[${price}](${page})` : price;
      const warning = ["fake_discount", "inflated_before_sale", "stale", "real_discount"].includes(String(offer.verdict))
        ? ` · ${text(words, String(offer.verdict))}` : "";
      return `• ${name}${payment ? ` · ${payment}` : ""} — **${amount}**${warning}`;
    }), words, 1700));
  }
  if (alternativeLines.length > 0) {
    fields.push(...comparisonFields(text(words, "compare_alternatives"), alternativeLines, words, result.technical_sources?.length ? 1900 : 2500));
    if (alternatives.some((item) => item.model_assessment)) {
      notes.push(text(words, "compare_estimated"));
    } else if (alternatives.some((item) => item.classification === "same_category_candidate")) {
      notes.push(text(words, "compare_unconfirmed"));
    }
    if (!alternatives.some((item) => item.model_assessment) && alternatives.some((item) => item.classification === "similar_cpu_specs")) {
      notes.push(text(words, "compare_specs_note"));
    }
  }
  const sources=Array.isArray(result.technical_sources)?result.technical_sources:[];
  const sourceLines=[...new Map(sources.map(s=>[String(s.url),s])).values()].slice(0,5).flatMap(s=>{
    const address=safeUrl(s.url);if(!address||address.length>300)return [];
    const title=String(s.title??"").replace(/[\[\]*_`]/g,"").slice(0,100);
    const sourceDate=s.updated_at?text(words,"compare_source_updated",{date:String(s.updated_at).slice(0,10)}):s.published_at?text(words,"compare_source_published",{date:String(s.published_at).slice(0,10)}):s.observed_at?text(words,"compare_source_collected",{date:String(s.observed_at).slice(0,10)}):"";
    return [`• [${title}](${address})${s.workload?` · ${String(s.workload).slice(0,100)}`:""}${sourceDate?` · ${sourceDate}`:""}`];
  });
  if(sourceLines.length)fields.push(...comparisonFields(text(words,"compare_sources"),sourceLines,words,700));
  else if(alternatives.some(a=>a.model_assessment))notes.push(text(words,"compare_no_sources"));
  const comparisonContext=result.comparison_context;
  const contextNote=comparisonContext&&(comparisonContext.intended_use||comparisonContext.current_equipment)?text(words,"compare_context",{use:shortExplanation(comparisonContext.intended_use||text(words,"compare_unspecified"),100),equipment:shortExplanation(comparisonContext.current_equipment||text(words,"compare_unspecified"),150)}):"";
  const budget = typeof decision?.budget_cents === "number" && decision.budget_cents > 0
    ? text(words, "compare_budget", { price: money(decision.budget_cents, locale) }) : "";
  const comment = typeof result.comment === "string" ? result.comment.trim().slice(0, 600) : "";
  const reference = (result.reference_selection?.matches ?? 0) > 1
    ? text(words,result.reference_selection?.reason === "lowest_current_offer" ? "compare_reference" : "compare_reference_unpriced",{count:result.reference_selection!.matches!}) : "";
  const embed: DiscordEmbedSpec={
      title: text(words, "compare_header", { title: comparisonTitle(product.title ?? product.product) }),
      ...(cheapest ? { url: safeUrl(cheapest.url) } : {}),
      description: [decisionNote, budget, contextNote, reference, comment].filter(Boolean).join("\n\n"),
      color: looksOf(lead?.verdict).color,
      fields,
      footer: [...notes, part ? text(words, "compare_part", { part }) : ""].filter(Boolean).join("\n"),
  };
  // Trim complete trailing rows/sections, preserving valid markdown links.
  while(embedLength(embed)>MAX_EMBED_TEXT && embed.fields?.length){
    const last=embed.fields.at(-1)!;const rows=last.value.split("\n");
    if(rows.length>1){rows.pop();last.value=rows.join("\n");}else{embed.fields.pop();}
  }
  return {content:"",embeds:[embed]};
}

function comparisonTitle(value: unknown): string {
  const title = String(value ?? "").replace(/[\[\]*_`]/g, "");
  const cpu = title.match(/\b(?:(?:AMD\s+)?Ryzen\s+[3579]\s+\d+[a-z0-9]*|(?:Intel\s+)?Core\s+(?:Ultra\s+[3579]\s+\d+[a-z0-9]*|i[3579][ -]?\d+[a-z0-9]*))\b/i);
  if (cpu) return cpu[0];
  const headline = title.split(",")[0].trim();
  return (title.length > 70 && headline.length >= 16 ? headline : title).slice(0, 120);
}

function shortExplanation(value: string, limit = 160): string {
  const sentence = value.trim().match(/^.*?[.!?](?:\s|$)/)?.[0].trim() || value.trim();
  if (sentence.length <= limit) return sentence;
  return sentence.slice(0, limit - 1).replace(/\s+\S*$/, "") + "…";
}

// Keep whole rows and links together within Discord's 1024-character field values.
function comparisonFields(name: string, lines: string[], words: Catalog, budget: number): NonNullable<DiscordEmbedSpec["fields"]> {
  const fields: NonNullable<DiscordEmbedSpec["fields"]> = [];
  let value = "";
  let used = 0;
  for (const [index, line] of lines.entries()) {
    if (used + line.length + 100 > budget) {
      fields.push({ name: fields.length === 0 ? name : "\u200b", value: [value, text(words, "compare_more_rows", { count: lines.length - index })].filter(Boolean).join("\n") });
      return fields;
    }
    if (value && value.length + line.length + 1 > 1024) {
      fields.push({ name: fields.length === 0 ? name : "\u200b", value });
      value = "";
    }
    value = [value, line].filter(Boolean).join("\n");
    used += line.length + 1;
  }
  if (value) fields.push({ name: fields.length === 0 ? name : "\u200b", value });
  return fields;
}

// decisionLine says what the domain decided: where to buy, that the offer is over budget, or that
// there is no current price. A short history says so instead of calling the price a deal.
function decisionLine(
  decision: { action?: string; selected_merchant?: string; selected_cents?: number; confidence?: string; reason_codes?: string[] } | undefined,
  offers: Assessment[], words: Catalog, locale: string, hasAlternatives: boolean,
): string {
  switch (decision?.action) {
    case "buy_original": {
      const store = String(decision.selected_merchant ?? "");
      const chosen = offers.find((offer) => offer.merchant === store && offer.series !== "market_min");
      const name = String(chosen?.merchant_name ?? store);
      const values = { store: name, price: money(decision.selected_cents, locale) };
      return text(words, decision.confidence === "low" ? "compare_buy_low" : "compare_buy", values);
    }
    case "seek_alternative":
      return text(words, hasAlternatives ? "compare_over_budget_alternatives" : "compare_over_budget");
    case "no_recommendation":
      return text(words, "compare_no_price");
    default:
      return "";
  }
}

export function createCompareCommand(orchestratorEndpoint: string, orchestratorTimeoutMs: number, locale?: string): DiscordSlashCommand {
  return {
    name: "compare_product",
    description: "Compare one product across stores, by name, part number or link",
    options: [
      { name: "product", description: "Name or part number, such as ssd 1tb", type: ApplicationCommandOptionType.String, required: false },
      { name: "link", description: "A product link from a store; it is read, never opened", type: ApplicationCommandOptionType.String, required: false },
      { name:"intended_use", description:"Intended workload, resolution or applications", type:ApplicationCommandOptionType.String, required:false,max_length:500 },
      { name:"current_equipment", description:"Current CPU, GPU, motherboard or power supply", type:ApplicationCommandOptionType.String, required:false,max_length:500 },
      { name: "budget", description: "Maximum budget in BRL", type: ApplicationCommandOptionType.Number, required: false, min_value: 0 },
    ],
    handle: async (context: DiscordCommandContext) => {
      const words = catalog(locale);
      const query = context.getString("product").trim();
      const link = context.getString("link").trim();
      const budget = context.getNumber("budget");
      if (!query && !link) {
        await context.reply(text(words, "compare_need"), { ephemeral: true });
        return;
      }
      try {
        const response = await dispatchToOrchestratorResponse(orchestratorEndpoint, orchestratorTimeoutMs, buildBridgePayload({
          eventType: "price.compare", query, link,
 intended_use:context.getString("intended_use").trim(),current_equipment:context.getString("current_equipment").trim(),
          ...(typeof budget === "number" && Number.isFinite(budget) && budget > 0 ? { budget_cents: Math.round(budget * 100) } : {}),
          userId: context.userId, channelId: context.channelId, messageId: context.messageId, guildId: context.guildId,
        }));
        const reply = response.price === undefined && response.reply.trim() ? response.reply : buildCompareReply(response.price, locale);
        await context.reply(reply, { ephemeral: true });
      } catch (error) {
        console.error("[dokja-discord] command compare_product failed", error);
        await context.reply(text(words, "compare_failed"), { ephemeral: true });
      }
    },
  };
}
