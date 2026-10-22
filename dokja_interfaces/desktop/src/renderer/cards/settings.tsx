import { useCallback, useEffect, useId, useState } from "react";

import { isRecord } from "../../shared/records.js";
import type { RuntimeSetting, RuntimeSettings, RuntimeSnapshot } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { CardFrame, Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

// Product names are the same in every language; only "models" is translated.
const GROUPS: readonly { id: string; name?: string; labelId?: MessageId }[] = [
  { id: "discord", name: "Discord" },
  { id: "whatsapp", name: "WhatsApp" },
  { id: "models", labelId: "settings_group_models" },
];

const LABELS: Record<string, MessageId> = {
  "discord.meme_channels": "setting_meme_channels",
  "discord.safe_only_channels": "setting_safe_only_channels",
  "discord.answer_channels": "setting_answer_channels",
  "discord.direct_messages": "setting_direct_messages",
  "whatsapp.people": "setting_people",
  "whatsapp.groups": "setting_groups",
  "whatsapp.own_chat": "setting_own_chat",
  "models.embedding": "setting_model_embedding",
  "models.book_classifier": "setting_model_book_classifier",
  "models.book_overview": "setting_model_book_overview",
  "models.speech_to_text": "setting_model_speech_to_text",
  "models.ocr": "setting_model_ocr",
};

type View = RuntimeSettings & { running: string[] };

// What saving a setting does: the modules recreated now and on their next start, and what follows.
export function impactOf(row: RuntimeSetting, running: readonly string[]) {
  return {
    now: row.affects.filter((name) => running.includes(name)).sort(),
    later: row.affects.filter((name) => !running.includes(name)).sort(),
    sessions: row.note === "sessions" && running.includes("orchestrator"),
    reindex: row.note === "reindex",
  };
}

// The stack's settings the interfaces may change, kept in the shared local database by the runtime
// controller. Credentials are never here: webhook addresses and tokens are edited in .env by hand.
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
  const save = useCallback(async (change: { id: string; value: string }): Promise<LoadResult<true>> => {
    const answer = await transport.request("runtime.settings.set", { values: { [change.id]: change.value } }, { timeoutMs: 15_000 });
    return answer.ok ? { ok: true, data: true } : answer;
  }, [transport]);
  const action = useAction(save);
  const view = state.phase === "ready" ? state.data : undefined;

  useEffect(() => {
    if (action.state.phase === "ready" || action.state.phase === "error") reload();
  }, [action.state, reload]);
  useEffect(() => {
    if (!view?.busy) return;
    const timer = setTimeout(poll, 1200);
    return () => clearTimeout(timer);
  }, [view, poll]);

  return <CardFrame kindLabel={t("nav_system")} title={t("settings_title")} state={state} onReload={reload}>
    {(data) => <>
      <p className="detail">{t("settings_intro")}</p>
      {data.error && <p role="alert">{t("settings_last_change", { message: data.error })}</p>}
      {action.state.phase === "error" && <Problem error={action.state.error} />}
      {GROUPS.map((group) => <section key={group.id} className="settings-group">
        <h3>{group.name ?? (group.labelId && t(group.labelId))}</h3>
        <ul className="services settings">{data.settings.filter((row) => row.group === group.id).map((row) =>
          <SettingRow key={row.id} row={row} view={data} busy={data.busy || action.state.phase === "loading"} onSave={(value) => action.start({ id: row.id, value })} />)}</ul>
      </section>)}
      {data.webhookChannels.length > 0 && <p className="note">{t("settings_webhooks_by_hand")}</p>}
    </>}
  </CardFrame>;
}

function SettingRow({ row, view, busy, onSave }: { row: RuntimeSetting; view: View; busy: boolean; onSave: (value: string) => void }) {
  const t = useTranslate();
  const id = useId();
  const [draft, setDraft] = useState<string>();
  const [reviewing, setReviewing] = useState(false);
  const label = t(LABELS[row.id] ?? "settings_title");
  const impact = impactOf(row, view.running);
  const close = () => { setDraft(undefined); setReviewing(false); };
  return <li>
    <span className="service-name">{label}</span>
    <span className="service-status">{valueText(row, view, t)}</span>
    {row.readOnly ? <span className="detail">{t("setting_fixed_profile")}</span> : draft === undefined
      ? <button type="button" disabled={busy} aria-label={t("setting_edit_named", { name: label })} onClick={() => setDraft(row.value || (row.options.length ? row.default : ""))}>{t("setting_edit")}</button>
      : <form className="setting-edit" onSubmit={(event) => { event.preventDefault(); setReviewing(true); }}>
        <label htmlFor={id}>{label}</label>
        {row.options.length > 0
          ? <select id={id} value={draft} onChange={(event) => { setDraft(event.target.value); setReviewing(false); }}>
            {row.options.map((option) => <option key={option} value={option}>{option}</option>)}
          </select>
          : <input id={id} value={draft} maxLength={2000} autoComplete="off" onChange={(event) => { setDraft(event.target.value); setReviewing(false); }} />}
        <p className="detail">{t(row.kind === "list" ? "setting_hint_list" : row.kind === "text" ? "setting_hint_model" : "setting_hint_options")}</p>
        {!reviewing
          ? <><button type="submit">{t("setting_review")}</button><button type="button" onClick={close}>{t("runtime_confirm_cancel")}</button></>
          : <div className="module-confirm" role="alertdialog" aria-label={t("setting_save_named", { name: label })}>
            <ul>
              {impact.now.length > 0 && <li>{t("setting_recreated_now", { names: impact.now.join(", ") })}</li>}
              {impact.later.length > 0 && <li>{t("setting_recreated_later", { names: impact.later.join(", ") })}</li>}
              {impact.sessions && <li>{t("setting_sessions_end")}</li>}
              {impact.reindex && <li>{t("setting_reindex")}</li>}
            </ul>
            <button type="button" disabled={busy} onClick={() => { onSave(draft.trim()); close(); }}>{t("setting_save")}</button>
            <button type="button" onClick={close}>{t("runtime_confirm_cancel")}</button>
          </div>}
      </form>}
  </li>;
}

function valueText(row: RuntimeSetting, view: View, t: ReturnType<typeof useTranslate>): string {
  if (row.value === "") {
    if (row.id === "discord.answer_channels") return t("setting_every_channel");
    if (row.default) return `${row.default} (${t("setting_default")})`;
    return t(row.kind === "text" ? "setting_default" : "setting_none");
  }
  if (row.id !== "discord.meme_channels") return row.value.split(",").join(", ");
  return row.value.split(",").map((channel) => `${channel} (${view.webhookChannels.includes(channel) ? "webhook" : "bot"})`).join(", ");
}
