import { ApplicationCommandOptionType } from "discord-api-types/v10";

import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordCommandContext, DiscordReply, DiscordSlashCommand } from "../types.js";

// Reading and extracting a page stays inside the knowledge service's budget.
export const SAVE_TIMEOUT_MS = 2 * 60 * 1000;

type Catalog = Record<string, string>;

// Every word the reply shows comes from here, keyed by the codes the knowledge save returns.
const messages: Record<string, Catalog> = {
  "pt-BR": {
    saved: "Guardei na sua biblioteca.",
    updated: "Já estava guardado; atualizei o que mudou.",
    not_owner: "Só o dono guarda documento na biblioteca.",
    invalid_link: "Esse link eu não consigo abrir.",
    unavailable: "A biblioteca não respondeu. Tente de novo.",
    failed: "Não consegui guardar. Tente de novo.",
  },
  en: {
    saved: "Saved to your library.",
    updated: "It was already saved; I updated what changed.",
    not_owner: "Only the owner saves documents to the library.",
    invalid_link: "That is not a link I can open.",
    unavailable: "The library did not answer. Try again.",
    failed: "I could not save it. Try again.",
  },
};

function catalog(locale = "pt-BR"): Catalog {
  return messages[locale] ?? messages[locale.split("-")[0]] ?? messages.en;
}

// buildSaveReply words a save's result or refusal; exported for tests.
export function buildSaveReply(save: unknown, locale = "pt-BR"): DiscordReply {
  const words = catalog(locale);
  const result = (save ?? {}) as Record<string, unknown>;
  if (result.status !== "ok" || typeof result.created !== "boolean") {
    return words[String(result.refused ?? "failed")] ?? words.failed;
  }
  return result.created ? words.saved : words.updated;
}

export function createSaveCommand(orchestratorEndpoint: string, locale?: string): DiscordSlashCommand {
  return {
    name: "save_document",
    description: "Save a link into Ningo's knowledge library, as your own document.",
    options: [
      {
        name: "link",
        description: "The link to read and keep",
        type: ApplicationCommandOptionType.String,
        required: true,
      },
    ],
    handle: async (context: DiscordCommandContext) => {
      try {
        const response = await dispatchToOrchestratorResponse(
          orchestratorEndpoint,
          SAVE_TIMEOUT_MS,
          buildBridgePayload({
            eventType: "knowledge.save",
            link: context.getString("link"),
            locale,
            userId: context.userId,
            channelId: context.channelId,
            messageId: context.messageId,
            guildId: context.guildId,
          }),
        );
        // A switched-off or refused request carries a reply and no save result.
        const reply = response.knowledge === undefined && response.reply.trim()
          ? response.reply
          : buildSaveReply(response.knowledge, locale);
        await context.reply(reply);
      } catch (error) {
        console.error("[dokja-discord] command save_document failed", error, JSON.stringify((() => { try { return (globalThis as any).__lastResponse; } catch { return undefined; } })()));
        await context.reply(catalog(locale).failed);
      }
    },
  };
}
