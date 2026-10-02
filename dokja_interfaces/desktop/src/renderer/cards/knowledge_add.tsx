import { useCallback, useEffect, useId, useState, type DragEvent, type KeyboardEvent } from "react";

import { INGEST_LIMITS, INGEST_MODES, type IngestMode } from "../../shared/ingest.js";
import type { IngestResult, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { OffNotice, Problem as ErrorLine } from "./card_frame.js";
import {
  FILE_MEGABYTES,
  check,
  outcomeOf,
  parseIngestResult,
  readFile,
  type Draft,
  type FileDraft,
  type Problem,
} from "./knowledge_add_model.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

// A big file can take a while to read, split and index, and the orchestrator answers one request
// at a time. If the wait runs out the document may still have been added.
const INGEST_TIMEOUT_MS = 120_000;

const MODE_IDS: Record<IngestMode, MessageId> = {
  note: "add_mode_note",
  address: "add_mode_address",
  file: "add_mode_file",
};

export async function addToBase(
  transport: Transport,
  payload: Record<string, unknown>,
): Promise<LoadResult<IngestResult | Off>> {
  const answer = await transport.request("knowledge.ingest", payload, { timeoutMs: INGEST_TIMEOUT_MS });
  if (!answer.ok) return answer;
  const result = parseIngestResult(answer.result);
  return result
    ? { ok: true, data: result }
    : { ok: false, error: { code: "unexpected", message: "unexpected ingest reply" } };
}

const EMPTY: Draft = { mode: "note", title: "", body: "", address: "", tags: "" };

// A note, an address or a file, added to the research base. The form is off while a request is on
// its way and never sends again by itself.
export function AddToBase({ transport, onAdded }: { transport: Transport; onAdded?: () => void }) {
  const t = useTranslate();
  const ids = useId();
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [problem, setProblem] = useState<Problem | undefined>(undefined);
  const run = useCallback((payload: Record<string, unknown>) => addToBase(transport, payload), [transport]);
  const { state, start } = useAction(run);
  const sending = state.phase === "loading";

  const set = <K extends keyof Draft>(field: K, value: Draft[K]) => setDraft((current) => ({ ...current, [field]: value }));

  // What was sent is cleared once it is in; what failed stays, so nothing typed is lost.
  useEffect(() => {
    if (state.phase !== "ready" || state.data.off) return;
    setDraft((current) => ({ ...current, title: "", body: "", address: "", tags: "", file: undefined }));
    if (state.data.created + state.data.updated > 0) onAdded?.();
    // onAdded is a new function on every render of the card; what matters is a new answer
  }, [state]);

  const choose = (file: File | undefined) => {
    if (!file) return;
    setProblem(undefined);
    readFile(file).then(
      (read: FileDraft) => set("file", read),
      (error: unknown) => {
        set("file", undefined);
        setProblem(
          error instanceof RangeError
            ? { id: "add_file_too_big", params: { size: FILE_MEGABYTES } }
            : { id: "add_file_unreadable" },
        );
      },
    );
  };

  const onDrop = (event: DragEvent<HTMLFormElement>) => {
    event.preventDefault();
    const file = event.dataTransfer.files[0];
    if (!file || sending) return;
    set("mode", "file");
    choose(file);
  };

  const submit = () => {
    if (sending) return;
    const checked = check(draft);
    if (!checked.ok) {
      setProblem(checked.problem);
      return;
    }
    setProblem(undefined);
    start(checked.payload);
  };

  // Enter in a text field sends the form; in the note's text box Enter is a new line, so Ctrl+Enter sends.
  const onNoteKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      submit();
    }
  };

  const field = (name: string) => `${ids}-${name}`;
  return (
    <section className="add" aria-labelledby={field("title")}>
      <h3 id={field("title")}>{t("add_title")}</h3>
      <div className="scope" role="group" aria-label={t("add_title")}>
        {INGEST_MODES.map((mode) => (
          <button
            key={mode}
            type="button"
            aria-pressed={draft.mode === mode}
            disabled={sending}
            onClick={() => {
              set("mode", mode);
              setProblem(undefined);
            }}
          >
            {t(MODE_IDS[mode])}
          </button>
        ))}
      </div>
      <form
        className="add-form"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
        onDragOver={(event) => event.preventDefault()}
        onDrop={onDrop}
      >
        <fieldset disabled={sending}>
          <label htmlFor={field("name")}>{draft.mode === "note" ? t("add_field_title") : t("add_field_title_optional")}</label>
          <input
            id={field("name")}
            value={draft.title}
            maxLength={INGEST_LIMITS.titleChars}
            autoComplete="off"
            onChange={(event) => set("title", event.target.value)}
          />

          {draft.mode === "note" && (
            <>
              <label htmlFor={field("text")}>{t("add_field_text")}</label>
              <textarea
                id={field("text")}
                rows={6}
                value={draft.body}
                maxLength={INGEST_LIMITS.noteChars}
                onChange={(event) => set("body", event.target.value)}
                onKeyDown={onNoteKey}
              />
            </>
          )}
          {draft.mode === "address" && (
            <>
              <label htmlFor={field("address")}>{t("add_field_address")}</label>
              <input
                id={field("address")}
                value={draft.address}
                maxLength={INGEST_LIMITS.addressChars}
                inputMode="url"
                autoComplete="off"
                placeholder="https://"
                onChange={(event) => set("address", event.target.value)}
              />
            </>
          )}
          {draft.mode === "file" && (
            <>
              <label htmlFor={field("file")}>{t("add_field_file")}</label>
              <input
                id={field("file")}
                type="file"
                accept=".txt,.md,.rst,.html,.htm,.docx,.epub,.pdf,.xml,.rss,.atom"
                onChange={(event) => choose(event.target.files?.[0])}
              />
              <p className="detail">
                {draft.file
                  ? t("add_file_chosen", { name: draft.file.filename, kb: Math.max(1, Math.round(draft.file.bytes / 1024)) })
                  : t("add_file_hint", { size: FILE_MEGABYTES })}
              </p>
            </>
          )}

          <label htmlFor={field("tags")}>{t("add_field_tags")}</label>
          <input
            id={field("tags")}
            value={draft.tags}
            autoComplete="off"
            onChange={(event) => set("tags", event.target.value)}
          />
          <div>
            <button type="submit">{t("add_submit")}</button>
          </div>
        </fieldset>
      </form>
      <div aria-live="polite">
        {sending && <p role="status">{t("add_sending")}</p>}
        {problem && <p role="alert">{t(problem.id, problem.params)}</p>}
        {state.phase === "error" &&
          (state.error.code === "timeout" ? (
            <p role="alert">{t("add_timeout")}</p>
          ) : (
            <ErrorLine error={state.error} />
          ))}
        {state.phase === "ready" && <Outcome result={state.data} />}
      </div>
    </section>
  );
}

function Outcome({ result }: { result: IngestResult | Off }) {
  const t = useTranslate();
  if (result.off) return <OffNotice reason={result.reason} />;
  const outcome = outcomeOf(result);
  return (
    <>
      <p className="note" data-tone="ok">
        {t(outcome.id, outcome.params)}
      </p>
      {result.degraded && (
        <p className="note" data-tone="unchecked">
          {t("add_degraded")}
          {result.reason && <span className="detail"> {result.reason}</span>}
        </p>
      )}
    </>
  );
}
