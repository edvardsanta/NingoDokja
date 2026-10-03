import type { Locale } from "../../shared/locale.js";
import en from "./locales/en.json";
import pt from "./locales/pt.json";

export type MessageId = keyof typeof en;
export type Translate = (id: MessageId, params?: Record<string, string | number>) => string;

export const CATALOGS: Record<Locale, Record<string, string>> = { en, pt };

// English is the default and the fallback: a message missing from another catalog shows in
// English, never as its id. {name} placeholders are filled from params.
export function createTranslator(locale: Locale): Translate {
  return (id, params) => {
    const template = CATALOGS[locale][id] ?? en[id];
    return template.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
      params && name in params ? String(params[name]) : placeholder,
    );
  };
}
