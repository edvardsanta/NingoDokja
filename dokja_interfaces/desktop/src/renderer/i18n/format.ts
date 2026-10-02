import type { Locale } from "../../shared/locale.js";

const TAGS: Record<Locale, string> = { en: "en", pt: "pt-BR" };

// Numbers follow the language: 0.081 in English, 0,081 in Portuguese.
export function formatNumber(
  locale: Locale,
  value: number,
  digits: number,
  options: { signed?: boolean } = {},
): string {
  return new Intl.NumberFormat(TAGS[locale], {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
    ...(options.signed ? { signDisplay: "always" as const } : {}),
  }).format(value);
}
