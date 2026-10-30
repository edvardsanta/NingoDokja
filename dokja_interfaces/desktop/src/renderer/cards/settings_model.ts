import type { RuntimeSetting } from "../../shared/runtime.js";
import type { MessageId } from "../i18n/i18n.js";

// What the settings screen knows about the catalog the runtime controller serves: which settings the
// owner thinks of together, what each one is called, and what an entry of a list has to look like.
// The controller validates every value again; the checks here only say so before the owner saves.

// What an entry of a list is, which decides how it is checked and how it is shown.
export type EntryKind = "channel" | "phone" | "group" | "conversation" | "model";

type FieldSpec = { label: MessageId; help?: MessageId; entry?: EntryKind; unset?: MessageId };

// A setting is named by what it does for the owner, never by its variable or the shape of its value.
export const FIELDS: Readonly<Record<string, FieldSpec>> = {
  "discord.answer_channels": { label: "setting_answer_channels", help: "settings_help_answer_channels", entry: "channel", unset: "setting_every_channel" },
  "discord.direct_messages": { label: "setting_direct_messages", help: "settings_help_direct_messages" },
  "discord.meme_channels": { label: "setting_meme_channels", help: "settings_help_meme_channels", entry: "channel" },
  "discord.safe_only_channels": { label: "setting_safe_only_channels", help: "settings_help_safe_only_channels", entry: "channel" },
  "whatsapp.people": { label: "setting_people", help: "settings_help_people", entry: "phone" },
  "whatsapp.groups": { label: "setting_groups", help: "settings_help_groups", entry: "group" },
  "whatsapp.own_chat": { label: "setting_own_chat", help: "settings_help_own_chat" },
  "whatsapp.conversations": { label: "setting_conversations", help: "settings_help_conversations", entry: "conversation" },
  "models.embedding": { label: "setting_model_embedding" },
  "models.book_classifier": { label: "setting_model_book_classifier", entry: "model" },
  "models.book_overview": { label: "setting_model_book_overview", entry: "model" },
  "models.speech_to_text": { label: "setting_model_speech_to_text", entry: "model" },
  "models.ocr": { label: "setting_model_ocr", entry: "model" },
};

// The sections, in the order the owner meets them: one per thing the owner is trying to do. A section
// that has nothing set says "default" when the stack then behaves well on its own, "empty" when it
// does nothing.
// `takes` names a group whose settings the screen does not know yet: they join this section.
type SectionSpec = { id: string; title: MessageId; about: MessageId; settings: readonly string[]; unset: "default" | "empty"; takes?: string };

const SECTIONS: readonly SectionSpec[] = [
  { id: "discord_replies", title: "settings_section_discord_replies", about: "settings_about_discord_replies", settings: ["discord.answer_channels", "discord.direct_messages"], unset: "default" },
  { id: "discord_memes", title: "settings_section_discord_memes", about: "settings_about_discord_memes", settings: ["discord.meme_channels", "discord.safe_only_channels"], unset: "empty" },
  { id: "whatsapp_access", title: "settings_section_whatsapp_access", about: "settings_about_whatsapp_access", settings: ["whatsapp.people", "whatsapp.groups", "whatsapp.own_chat"], unset: "default" },
  { id: "whatsapp_messages", title: "settings_section_whatsapp_messages", about: "settings_about_whatsapp_messages", settings: ["whatsapp.conversations"], unset: "empty" },
  { id: "models", title: "settings_group_models", about: "settings_about_models", settings: ["models.embedding", "models.book_classifier", "models.book_overview", "models.speech_to_text", "models.ocr"], unset: "default", takes: "models" },
];

export type Section = {
  id: string;
  // A section the screen has no table entry for is named after the group the controller put its
  // settings in, as it came.
  title?: MessageId;
  group?: string;
  about?: MessageId;
  unset: "default" | "empty";
  rows: RuntimeSetting[];
};

// Settings the screen has no place for are never hidden: they follow, under the group they came in.
export function sectionsOf(rows: readonly RuntimeSetting[]): Section[] {
  const known = new Set(SECTIONS.flatMap((section) => section.settings));
  const sections: Section[] = SECTIONS.flatMap((section) => {
    const own = section.settings.flatMap((id) => rows.filter((row) => row.id === id));
    return own.length > 0 ? [{ id: section.id, title: section.title, about: section.about, unset: section.unset, rows: own }] : [];
  });
  for (const row of rows.filter((item) => !known.has(item.id))) {
    const taker = SECTIONS.find((section) => section.takes === row.group);
    const joined = taker && sections.find((section) => section.id === taker.id);
    if (joined) {
      joined.rows.push(row);
      continue;
    }
    const id = `group:${row.group}`;
    const existing = sections.find((section) => section.id === id);
    if (existing) existing.rows.push(row);
    else sections.push({ id, group: row.group, unset: "default", rows: [row] });
  }
  return sections;
}

const CHANNEL = /^[0-9]{1,20}$/;
const PHONE_MARKS = /[\s().+-]/g;
const PHONE = /^[0-9]{7,15}$/;
const GROUP = /^[0-9]+(-[0-9]+)*@g\.us$/;
const ALIAS = /^[a-z0-9][a-z0-9_-]{0,31}$/;
const MODEL = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/;

// The shell refuses a value longer than this, so a list is stopped before it gets there.
export const MAX_VALUE = 2000;

export type Checked = { ok: true; value: string } | { ok: false; reason: MessageId };

export function checkChannel(raw: string): Checked {
  const value = raw.trim();
  return CHANNEL.test(value) ? { ok: true, value } : { ok: false, reason: "settings_invalid_channel" };
}

export function checkPhone(raw: string): Checked {
  const value = raw.trim().replace(PHONE_MARKS, "");
  return PHONE.test(value) ? { ok: true, value } : { ok: false, reason: "settings_invalid_phone" };
}

export function checkGroup(raw: string): Checked {
  const value = raw.trim();
  return GROUP.test(value) ? { ok: true, value } : { ok: false, reason: "settings_invalid_group" };
}

export function checkAlias(raw: string): Checked {
  const value = raw.trim();
  return ALIAS.test(value) ? { ok: true, value } : { ok: false, reason: "settings_invalid_alias" };
}

// The conversation a number or a group id names.
export function checkTarget(raw: string): Checked {
  const value = raw.trim();
  if (GROUP.test(value)) return { ok: true, value };
  const number = checkPhone(value);
  return number.ok ? number : { ok: false, reason: "settings_invalid_target" };
}

// A model's name; empty goes back to the default.
export function checkModel(raw: string): Checked {
  const value = raw.trim();
  return value === "" || MODEL.test(value) ? { ok: true, value } : { ok: false, reason: "settings_invalid_model" };
}

export function checkerOf(kind: EntryKind | undefined): ((raw: string) => Checked) | undefined {
  switch (kind) {
    case "channel": return checkChannel;
    case "phone": return checkPhone;
    case "group": return checkGroup;
    case "model": return checkModel;
    default: return undefined;
  }
}

// A conversation is "alias=target": the alias is what Messages calls it.
export function splitConversation(entry: string): { alias: string; target: string } {
  const at = entry.indexOf("=");
  return at < 0 ? { alias: "", target: entry } : { alias: entry.slice(0, at), target: entry.slice(at + 1) };
}

export function joinConversation(alias: string, target: string): string {
  return `${alias}=${target}`;
}

export const isList = (row: RuntimeSetting) => row.kind === "list";
export const isChoice = (row: RuntimeSetting) => row.kind !== "list" && row.options.length > 0;

// Comma-separated, in the order given, each entry once.
export function parseList(value: string): string[] {
  return [...new Set(value.split(",").map((entry) => entry.trim()).filter(Boolean))];
}

// An empty choice is its default; a list is the same list in any order.
export function effective(row: RuntimeSetting, value: string): string {
  return isChoice(row) && value === "" ? row.default : value;
}

export function isChanged(row: RuntimeSetting, draft: string | undefined): boolean {
  if (draft === undefined) return false;
  if (isList(row)) {
    const before = parseList(row.value);
    const after = parseList(draft);
    return before.length !== after.length || before.some((entry) => !after.includes(entry));
  }
  if (isChoice(row)) return effective(row, row.value) !== effective(row, draft);
  return row.value.trim() !== draft.trim();
}

export type Change = { row: RuntimeSetting; before: string; after: string; added: string[]; removed: string[] };

export function changesOf(rows: readonly RuntimeSetting[], drafts: Readonly<Record<string, string>>): Change[] {
  return rows.flatMap((row) => {
    const draft = drafts[row.id];
    if (row.readOnly || !isChanged(row, draft) || draft === undefined) return [];
    const before = parseList(row.value);
    const after = parseList(draft);
    return [{
      row,
      before: row.value,
      after: draft,
      added: isList(row) ? after.filter((entry) => !before.includes(entry)) : [],
      removed: isList(row) ? before.filter((entry) => !after.includes(entry)) : [],
    }];
  });
}

// What is sent: only what changed, as the text the controller takes.
export function valuesOf(changes: readonly Change[]): Record<string, string> {
  return Object.fromEntries(changes.map((change) => [change.row.id, isList(change.row) ? parseList(change.after).join(",") : change.after.trim()]));
}

// The controller refuses a WhatsApp that would answer nobody: own chat off, and nobody listed. It
// reads an empty own chat as the default, which is on.
export function answersNobody(rows: readonly RuntimeSetting[], drafts: Readonly<Record<string, string>>): boolean {
  const read = (id: string) => {
    const row = rows.find((item) => item.id === id);
    return row ? (drafts[id] ?? row.value) : undefined;
  };
  const own = read("whatsapp.own_chat");
  const people = read("whatsapp.people");
  const groups = read("whatsapp.groups");
  if (own === undefined || people === undefined || groups === undefined) return false;
  return own === "false" && parseList(people).length === 0 && parseList(groups).length === 0;
}

// What would stop a save, found before the controller is asked: setting id -> what is wrong.
export function problemsOf(rows: readonly RuntimeSetting[], drafts: Readonly<Record<string, string>>): Record<string, MessageId> {
  const problems: Record<string, MessageId> = {};
  for (const row of rows) {
    const draft = drafts[row.id];
    if (draft === undefined || row.readOnly) continue;
    if (FIELDS[row.id]?.entry === "model" && !checkModel(draft).ok) problems[row.id] = "settings_invalid_model";
    if (draft.length > MAX_VALUE) problems[row.id] = "settings_too_long";
  }
  if (answersNobody(rows, drafts)) problems["whatsapp.own_chat"] = "settings_nobody_answers";
  return problems;
}

// A refusal names the setting it is about ("whatsapp.people: entry 2 is not a phone number").
export function refusedSetting(message: string, ids: readonly string[]): string | undefined {
  return ids.find((id) => message.includes(`${id}:`));
}

// What saving does: the modules recreated now and on their next start, and what follows.
export function impactOf(row: RuntimeSetting, running: readonly string[]) {
  return {
    now: row.affects.filter((name) => running.includes(name)).sort(),
    later: row.affects.filter((name) => !running.includes(name)).sort(),
    sessions: row.note === "sessions" && running.includes("orchestrator"),
    reindex: row.note === "reindex",
  };
}

// The same for several settings saved at once: a module several of them touch is recreated once.
export function impactOfAll(rows: readonly RuntimeSetting[], running: readonly string[]) {
  const names = [...new Set(rows.flatMap((row) => row.affects))];
  const each = rows.map((row) => impactOf(row, running));
  return {
    now: names.filter((name) => running.includes(name)).sort(),
    later: names.filter((name) => !running.includes(name)).sort(),
    sessions: each.some((impact) => impact.sessions),
    reindex: each.some((impact) => impact.reindex),
  };
}
