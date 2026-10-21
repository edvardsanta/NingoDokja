import { useCallback, useEffect, useId, useRef, useState } from "react";

import { isRecord } from "../../shared/records.js";
import type { ClassificationAxis, KnowledgeClassification, Off } from "../../shared/replies.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { OffNotice, Pending, Problem } from "./card_frame.js";
import { useCard, type LoadResult } from "./use_card.js";

function snapshot(value: unknown, id: string): KnowledgeClassification | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true && typeof value.reason === "string") return value as Off;
  if (value.sourceId !== id || typeof value.revision !== "string" || !Array.isArray(value.questions) || !isRecord(value.form) || !isRecord(value.contentKind)) return undefined;
  return value as KnowledgeClassification;
}

export function KnowledgeClassificationPanel({ transport, id }: { transport: Transport; id: string }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<KnowledgeClassification | Off>> => {
    const answer = await transport.request("knowledge.classify", { source_id: id });
    if (!answer.ok) return answer;
    const data = snapshot(answer.result, id);
    return data ? { ok: true, data } : { ok: false, error: { code: "unexpected", message: "unexpected classification reply" } };
  }, [transport, id]);
  const { state, reload } = useCard(load);
  return (
    <section className="classification-panel" aria-label={t("classification_title")}>
      <h4>{t("classification_title")}</h4>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && <><Problem error={state.error} /><button type="button" onClick={reload}>{t("retry")}</button></>}
      {state.phase === "ready" && (state.data.off ? <OffNotice reason={state.data.reason} /> :
        <ClassificationEditor key={state.data.revision} transport={transport} initial={state.data} reload={reload} />)}
    </section>
  );
}

function ClassificationEditor({ transport, initial, reload }: { transport: Transport; initial: KnowledgeClassification; reload: () => void }) {
  const t = useTranslate();
  const ids = useId();
  const originLabel = (origin: string) => {
    switch (origin) {
      case "user": return t("classification_origin_user");
      case "classifier": return t("classification_origin_classifier");
      case "source_default": return t("classification_origin_source");
      default: return t("classification_unresolved");
    }
  };
  const [current, setCurrent] = useState(initial);
  const [answers, setAnswers] = useState<Partial<Record<ClassificationAxis, string>>>({});
  const [sending, setSending] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<TransportError>();
  const alive = useRef(true);
  const inFlight = useRef(false);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);

  const submit = async () => {
    const selected = Object.fromEntries(Object.entries(answers).filter(([, value]) => value));
    if (inFlight.current || error || !Object.keys(selected).length) return;
    inFlight.current = true;
    setSending(true);
    setSaved(false);
    try {
      const answer = await transport.request("knowledge.classify", { source_id: current.sourceId, revision: current.revision, answers: selected });
      if (!alive.current) return;
      if (!answer.ok) { setError(answer.error); return; }
      const next = snapshot(answer.result, current.sourceId);
      if (!next || next.off) { setError({ code: "unexpected", message: "unexpected classification reply" }); return; }
      setCurrent(next);
      setAnswers({});
      setSaved(true);
    } catch {
      if (alive.current) setError({ code: "unavailable", message: "request failed" });
    } finally {
      inFlight.current = false;
      if (alive.current) setSending(false);
    }
  };
  return (
    <>
      <dl>
        {([["form", current.form], ["content_kind", current.contentKind]] as const).map(([field, choice]) => (
          <div key={field}>
            <dt>{t(field === "form" ? "classification_form" : "classification_kind")}</dt>
            <dd>{choice.value ?? t("classification_unresolved")}
              {choice.origin && <span className="detail"> · {originLabel(choice.origin)}{choice.classifierId ? ` (${choice.classifierId}@${choice.classifierVersion})` : ""}</span>}
            </dd>
          </div>
        ))}
      </dl>
      {saved && <p role="status">{t("classification_saved")}</p>}
      {!current.needsUserInput && <p>{t("classification_complete")}</p>}
      {current.needsUserInput && <form onSubmit={(event) => { event.preventDefault(); void submit(); }}>
        <p>{t("classification_explain")}</p>
        <fieldset disabled={sending || !!error}>
          {current.questions.map((question) => <div key={question.field}>
            <label htmlFor={`${ids}-${question.field}`}>{question.question}</label>
            <select id={`${ids}-${question.field}`} value={answers[question.field] ?? ""}
              onChange={(event) => setAnswers((old) => ({ ...old, [question.field]: event.target.value }))}>
              <option value="">{t("classification_choose")}</option>
              {question.allowedValues.map((value) => <option key={value} value={value}>{value}</option>)}
            </select>
          </div>)}
          <button type="submit" disabled={sending || !!error || !Object.values(answers).some(Boolean)}>{t(sending ? "classification_saving" : "classification_save")}</button>
        </fieldset>
      </form>}
      {error && <><Problem error={error} /><p>{t("classification_reload_explain")}</p></>}
      <button type="button" disabled={sending} onClick={reload}>{t("classification_reload")}</button>
    </>
  );
}
