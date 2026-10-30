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

// Names in a row as the language says them: "A, B and C", "A, B e C". Plain leaves the "and" out, for a
// row that goes on ("A, B, C and 2 more").
export function formatList(locale: Locale, items: readonly string[], plain = false): string {
  return new Intl.ListFormat(TAGS[locale], { style: plain ? "narrow" : "long", type: "conjunction" }).format(items);
}

const AGO_UNITS: ReadonlyArray<readonly [Intl.RelativeTimeFormatUnit, number]> = [
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
];

// How long ago an RFC 3339 time was, as the language says it ("3 hours ago", "ha 3 horas"). Empty
// when the text is not a date. A time a little ahead of the clock reads as now.
export function formatAgo(locale: Locale, iso: string, nowMs: number = Date.now()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const seconds = Math.min(Math.round((then - nowMs) / 1000), 0);
  const format = new Intl.RelativeTimeFormat(TAGS[locale], { numeric: "auto" });
  for (const [unit, size] of AGO_UNITS) {
    if (-seconds >= size) return format.format(Math.trunc(seconds / size), unit);
  }
  return format.format(0, "second");
}

// Capture instants stay in UTC on the wire; display uses the desktop timezone.
export function formatTime(locale: Locale, iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(TAGS[locale], {
    hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23",
  }).format(date);
}

// The day of an RFC 3339 time, as the language writes a date; empty when the text is not a date.
export function formatDay(locale: Locale, iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(TAGS[locale], { day: "numeric", month: "short", year: "numeric" }).format(date);
}
