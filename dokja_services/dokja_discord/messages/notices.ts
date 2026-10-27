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
