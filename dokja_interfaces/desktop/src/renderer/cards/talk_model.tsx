import { useContext, useEffect, useId, useState } from "react";
import type { Transport, TransportError } from "../../shared/transport.js";
import type { ChatBinding, ProviderRow } from "../../shared/runtime.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { RuntimeEnabled } from "../runtime_context.js";
import { useOpenWorkspace } from "../sidebar.js";
import { Problem } from "./card_frame.js";

type Choice = { provider: string; model: string; label: string };

// The models Talk may switch to: the declared models of the enabled providers, as in System. Choosing
// one there does not test it, and neither does choosing it here.
function choicesFrom(providers: readonly ProviderRow[], current: ChatBinding | undefined): Choice[] {
  const choices = providers.filter((row) => row.enabled !== false).flatMap((row) =>
    (row.declaredModels ?? []).map((model) => ({ provider: row.id, model, label: `${row.name} / ${model}` })));
  if (current?.provider && !choices.some((each) => each.provider === current.provider && each.model === current.model)) {
    choices.unshift({ provider: current.provider, model: current.model, label: `${current.provider} / ${current.model}` });
  }
  return choices;
}

// Which model answers, chosen where the owner is talking. Choosing saves at once and takes effect
// from the next message; the conversation and its history stay as they are. Declaring models and
// providers stays in System.
export function TalkModel({ transport, busy }: { transport: Transport; busy: boolean }) {
  const t = useTranslate();
  const id = useId();
  const runtime = useContext(RuntimeEnabled);
  const open = useOpenWorkspace();
  const [current, setCurrent] = useState<ChatBinding>();
  const [providers, setProviders] = useState<ProviderRow[]>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<TransportError>();

  const readBinding = async () => {
    const chat = await transport.request("runtime.chat", {});
    if (chat.ok && isRecord(chat.result)) { setCurrent(chat.result as ChatBinding); return chat.result as ChatBinding; }
    if (!chat.ok) setError(chat.error);
    return undefined;
  };
  useEffect(() => {
    if (runtime) void readBinding();
  }, [runtime, transport]);

  // The inventory inspects the providers, so it is read only when the owner goes to choose.
  const [listing, setListing] = useState(false);
  const list = () => {
    if (listing || providers) return;
    setListing(true);
    void transport.request("runtime.providers", {}).then((answer) => {
      if (answer.ok && isRecord(answer.result) && Array.isArray(answer.result.providers)) setProviders(answer.result.providers as ProviderRow[]);
      else { setProviders([]); if (!answer.ok) setError(answer.error); }
      setListing(false);
    });
  };

  if (!runtime || !current) return null;
  const choices = choicesFrom(providers ?? [], current);
  if (providers && choices.length === 0) {
    return <p className="detail talk-model">{t("talk_model_none")} <button type="button" onClick={() => open("health")}>{t("talk_open_system")}</button></p>;
  }

  const choose = async (value: string) => {
    const [provider = "", model = ""] = value.split("|");
    if (!provider || !model || saving) return;
    setSaving(true); setError(undefined);
    // The revision is read again just before the change, so a choice made in System meanwhile is not lost.
    const fresh = await readBinding();
    const answer = fresh && await transport.request("runtime.chat.configure", { configuration_revision: fresh.configurationRevision, provider, model });
    if (answer?.ok && isRecord(answer.result)) setCurrent(answer.result as ChatBinding);
    else if (answer && !answer.ok) setError(answer.error);
    setSaving(false);
  };

  return <div className="talk-model">
    <label htmlFor={`${id}-model`}>{t("talk_model_label")}</label>
    <select id={`${id}-model`} value={current.provider ? `${current.provider}|${current.model}` : ""} disabled={busy || saving}
      aria-busy={listing ? "true" : undefined} onFocus={list} onPointerDown={list} onChange={(event) => void choose(event.target.value)}>
      {!current.provider && <option value="">{t("chat_provider_missing")}</option>}
      {choices.map((each) => <option key={`${each.provider}|${each.model}`} value={`${each.provider}|${each.model}`}>{each.label}</option>)}
    </select>
    {saving && <span role="status" className="detail">{t("talk_model_saving")}</span>}
    {error && <Problem error={error}/>}
  </div>;
}
