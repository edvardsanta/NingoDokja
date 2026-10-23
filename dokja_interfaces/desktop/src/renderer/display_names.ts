import type { MessageId, Translate } from "./i18n/i18n.js";

// The names the owner reads for the modules, services, interfaces and jobs the orchestrator reports
// by id. The id stays where it helps an operator (a tooltip, the logs); a label never shows it.
const NAMES: Readonly<Record<string, MessageId>> = {
  "orchestrator": "name_orchestrator",
  "memory": "name_memory",
  "knowledge": "name_knowledge",
  "chat_ai": "name_chat_ai",
  "feeds": "name_feeds",
  "voice": "name_voice",
  "communications": "name_communications",
  "whatsapp": "name_whatsapp",
  "discord": "name_discord",
  "scheduler": "name_scheduler",
  "meme": "name_meme",
  "book": "name_book",
  "meme.refresh": "name_meme_refresh",
  "meme.dispatch": "name_meme_dispatch",
};

// A name the screen does not know is shown as it came, rather than hidden.
export function displayName(id: string, t: Translate): string {
  const key = NAMES[id];
  return key ? t(key) : id;
}
