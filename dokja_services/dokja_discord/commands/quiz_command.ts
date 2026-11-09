import { ApplicationCommandOptionType } from "discord-api-types/v10";

import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordCommandContext, DiscordReply, DiscordSlashCommand } from "../types.js";

// A run reads the questions, answers them in a few model calls and clicks through the site once.
// The orchestrator gives it 13 minutes; Discord keeps a deferred reply open for 15.
export const QUIZ_TIMEOUT_MS = 14 * 60 * 1000;

type Catalog = Record<string, string>;

// Every word the reply shows comes from here, keyed by the codes the Quiz domain returns.
const messages: Record<string, Catalog> = {
  "pt-BR": {
    done: "Fiz o teste: {answered} respostas. O site me chamou de **{label}**.",
    done_unlabeled: "Fiz o teste: {answered} respostas.",
    link: "Resultado: {url}",
    invalid_link: "Isso não é um link de teste que eu consiga abrir.",
    busy: "Já estou fazendo um teste. Espere eu terminar.",
    no_runner: "Não tenho nenhum executor de testes configurado.",
    unsupported: "Não sei fazer esse teste.",
    unavailable: "O teste não está disponível agora.",
    unanswered: "Não consegui responder todas as perguntas, então não enviei nada.",
    failed: "O teste falhou no meio do caminho. Não vou inventar um resultado.",
  },
  en: {
    done: "I took the test: {answered} answers. The site called me **{label}**.",
    done_unlabeled: "I took the test: {answered} answers.",
    link: "Result: {url}",
    invalid_link: "That is not a test link I can open.",
    busy: "I am already taking a test. Wait for me to finish.",
    no_runner: "I have no test runner configured.",
    unsupported: "I do not know how to take that test.",
    unavailable: "The test is not available right now.",
    unanswered: "I could not answer every question, so I submitted nothing.",
    failed: "The test failed midway. I will not make up a result.",
  },
};

function catalog(locale = "pt-BR"): Catalog {
  return messages[locale] ?? messages[locale.split("-")[0]] ?? messages.en;
}

function text(words: Catalog, key: string, values: Record<string, string | number> = {}): string {
  let value = words[key] ?? words.failed;
  for (const [name, replacement] of Object.entries(values)) {
    value = value.replace(`{${name}}`, String(replacement));
  }
  return value;
}

// buildQuizReply words a run's result or refusal; exported for tests.
export function buildQuizReply(run: unknown, locale = "pt-BR"): DiscordReply {
  const words = catalog(locale);
  const result = (run ?? {}) as Record<string, unknown>;
  if (result.status !== "ok" || typeof result.url !== "string" || !result.url) {
    return text(words, String(result.refused ?? "failed"));
  }
  const answered = typeof result.answered === "number" ? result.answered : 0;
  const label = typeof result.label === "string" ? result.label.trim() : "";
  const lines = [label ? text(words, "done", { answered, label }) : text(words, "done_unlabeled", { answered })];
  const comment = typeof result.comment === "string" ? result.comment.trim() : "";
  if (comment) {
    lines.push("", comment);
  }
  lines.push("", text(words, "link", { url: result.url }));
  return lines.join("\n");
}

export function createQuizCommand(orchestratorEndpoint: string, locale?: string): DiscordSlashCommand {
  return {
    name: "political_test",
    description: "Ningo takes an online questionnaire himself and posts his result link.",
    options: [
      {
        name: "link",
        description: "Link to the questionnaire",
        type: ApplicationCommandOptionType.String,
        required: true,
      },
    ],
    handle: async (context: DiscordCommandContext) => {
      try {
        const response = await dispatchToOrchestratorResponse(
          orchestratorEndpoint,
          QUIZ_TIMEOUT_MS,
          buildBridgePayload({
            eventType: "quiz.take",
            content: context.getString("link"),
            locale,
            userId: context.userId,
            channelId: context.channelId,
            messageId: context.messageId,
            guildId: context.guildId,
          }),
        );
        // A switched-off or refused request carries a reply and no quiz result.
        const reply = response.quiz === undefined && response.reply.trim()
          ? response.reply
          : buildQuizReply(response.quiz, locale);
        await context.reply(reply);
      } catch (error) {
        console.error("[dokja-discord] command teste_politico failed", error);
        await context.reply(text(catalog(locale), "failed"));
      }
    },
  };
}
