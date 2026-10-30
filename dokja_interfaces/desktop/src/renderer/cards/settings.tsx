import { useCallback, useEffect, useId, useMemo, useRef, useState, type RefObject } from "react";

import { isRecord } from "../../shared/records.js";
import type { RuntimeSetting, RuntimeSettings, RuntimeSnapshot } from "../../shared/runtime.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { displayName } from "../display_names.js";
import { useLists, useTranslate } from "../i18n/context.js";
import type { MessageId, Translate } from "../i18n/i18n.js";
import { CardFrame, describeError } from "./card_frame.js";
import { useChannelNames, useGroupNames } from "./settings_lookups.js";
import { FieldFor, SettingsEnv, describeEntry, fieldLabel, optionText, type Env } from "./settings_fields.js";
import {
  FIELDS, changesOf, effective, impactOfAll, isChanged, isChoice, isList, parseList, problemsOf, refusedSetting,
  sectionsOf, splitConversation, valuesOf, type Change, type Section,
} from "./settings_model.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

export { impactOf } from "./settings_model.js";

type View = RuntimeSettings & { running: string[] };

type SectionState = "invalid" | "pending" | "set" | "default" | "empty";

const STATE_WORDS: Record<SectionState, MessageId> = {
  invalid: "attention_title",
  pending: "settings_state_pending",
  set: "settings_state_set",
  default: "settings_state_default",
  empty: "settings_state_empty",
};

const STATE_TONES: Record<SectionState, string> = { invalid: "problem", pending: "unchecked", set: "ok", default: "off", empty: "off" };

// The stack's settings the interfaces may change, kept in the shared local database by the runtime
// controller. Credentials are never here: webhook addresses and tokens are edited in .env by hand.
//
// The owner works through sections, one for each thing to set up, each saying what it holds and
// whether it is set. Every edit joins one draft, held here and not in the sections, so reading the
// stack again never throws it away. One review lists the draft and what saving it recreates, and one
// save sends it whole: the controller refuses it whole or accepts it whole.
export function SettingsCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<View>> => {
    const [answer, modules] = await Promise.all([
      transport.request("runtime.settings", {}, { timeoutMs: 15_000 }),
      transport.request("runtime.modules", {}, { timeoutMs: 15_000 }),
    ]);
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.settings)) return { ok: false, error: { code: "unexpected", message: "unexpected settings reply" } };
    if (!modules.ok) return modules;
    if (!isRecord(modules.result) || !Array.isArray(modules.result.modules)) return { ok: false, error: { code: "unexpected", message: "unexpected modules reply" } };
    const snapshot = modules.result as RuntimeSnapshot;
    if (snapshot.error) return { ok: false, error: { code: "unavailable", message: snapshot.error } };
    const running = snapshot.modules.filter((m) => m.state !== "stopped" && m.state !== "failed").map((m) => m.name);
    return { ok: true, data: { ...(answer.result as RuntimeSettings), busy: Boolean(answer.result.busy || snapshot.busy), running } };
  }, [transport]);
  const { state, reload, poll } = useCard(load);
  const save = useCallback(async (values: Record<string, string>): Promise<LoadResult<true>> => {
    const answer = await transport.request("runtime.settings.set", { values }, { timeoutMs: 15_000 });
    return answer.ok ? { ok: true, data: true } : answer;
  }, [transport]);
  const action = useAction(save);
  const view = state.phase === "ready" ? state.data : undefined;
  const rows = useMemo(() => view?.settings ?? [], [view]);
  const sections = useMemo(() => sectionsOf(rows), [rows]);

  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [reviewing, setReviewing] = useState(false);
  const [failure, setFailure] = useState<TransportError>();
  const [saved, setSaved] = useState(false);
  const changes = useMemo(() => changesOf(rows, drafts), [rows, drafts]);
  const problems = useMemo(() => problemsOf(rows, drafts), [rows, drafts]);
  const refused = failure ? refusedSetting(failure.message, rows.map((row) => row.id)) : undefined;
  const refusalId = useId();

  // Names for the ids on screen: asked once, when a list of them is shown, and never loudly.
  const channels = useChannelNames(transport, rows.some((row) => FIELDS[row.id]?.entry === "channel" && parseList(row.value).length > 0)
    || sections.some((section) => open.has(section.id) && section.rows.some((row) => FIELDS[row.id]?.entry === "channel")));
  const groups = useGroupNames(transport, rows.some((row) => {
    const entry = FIELDS[row.id]?.entry;
    return entry === "group" ? parseList(row.value).length > 0 : entry === "conversation" && parseList(row.value).some((item) => splitConversation(item).target.endsWith("@g.us"));
  }));

  const anchor = useRef<string | undefined>(undefined);
  const toggles = useRef(new Map<string, HTMLButtonElement>());
  const reviewHeading = useRef<HTMLHeadingElement>(null);
  const reviewButton = useRef<HTMLButtonElement>(null);
  const failureText = useRef<HTMLParagraphElement>(null);
  const wasReviewing = useRef(false);
  const focusAnchor = () => { if (anchor.current) toggles.current.get(anchor.current)?.focus(); };

  const saving = action.state.phase === "loading";
  const busy = Boolean(view?.busy) || saving;

  useEffect(() => {
    if (action.state.phase === "ready") {
      setDrafts({});
      setReviewing(false);
      setFailure(undefined);
      setSaved(true);
      poll();
      if (anchor.current) toggles.current.get(anchor.current)?.focus();
    } else if (action.state.phase === "error") {
      setReviewing(false);
      setFailure(action.state.error);
      poll();
    }
  }, [action.state, poll]);
  useEffect(() => {
    if (!view?.busy) return;
    const timer = setTimeout(poll, 1200);
    return () => clearTimeout(timer);
  }, [view, poll]);
  // where a refusal is about is shown, so the owner finds the field
  useEffect(() => {
    const section = refused ? sections.find((item) => item.rows.some((row) => row.id === refused)) : undefined;
    if (section) setOpen((previous) => previous.has(section.id) ? previous : new Set(previous).add(section.id));
  }, [refused, sections]);
  useEffect(() => {
    if (reviewing) reviewHeading.current?.focus();
    else if (wasReviewing.current) reviewButton.current?.focus();
    wasReviewing.current = reviewing;
  }, [reviewing]);
  useEffect(() => { if (failure) failureText.current?.focus(); }, [failure]);

  const set = (row: RuntimeSetting, value: string) => {
    // a draft that is what the stack already has is no draft; text is compared as typed, so a space
    // typed on the way to something else is not taken from the owner
    const same = isList(row) || isChoice(row) ? !isChanged(row, value) : value === row.value;
    setDrafts((previous) => {
      const next = { ...previous };
      if (same) delete next[row.id]; else next[row.id] = value;
      return next;
    });
    anchor.current = sections.find((section) => section.rows.some((item) => item.id === row.id))?.id;
    setReviewing(false);
    setSaved(false);
    setFailure(undefined);
  };
  const discard = () => {
    setDrafts({});
    setReviewing(false);
    setFailure(undefined);
    focusAnchor();
  };
  const submit = () => {
    setFailure(undefined);
    setSaved(false);
    action.start(valuesOf(changes));
  };
  const toggle = (id: string) => setOpen((previous) => {
    const next = new Set(previous);
    if (!next.delete(id)) next.add(id);
    return next;
  });

  const env: Env = { disabled: busy, value: (row) => drafts[row.id] ?? row.value, set, channels, groups, webhookChannels: view?.webhookChannels ?? [], problems, refused, refusalId };
  const blocked = Object.keys(problems).length > 0;
  const live = saving ? t("settings_saving") : view?.busy ? t("settings_busy") : saved ? t("settings_saved") : "";

  return <CardFrame title={t("settings_title")} state={state} onReload={reload}>
    {(data) => <SettingsEnv.Provider value={env}>
      <p className="detail cfg-intro">{t("settings_intro")}</p>
      {data.error && <p role="alert">{t("settings_last_change", { message: data.error })}</p>}
      {sections.length === 0 && !data.error && <p className="detail">{t("setting_none")}</p>}
      <div className="cfg-sections">{sections.map((section) =>
        <SectionView key={section.id} section={section} env={env} changes={changes} open={open.has(section.id)} onToggle={() => toggle(section.id)}
          webhooks={data.webhookChannels.length > 0}
          register={(node) => { if (node) toggles.current.set(section.id, node); else toggles.current.delete(section.id); }} />)}</div>
      <p className="sr-only" aria-live="polite">{live}</p>
      {(changes.length > 0 || reviewing || failure || busy || saved) && <div className="cfg-dock" data-reviewing={reviewing || undefined}>
        {failure && <p ref={failureText} tabIndex={-1} role="alert" id={refusalId} className="cfg-error">
          <span>{describeError(failure, t)}</span> <span>{t("settings_not_saved")}</span>
        </p>}
        {busy && <p className="note" data-tone="progress">{saving ? t("settings_saving") : t("settings_busy")}</p>}
        {saved && !busy && !data.error && changes.length === 0 && <p className="note" data-tone="ok">
          <span>{t("settings_saved")}</span>
          <button type="button" className="quiet compact" onClick={() => setSaved(false)}>{t("settings_dismiss")}</button>
        </p>}
        {changes.length > 0 && !reviewing && <div className="cfg-bar">
          <p className="cfg-count">{t("settings_pending", { count: changes.length })}</p>
          {blocked && <p id={`${refusalId}-blocked`} className="cfg-error">{t("settings_blocked")}</p>}
          <div className="cfg-actions">
            <button type="button" className="quiet compact" disabled={saving} onClick={discard}>{t("settings_discard")}</button>
            <button type="button" className="primary compact" ref={reviewButton} disabled={busy || blocked} aria-describedby={blocked ? `${refusalId}-blocked` : undefined} onClick={() => setReviewing(true)}>{t("setting_review")}</button>
          </div>
        </div>}
        {changes.length > 0 && reviewing && <Review changes={changes} view={data} env={env} headingRef={reviewHeading} busy={busy} onSave={submit} onBack={() => setReviewing(false)} />}
      </div>}
    </SettingsEnv.Provider>}
  </CardFrame>;
}

// What reading a section tells: a problem first, then what waits to be saved, then whether anything
// is set; a section with nothing set says "default" or "empty" as its kind does.
function stateOf(section: Section, env: Env, changes: readonly Change[]): SectionState {
  if (section.rows.some((row) => env.problems[row.id] || env.refused === row.id)) return "invalid";
  if (changes.some((change) => section.rows.some((row) => row.id === change.row.id))) return "pending";
  const set = section.rows.some((row) => {
    if (row.readOnly) return false;
    const value = env.value(row);
    if (isList(row)) return parseList(value).length > 0;
    if (isChoice(row)) return effective(row, value) !== row.default;
    return value.trim() !== "";
  });
  return set ? "set" : section.unset;
}

function SectionView({ section, env, changes, open, onToggle, webhooks, register }: {
  section: Section; env: Env; changes: readonly Change[]; open: boolean; onToggle: () => void; webhooks: boolean; register: (node: HTMLButtonElement | null) => void;
}) {
  const t = useTranslate();
  const join = useLists();
  const id = useId();
  const state = stateOf(section, env, changes);
  const title = section.title ? t(section.title) : displayName(section.group ?? "", t);
  const affects = [...new Set(section.rows.filter((row) => !row.readOnly).flatMap((row) => row.affects))].sort().map((name) => displayName(name, t));
  const holds = (kind: string) => section.rows.some((row) => FIELDS[row.id]?.entry === kind && parseList(env.value(row)).length > 0);
  const holdsGroups = holds("group") || section.rows.some((row) => FIELDS[row.id]?.entry === "conversation" && parseList(env.value(row)).some((entry) => splitConversation(entry).target.endsWith("@g.us")));
  return <section className="cfg-section" data-tone={STATE_TONES[state]} data-open={open || undefined}>
    <h3 className="cfg-heading">
      <button type="button" ref={register} className="cfg-toggle" aria-expanded={open} aria-controls={`${id}-body`} aria-labelledby={`${id}-title ${id}-state`} aria-describedby={`${id}-summary`} onClick={onToggle}>
        <span id={`${id}-title`} className="cfg-title">{title}</span>
        <span id={`${id}-state`} className="cfg-state">{t(STATE_WORDS[state])}</span>
        <span className="cfg-chevron" aria-hidden="true">{open ? "▾" : "▸"}</span>
        <span id={`${id}-summary`} className="cfg-summary">{summaryOf(section, env, t, join).map((part, at) =>
          <span key={at} className="cfg-sum">{part.label && <span className="cfg-sum-label">{part.label}</span>}<span>{part.value}</span></span>)}</span>
      </button>
    </h3>
    {open && <div id={`${id}-body`} className="cfg-body">
      {section.about && <p className="detail cfg-about">{t(section.about)}</p>}
      {affects.length > 0 && <p className="detail cfg-about">{t("settings_affects", { names: join(affects) })}</p>}
      {env.channels.phase === "unavailable" && holds("channel") && <p className="detail cfg-about">{t("settings_channel_names_unavailable")}</p>}
      {env.groups.phase === "unavailable" && holdsGroups && !env.groups.asked && <p className="detail cfg-about">{t("settings_group_names_unavailable")}</p>}
      {webhooks && section.rows.some((row) => row.id === "discord.meme_channels") && <p className="note cfg-about">{t("settings_webhooks_by_hand")}</p>}
      {section.rows.map((row) => <FieldFor key={row.id} row={row} />)}
    </div>}
  </section>;
}

// The collapsed line of a section: each setting by its name and what it holds, as it would be saved.
// A model left at its default is not mentioned.
function summaryOf(section: Section, env: Env, t: Translate, join: (items: readonly string[], plain?: boolean) => string): { label: string; value: string }[] {
  const parts = section.rows.flatMap((row) => {
    if (row.readOnly) return [];
    const value = env.value(row);
    const label = fieldLabel(row, t);
    if (isList(row)) {
      const names = parseList(value).map((entry) => describeEntry(row, entry, env, t).title);
      const text = names.length === 0 ? t(FIELDS[row.id]?.unset ?? "setting_none") : names.length <= 3 ? join(names) : t("settings_and_more", { names: join(names.slice(0, 3), true), count: names.length - 3 });
      return [{ label, value: text }];
    }
    if (isChoice(row)) return [{ label, value: optionText(effective(row, value), t) }];
    return value.trim() === "" ? [] : [{ label, value: value.trim() }];
  });
  return parts.length > 0 ? parts : [{ label: "", value: t("settings_all_default") }];
}

// One look at everything that is about to change, and what saving it does to the modules.
function Review({ changes, view, env, headingRef, busy, onSave, onBack }: {
  changes: readonly Change[]; view: View; env: Env; headingRef: RefObject<HTMLHeadingElement | null>; busy: boolean; onSave: () => void; onBack: () => void;
}) {
  const t = useTranslate();
  const join = useLists();
  const id = useId();
  const impact = impactOfAll(changes.map((change) => change.row), view.running);
  const names = (list: readonly string[]) => join(list.map((name) => displayName(name, t)));
  const wording = (row: RuntimeSetting, value: string) => isChoice(row) ? optionText(effective(row, value), t) : value.trim() || t("setting_default");
  const recorded = changes.some((change) => change.row.id === "whatsapp.conversations" && change.added.length > 0);
  return <section className="cfg-review" aria-labelledby={`${id}-title`}>
    <h3 id={`${id}-title`} ref={headingRef} tabIndex={-1}>{t("settings_review_title")}</h3>
    <ul className="cfg-changes">{changes.map((change) => <li key={change.row.id}>
      <strong>{fieldLabel(change.row, t)}</strong>
      <span className="cfg-change">{isList(change.row)
        ? <>
          {change.added.length > 0 && <span>{t("settings_review_add", { names: join(change.added.map((entry) => describeEntry(change.row, entry, env, t).title)) })}</span>}
          {change.removed.length > 0 && <span>{t("settings_review_remove", { names: join(change.removed.map((entry) => describeEntry(change.row, entry, env, t).title)) })}</span>}
        </>
        : <span>{t("settings_review_change", { from: wording(change.row, change.before), to: wording(change.row, change.after) })}</span>}</span>
    </li>)}</ul>
    {recorded && <p className="note">{t("setting_conversations_recording")}</p>}
    <ul className="cfg-impact">
      {impact.now.length > 0 && <li>{t("setting_recreated_now", { names: names(impact.now) })}</li>}
      {impact.later.length > 0 && <li>{t("setting_recreated_later", { names: names(impact.later) })}</li>}
      {impact.sessions && <li>{t("setting_sessions_end")}</li>}
      {impact.reindex && <li>{t("setting_reindex")}</li>}
    </ul>
    <div className="cfg-actions">
      <button type="button" className="primary compact" disabled={busy} onClick={onSave}>{t("setting_save")}</button>
      <button type="button" className="compact" onClick={onBack}>{t("settings_back")}</button>
    </div>
  </section>;
}
