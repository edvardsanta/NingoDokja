// What the interface says when the orchestrator itself could not take the turn
// (unreachable or over its deadline): the sender waited for an answer, so the
// interface promises a late retry instead of asking them to re-send by hand.
export function dispatchFailureNotice(locale = "pt-BR"): string {
  const messages: Record<string, string> = {
    "pt-BR": "Não consegui processar essa mensagem agora — a conexão com o orquestrador falhou. Vou tentar de novo mais tarde e volto aqui com a resposta.",
    en: "I could not process that message right now — the connection to the orchestrator failed. I will try again later and come back with the answer here.",
  };
  return messages[locale] ?? messages[locale.split("-")[0]] ?? messages.en;
}

// What the interface says when the late retries ran out without an answer.
export function dispatchRetriesExhaustedNotice(locale = "pt-BR"): string {
  const messages: Record<string, string> = {
    "pt-BR": "Tentei de novo algumas vezes e ainda não consegui processar essa mensagem. Manda de novo quando puder.",
    en: "I retried a few times and still could not process that message. Send it again when you can.",
  };
  return messages[locale] ?? messages[locale.split("-")[0]] ?? messages.en;
}

export function localizeNotice(notice: string | undefined, reply: string, locale = "pt-BR"): string {
  const messages: Record<string, Record<string, string>> = {
    "chat.context_compacted": {
      "pt-BR": "O contexto da conversa foi compactado.",
      en: "Conversation context was compacted.",
    },
    "chat.session_started": {
      "pt-BR": "Nova sessão de conversa iniciada.",
      en: "New conversation session started.",
    },
    "chat.math_render_failed": {
      "pt-BR": "Algumas fórmulas não puderam ser geradas como imagem. As versões em texto estão abaixo.",
      en: "Some formula images could not be generated. The formulas are included as text.",
    },
  };
  const translations = notice ? messages[notice] : undefined;
  const translated = translations?.[locale] ?? translations?.[locale.split("-")[0]] ?? translations?.en;
  if (!translated) return reply;
  return reply.trim() ? `${translated}\n\n${reply}` : translated;
}
