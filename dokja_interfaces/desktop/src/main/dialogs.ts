import type { ActionType } from "../shared/actions.js";
import type { Locale } from "../shared/locale.js";

// The question the shell asks, in a window of its own, before an action that cannot be undone. It is
// asked by the main process, not by the page: a page that was tricked into asking cannot answer for
// the person, and cannot change what the window says. The window names what is about to be
// deleted by its id, which is what the shell knows, not by a title the page could make up.
export type Question = { message: string; detail: string; accept: string; cancel: string };

const TEXTS: Record<Locale, { message: string; detail: (id: string) => string; accept: string; cancel: string }> = {
  en: {
    message: "Delete this document?",
    detail: (id) => `${id}\n\nIts passages are removed from the research base. This cannot be undone.`,
    accept: "Delete",
    cancel: "Cancel",
  },
  pt: {
    message: "Apagar este documento?",
    detail: (id) => `${id}\n\nOs trechos dele saem da base de pesquisa. Isso não pode ser desfeito.`,
    accept: "Apagar",
    cancel: "Cancelar",
  },
};

// Asks the person, in a window the shell owns, whether to go on. True means yes.
export type Confirm = (type: ActionType, wire: Record<string, unknown>) => Promise<boolean>;

// Shows one question and answers whether the person accepted it.
export type ShowQuestion = (question: Question) => Promise<boolean>;

// One question at a time: while one is open, another request to ask is a no, so a page that was
// tricked into asking again and again cannot pile windows on the person.
export function createConfirmer(locale: Locale, show: ShowQuestion): Confirm {
  let open = false;
  return async (type, wire) => {
    const question = questionFor(locale, type, wire);
    if (!question || open) return false;
    open = true;
    try {
      return await show(question);
    } finally {
      open = false;
    }
  };
}

export function questionFor(locale: Locale, type: ActionType, wire: Record<string, unknown>): Question | undefined {
  if (type !== "knowledge.delete") return undefined;
  const texts = TEXTS[locale] ?? TEXTS.en;
  return {
    message: texts.message,
    detail: texts.detail(String(wire.source_id ?? "")),
    accept: texts.accept,
    cancel: texts.cancel,
  };
}
