import { isRecord, text } from "./records.js";

export type PassageLocator = {
  version: string; format: "pdf" | "epub"; unit: number; start: number; end: number;
  page?: number; href?: string; label?: string; passage_id?: string;
};
export type OriginalReading = {
  state: "resolved" | "unavailable" | "unresolved";
  format?: "pdf" | "epub"; version?: string; file_hash?: string; filename?: string; content_b64?: string;
  locator?: PassageLocator; unit?: { text: string; label: string; page?: number; href?: string };
  navigation?: { label: string; page?: number; href?: string; chars?: number }[];
};
export function passageLocator(raw: unknown): PassageLocator | undefined {
  if (!isRecord(raw) || !/^[a-f0-9]{64}$/.test(text(raw.version)) || !["pdf", "epub"].includes(text(raw.format))) return undefined;
  const { unit, start, end } = raw;
  if (![unit, start, end].every((n) => typeof n === "number" && Number.isSafeInteger(n) && n >= 0) || (end as number) <= (start as number)) return undefined;
  if (raw.format === "pdf" && (typeof raw.page !== "number" || raw.page !== (unit as number) + 1)) return undefined;
  if (raw.format === "epub" && (!text(raw.href) || text(raw.href).length > 2000 || /(^\/|:|\\)/.test(text(raw.href)) || text(raw.href).split("/").includes(".."))) return undefined;
  return { version: text(raw.version), format: raw.format as "pdf" | "epub", unit: unit as number, start: start as number, end: end as number,
    ...(typeof raw.page === "number" ? { page: raw.page } : {}), ...(typeof raw.href === "string" ? { href: raw.href } : {}),
    label: text(raw.label).slice(0, 300), passage_id: text(raw.passage_id).slice(0, 64) };
}
export function originalReading(raw: unknown): OriginalReading | undefined {
  if (!isRecord(raw)) return undefined;
  if (raw.state === "unavailable" || raw.state === "unresolved") return { state: raw.state };
  const locator = passageLocator(raw.locator);
  if (raw.state !== "resolved" || !locator || !isRecord(raw.unit) || typeof raw.unit.text !== "string" || typeof raw.content_b64 !== "string" || raw.version !== locator.version || raw.format !== locator.format || locator.end > Array.from(raw.unit.text).length) return { state: "unresolved" };
  return { state: "resolved", locator, version: locator.version, format: locator.format,
    file_hash: text(raw.file_hash), filename: text(raw.filename), content_b64: raw.content_b64,
    unit: { text: raw.unit.text, label: text(raw.unit.label), page: typeof raw.unit.page === "number" ? raw.unit.page : undefined, href: text(raw.unit.href) },
    navigation: Array.isArray(raw.navigation) ? raw.navigation.slice(0, 5000).filter(isRecord).map((u) => ({ label: text(u.label), chars: typeof u.chars === "number" ? u.chars : 0, page: typeof u.page === "number" ? u.page : undefined, href: text(u.href) })) : [] };
}
// Python spans count Unicode code points, whereas JavaScript string slices count UTF-16 units.
export function passageText(text: string, start: number, end: number): [string, string, string] {
  const points = Array.from(text);
  return [points.slice(0, start).join(""), points.slice(start, end).join(""), points.slice(end).join("")];
}
