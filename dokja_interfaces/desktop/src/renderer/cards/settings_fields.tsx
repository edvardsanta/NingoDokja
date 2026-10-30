import { createContext, useContext, useId, useState, type FormEvent } from "react";

import type { CommunicationChannel } from "../../shared/replies.js";
import type { RuntimeSetting } from "../../shared/runtime.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId, Translate } from "../i18n/i18n.js";
import { ChannelPicker, GroupDiscovery, conversationAlias } from "./group_discovery.js";
import type { Groups, Lookup } from "./settings_lookups.js";
import {
  FIELDS, MAX_VALUE, checkAlias, checkChannel, checkGroup, checkPhone, checkTarget, effective,
  isChanged, isChoice, isList, joinConversation, parseList, splitConversation, type Checked,
} from "./settings_model.js";

// What every field of the screen shares: the draft, the names found for the ids, and whether the
// stack lets the owner edit right now. Held by the card, so a reload does not take the draft away.
export type Env = {
  disabled: boolean;
  value: (row: RuntimeSetting) => string;
  set: (row: RuntimeSetting, value: string) => void;
  channels: Lookup<CommunicationChannel>;
  groups: Groups;
  webhookChannels: readonly string[];
  // what the owner has to fix before saving, by setting
  problems: Readonly<Record<string, MessageId>>;
  // the setting the controller refused, and the text that says why
  refused: string | undefined;
  refusalId: string;
};

export const SettingsEnv = createContext<Env | null>(null);

function useEnv(): Env {
  const env = useContext(SettingsEnv);
  if (!env) throw new Error("a settings field needs its card");
  return env;
}

const OPTIONS: Record<string, MessageId> = {
  open: "setting_option_open",
  closed: "setting_option_closed",
  true: "setting_option_true",
  false: "setting_option_false",
};

// The words for a closed set of values; a value outside it is shown as it came.
export function optionText(option: string, t: Translate): string {
  const id = OPTIONS[option];
  return id ? t(id) : option;
}

// A setting the screen has no name for is called by its id, as it came.
export function fieldLabel(row: RuntimeSetting, t: Translate): string {
  const spec = FIELDS[row.id];
  return spec ? t(spec.label) : row.id;
}

// What an entry of a list is called, and what else is known about it. The name leads and the id
// follows; an entry nobody has a name for is its id.
export function describeEntry(row: RuntimeSetting, entry: string, env: Pick<Env, "channels" | "groups" | "webhookChannels">, t: Translate): { title: string; detail: string[] } {
  switch (FIELDS[row.id]?.entry) {
    case "channel": {
      const known = env.channels.items.find((channel) => channel.id === entry);
      const named = known && known.name && known.name !== entry ? known : undefined;
      const via = row.id === "discord.meme_channels" ? t(env.webhookChannels.includes(entry) ? "settings_via_webhook" : "settings_via_bot") : "";
      return { title: named ? `#${named.name}` : entry, detail: [named?.guildName ?? "", named ? entry : "", via].filter(Boolean) };
    }
    case "group": {
      const group = env.groups.items.find((item) => item.id === entry);
      return group ? { title: group.name || t("groups_unnamed"), detail: [t("groups_members", { count: String(group.members) }), entry] } : { title: entry, detail: [] };
    }
    case "conversation": {
      const { alias, target } = splitConversation(entry);
      const group = env.groups.items.find((item) => item.id === target);
      return { title: alias || target, detail: [group ? group.name || t("groups_unnamed") : "", target].filter(Boolean) };
    }
    default:
      return { title: entry, detail: [] };
  }
}

const joinIds = (...ids: (string | undefined | false)[]) => ids.filter(Boolean).join(" ") || undefined;

export function FieldFor({ row }: { row: RuntimeSetting }) {
  if (row.readOnly) return <FixedField row={row} />;
  if (isList(row)) return <ListField row={row} />;
  if (isChoice(row)) return <ChoiceField row={row} />;
  return <TextField row={row} />;
}

// The name of a field, what it means, and the word that says the owner has changed it.
function Head({ row, labelId, helpId, changed, labelFor }: { row: RuntimeSetting; labelId: string; helpId: string; changed: boolean; labelFor?: string }) {
  const t = useTranslate();
  const help = FIELDS[row.id]?.help;
  const label = fieldLabel(row, t);
  return <>
    <div className="cfg-field-head">
      {labelFor ? <label id={labelId} htmlFor={labelFor} className="cfg-label">{label}</label> : <span id={labelId} className="cfg-label">{label}</span>}
      {changed && <span className="cfg-changed">{t("settings_field_changed")}</span>}
    </div>
    {help && <p id={helpId} className="detail cfg-help">{t(help)}</p>}
  </>;
}

function Problem({ id, reason }: { id: string; reason: MessageId | undefined }) {
  const t = useTranslate();
  return reason ? <p id={id} role="alert" className="cfg-error">{t(reason)}</p> : null;
}

function ListField({ row }: { row: RuntimeSetting }) {
  const env = useEnv();
  const t = useTranslate();
  const id = useId();
  const kind = FIELDS[row.id]?.entry;
  const base = parseList(row.value);
  const draft = env.value(row);
  const final = parseList(draft);
  // what was there stays in its place, struck when it is to go; what is new follows
  const entries = [...base, ...final.filter((entry) => !base.includes(entry))];
  const changed = isChanged(row, draft);
  const commit = (next: readonly string[]) => env.set(row, next.join(","));
  const add = (entry: string) => commit([...final, entry]);
  const problem = env.problems[row.id];
  const refused = env.refused === row.id;
  return <div role="group" className="cfg-field" data-changed={changed || undefined} aria-labelledby={`${id}-label`}
    aria-describedby={joinIds(FIELDS[row.id]?.help && `${id}-help`, problem && `${id}-problem`, refused && env.refusalId)}>
    <Head row={row} labelId={`${id}-label`} helpId={`${id}-help`} changed={changed} />
    {kind === "conversation" && <p className="note">{t("setting_conversations_recording")}</p>}
    {entries.length === 0
      ? <p className="detail cfg-empty">{t(FIELDS[row.id]?.unset ?? "setting_none")}</p>
      : <ul className="cfg-entries">{entries.map((entry) => {
        const state = !final.includes(entry) ? "removed" : base.includes(entry) ? "kept" : "added";
        const { title, detail } = describeEntry(row, entry, env, t);
        return <li key={entry} data-state={state}>
          <span className="cfg-entry-text">
            <strong className="cfg-entry-title">{title}</strong>
            {detail.map((part, at) => <span key={at} className="detail cfg-entry-detail">{part}</span>)}
            {state !== "kept" && <span className="cfg-entry-state">{t(state === "added" ? "settings_entry_new" : "settings_entry_removed")}</span>}
          </span>
          {state === "removed"
            ? <button type="button" className="quiet compact" disabled={env.disabled} aria-label={t("settings_undo_named", { name: title })} onClick={() => add(entry)}>{t("settings_undo")}</button>
            : <button type="button" className="quiet compact" disabled={env.disabled} aria-label={t("list_remove_named", { name: title })} onClick={() => commit(final.filter((item) => item !== entry))}>{t("list_remove")}</button>}
        </li>;
      })}</ul>}
    <Problem id={`${id}-problem`} reason={problem} />
    {kind === "conversation"
      ? <AddConversation entries={final} disabled={env.disabled} onAdd={add} />
      : <AddEntry label={t(addLabel(kind))} check={checkerFor(kind)} current={final} disabled={env.disabled} onAdd={add} inputMode={kind === "phone" ? "tel" : kind === "channel" ? "numeric" : "text"} />}
    {kind === "channel" && <ChannelPicker channels={env.channels} chosen={final} disabled={env.disabled} onAdd={add} />}
    {kind === "group" && <GroupDiscovery groups={env.groups} chosen={final} disabled={env.disabled} onAdd={(group) => add(group.id)} />}
    {kind === "conversation" && <GroupDiscovery groups={env.groups} chosen={final.map((entry) => splitConversation(entry).target)} disabled={env.disabled}
      onAdd={(group) => add(joinConversation(conversationAlias(group.name, final.map((entry) => splitConversation(entry).alias)), group.id))} />}
  </div>;
}

function addLabel(kind: string | undefined): MessageId {
  switch (kind) {
    case "channel": return "settings_add_channel";
    case "phone": return "settings_add_person";
    case "group": return "settings_add_group";
    default: return "settings_add_entry";
  }
}

function checkerFor(kind: string | undefined): (raw: string) => Checked {
  switch (kind) {
    case "channel": return checkChannel;
    case "phone": return checkPhone;
    case "group": return checkGroup;
    // a setting the screen does not know: any text that keeps the list's commas apart
    default: return (raw) => {
      const value = raw.trim();
      return value !== "" && !value.includes(",") ? { ok: true, value } : { ok: false, reason: "settings_invalid_entry" };
    };
  }
}

// One entry typed in: checked before it is added, and the reason said beside the field.
function AddEntry({ label, check, current, disabled, onAdd, inputMode }: {
  label: string; check: (raw: string) => Checked; current: readonly string[]; disabled: boolean; onAdd: (entry: string) => void; inputMode: "text" | "tel" | "numeric";
}) {
  const t = useTranslate();
  const id = useId();
  const [text, setText] = useState("");
  const [error, setError] = useState<MessageId>();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const checked = check(text);
    if (!checked.ok) return setError(checked.reason);
    if (current.includes(checked.value)) return setError("settings_duplicate");
    if ([...current, checked.value].join(",").length > MAX_VALUE) return setError("settings_too_long");
    onAdd(checked.value);
    setText("");
    setError(undefined);
  };
  return <form className="cfg-add" onSubmit={submit} noValidate>
    <label htmlFor={id}>{label}</label>
    <div className="cfg-add-row">
      <input id={id} value={text} inputMode={inputMode} maxLength={128} autoComplete="off" disabled={disabled}
        aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-error` : undefined}
        onChange={(event) => { setText(event.target.value); setError(undefined); }} />
      <button type="submit" className="compact" disabled={disabled || text.trim() === ""}>{t("list_add")}</button>
    </div>
    {error && <p id={`${id}-error`} role="alert" className="cfg-error">{t(error)}</p>}
  </form>;
}

// A conversation is named by an alias, which Messages shows, and a number or a group id.
function AddConversation({ entries, disabled, onAdd }: { entries: readonly string[]; disabled: boolean; onAdd: (entry: string) => void }) {
  const t = useTranslate();
  const id = useId();
  const [alias, setAlias] = useState("");
  const [target, setTarget] = useState("");
  const [error, setError] = useState<{ field: "alias" | "target"; reason: MessageId }>();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const named = checkAlias(alias);
    if (!named.ok) return setError({ field: "alias", reason: named.reason });
    const goal = checkTarget(target);
    if (!goal.ok) return setError({ field: "target", reason: goal.reason });
    const taken = entries.map(splitConversation);
    if (taken.some((entry) => entry.alias === named.value)) return setError({ field: "alias", reason: "settings_alias_taken" });
    if (taken.some((entry) => entry.target === goal.value)) return setError({ field: "target", reason: "settings_conversation_taken" });
    const entry = joinConversation(named.value, goal.value);
    if ([...entries, entry].join(",").length > MAX_VALUE) return setError({ field: "target", reason: "settings_too_long" });
    onAdd(entry);
    setAlias("");
    setTarget("");
    setError(undefined);
  };
  const invalid = (field: "alias" | "target") => error?.field === field;
  return <form className="cfg-add" onSubmit={submit} noValidate>
    <div className="cfg-add-pair">
      <div>
        <label htmlFor={`${id}-alias`}>{t("settings_add_alias")}</label>
        <input id={`${id}-alias`} value={alias} maxLength={32} autoComplete="off" disabled={disabled} aria-invalid={invalid("alias") ? true : undefined} aria-describedby={invalid("alias") ? `${id}-error` : undefined}
          onChange={(event) => { setAlias(event.target.value); setError(undefined); }} />
      </div>
      <div>
        <label htmlFor={`${id}-target`}>{t("settings_add_target")}</label>
        <input id={`${id}-target`} value={target} maxLength={128} autoComplete="off" disabled={disabled} aria-invalid={invalid("target") ? true : undefined} aria-describedby={invalid("target") ? `${id}-error` : undefined}
          onChange={(event) => { setTarget(event.target.value); setError(undefined); }} />
      </div>
      <button type="submit" className="compact" disabled={disabled || alias.trim() === "" || target.trim() === ""}>{t("list_add")}</button>
    </div>
    {error && <p id={`${id}-error`} role="alert" className="cfg-error">{t(error.reason)}</p>}
  </form>;
}

// A closed set of values: each one is a choice, the chosen one is lit.
function ChoiceField({ row }: { row: RuntimeSetting }) {
  const env = useEnv();
  const t = useTranslate();
  const id = useId();
  const draft = env.value(row);
  const changed = isChanged(row, draft);
  const problem = env.problems[row.id];
  const refused = env.refused === row.id;
  return <div className="cfg-field" data-changed={changed || undefined}>
    <Head row={row} labelId={`${id}-label`} helpId={`${id}-help`} changed={changed} />
    <div role="radiogroup" className="cfg-choice" aria-labelledby={`${id}-label`} aria-invalid={problem || refused ? true : undefined}
      aria-describedby={joinIds(FIELDS[row.id]?.help && `${id}-help`, problem && `${id}-problem`, refused && env.refusalId)}>
      {row.options.map((option) => <label key={option}>
        <input type="radio" name={id} value={option} checked={effective(row, draft) === option} disabled={env.disabled} onChange={() => env.set(row, option)} />
        <span>{optionText(option, t)}</span>
      </label>)}
    </div>
    <Problem id={`${id}-problem`} reason={problem} />
  </div>;
}

// A model's name: empty goes back to the default, which is said under the field.
function TextField({ row }: { row: RuntimeSetting }) {
  const env = useEnv();
  const t = useTranslate();
  const id = useId();
  const [touched, setTouched] = useState(false);
  const draft = env.value(row);
  const changed = isChanged(row, draft);
  const problem = touched ? env.problems[row.id] : undefined;
  const refused = env.refused === row.id;
  return <div className="cfg-field" data-changed={changed || undefined}>
    <Head row={row} labelId={`${id}-label`} helpId={`${id}-help`} changed={changed} labelFor={id} />
    <input id={id} value={draft} placeholder={row.default} maxLength={128} autoComplete="off" spellCheck={false} disabled={env.disabled}
      aria-invalid={problem || refused ? true : undefined} aria-describedby={joinIds(`${id}-default`, problem && `${id}-problem`, refused && env.refusalId)}
      onBlur={() => setTouched(true)} onChange={(event) => env.set(row, event.target.value)} />
    <p id={`${id}-default`} className="detail cfg-help">{row.default ? t("settings_default_is", { value: row.default }) : t("settings_default_builtin")}</p>
    <Problem id={`${id}-problem`} reason={problem} />
  </div>;
}

// What the controller fixes for this version: shown, never offered for editing.
function FixedField({ row }: { row: RuntimeSetting }) {
  const t = useTranslate();
  const id = useId();
  return <div className="cfg-field cfg-fixed">
    <div className="cfg-field-head">
      <span id={`${id}-label`} className="cfg-label">{fieldLabel(row, t)}</span>
    </div>
    <p className="cfg-fixed-value">
      <strong>{row.value || row.default}</strong>
      {!row.value && <span className="detail">{t("setting_default")}</span>}
    </p>
    <p className="detail cfg-help">{t("setting_fixed_profile")}</p>
  </div>;
}
