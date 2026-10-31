import { useCallback, useEffect, useId, useRef, useState, type DragEvent, type KeyboardEvent } from "react";

import { INGEST_LIMITS, type IngestMode } from "../../shared/ingest.js";
import type { IngestResult, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { OffNotice, Problem as ErrorLine } from "./card_frame.js";
import {
  ADD_MODES,
  DEFAULT_KINDS,
  KIND_SUGGESTIONS,
  addToBase,
  check,
  outcomeOf,
  readFile,
  type AddMode,
  type Draft,
  type FileDraft,
  type Problem,
} from "./knowledge_add_model.js";
import { PostPane } from "./knowledge_post.js";
import { Button } from "./ui.js";
import { useAction } from "./use_action.js";

const MODE_IDS: Record<AddMode, MessageId> = {
  note: "add_mode_note",
  address: "add_mode_address",
  file: "add_mode_file",
  post: "add_mode_post",
};

// What is wrong with a field put away under a disclosure opens it, so the owner sees the field the
// sentence is about.
const DETAILS_PROBLEMS: ReadonlySet<MessageId> = new Set(["add_too_many_tags", "add_tag_too_long", "add_bad_kind", "add_ref_too_long"]);
const ADVANCED_PROBLEMS: ReadonlySet<MessageId> = new Set(["add_bad_id"]);

const EMPTY: Draft = { mode: "note", title: "", body: "", address: "", tags: "", kind: "", reference: "", id: "" };

// A note, an address, a file or a post link, added to the research base. The form is off while a
// request is on its way and never sends again by itself.
// A file dropped on the view around the form, and how many have been: a second drop of the same file
// is still a new drop.
export type Dropped = { file: File; count: number };

// titled is false where something around the form already names it, such as the Add drawer.
//
// startMode and onMode let the view around the form keep the way of adding that was last used, since
// the drawer is made again each time it opens.
type Props = {
  transport: Transport;
  onAdded?: () => void;
  dropped?: Dropped;
  titled?: boolean;
  startMode?: AddMode;
  onMode?: (mode: AddMode) => void;
};

export function AddToBase({ transport, onAdded, dropped, titled = true, startMode = "note", onMode }: Props) {
  const t = useTranslate();
  const ids = useId();
  const [mode, setMode] = useState<AddMode>(startMode);
  const [draft, setDraft] = useState<Draft>({ ...EMPTY, mode: startMode === "post" ? EMPTY.mode : startMode });
  const [problem, setProblem] = useState<Problem | undefined>(undefined);
  const [reading, setReading] = useState(false);
  // The kind, the source and the tags (details) and the id (advanced) are put away: Add comes first.
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const run = useCallback((payload: Record<string, unknown>) => addToBase(transport, payload), [transport]);
  const { state, start } = useAction(run);
  const sending = state.phase === "loading";
  // a post being read or saved holds the way of adding, as a form being sent does
  const busy = sending || reading;

  const set = <K extends keyof Draft>(field: K, value: Draft[K]) => setDraft((current) => ({ ...current, [field]: value }));

  // What was sent is cleared once it is in; what failed stays, so nothing typed is lost.
  useEffect(() => {
    if (state.phase !== "ready" || state.data.off) return;
    // The kind stays: documents of one kind tend to come together.
    setDraft((current) => ({ ...current, title: "", body: "", address: "", tags: "", reference: "", id: "", file: undefined }));
    if (state.data.created + state.data.updated > 0) onAdded?.();
    // onAdded is a new function on every render of the card; what matters is a new answer
  }, [state]);

  const pick = (next: AddMode) => {
    setMode(next);
    onMode?.(next);
    setProblem(undefined);
    if (next !== "post") set("mode", next);
  };

  const choose = (file: File | undefined) => {
    if (!file) return;
    setProblem(undefined);
    readFile(file).then(
      (read: FileDraft) => set("file", read),
      (error: unknown) => {
        set("file", undefined);
        setProblem(
          error instanceof RangeError
            ? { id: "add_file_too_big" }
            : { id: "add_file_unreadable" },
        );
      },
    );
  };

  // "Use as address": the link of a post nobody reads goes to the address field, and the focus with it.
  const address = useRef<HTMLInputElement>(null);
  const carried = useRef(false);
  useEffect(() => {
    if (!carried.current) return;
    carried.current = false;
    address.current?.focus();
  }, [mode]);
  const carryToAddress = (url: string) => {
    carried.current = true;
    set("address", url);
    pick("address");
  };

  const form = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (!dropped || busy) return;
    pick("file");
    choose(dropped.file);
    form.current?.scrollIntoView?.({ block: "nearest" });
    // a new drop is a new count; the rest is read at that moment
  }, [dropped?.count]);

  const onDrop = (event: DragEvent<HTMLFormElement>) => {
    event.preventDefault();
    const file = event.dataTransfer.files[0];
    if (!file || busy) return;
    pick("file");
    choose(file);
  };

  const submit = () => {
    if (sending) return;
    const checked = check(draft);
    if (!checked.ok) {
      setProblem(checked.problem);
      if (DETAILS_PROBLEMS.has(checked.problem.id)) setDetailsOpen(true);
      if (ADVANCED_PROBLEMS.has(checked.problem.id)) setAdvancedOpen(true);
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
    <section className="add" {...(titled ? { "aria-labelledby": field("title") } : { "aria-label": t("add_title") })}>
      {titled && <h3 id={field("title")}>{t("add_title")}</h3>}
      <div className="scope" role="group" aria-label={t("add_title")}>
        {ADD_MODES.map((each) => (
          <button key={each} type="button" aria-pressed={mode === each} disabled={busy} onClick={() => pick(each)}>
            {t(MODE_IDS[each])}
          </button>
        ))}
      </div>
      {mode === "post" && <PostPane transport={transport} onAdded={onAdded} onBusy={setReading} onUseAddress={carryToAddress} />}
      {mode !== "post" && <form
        ref={form}
        className="add-form"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
        onDragOver={(event) => event.preventDefault()}
        onDrop={onDrop}
      >
        <fieldset disabled={sending}>
          <label htmlFor={field("name")}>{mode === "note" ? t("add_field_title") : t("add_field_title_optional")}</label>
          <input
            id={field("name")}
            value={draft.title}
            maxLength={INGEST_LIMITS.titleChars}
            autoComplete="off"
            onChange={(event) => set("title", event.target.value)}
          />

          {mode === "note" && (
            <>
              <label htmlFor={field("text")}>{t("add_field_text")}</label>
              <textarea
                id={field("text")}
                rows={6}
                value={draft.body}
                onChange={(event) => set("body", event.target.value)}
                onKeyDown={onNoteKey}
              />
            </>
          )}
          {mode === "address" && (
            <>
              <label htmlFor={field("address")}>{t("add_field_address")}</label>
              <input
                id={field("address")}
                ref={address}
                value={draft.address}
                maxLength={INGEST_LIMITS.addressChars}
                inputMode="url"
                autoComplete="off"
                placeholder="https://"
                onChange={(event) => set("address", event.target.value)}
              />
            </>
          )}
          {mode === "file" && (
            <>
              <label htmlFor={field("file")}>{t("add_field_file")}</label>
              {/* the browser's own picker says its words in the browser's language: the app's button
                  stands for it, and the input stays, out of sight and of the tab order, to hold the file */}
              <input
                id={field("file")}
                ref={picker}
                type="file"
                className="sr-only"
                tabIndex={-1}
                aria-hidden="true"
                accept=".txt,.md,.rst,.html,.htm,.docx,.epub,.pdf,.xml,.rss,.atom"
                onChange={(event) => choose(event.target.files?.[0])}
              />
              <div className="file-pick">
                <Button onClick={() => picker.current?.click()}>{t("add_file_choose")}</Button>
                <p className="detail">
                  {draft.file
                    ? t("add_file_chosen", { name: draft.file.filename, kb: Math.max(1, Math.round(draft.file.bytes / 1024)) })
                    : t("add_file_hint")}
                </p>
              </div>
            </>
          )}

          <div>
            <button type="submit">{t("add_submit")}</button>
          </div>

          <details className="more" open={detailsOpen} onToggle={(event) => setDetailsOpen(event.currentTarget.open)}>
            <summary>{t("add_details")}</summary>
            <label htmlFor={field("kind")}>{t("add_field_kind")}</label>
            <input
              id={field("kind")}
              list={field("kinds")}
              value={draft.kind}
              maxLength={INGEST_LIMITS.kindChars}
              placeholder={DEFAULT_KINDS[draft.mode]}
              autoComplete="off"
              onChange={(event) => set("kind", event.target.value)}
            />
            <datalist id={field("kinds")}>
              {KIND_SUGGESTIONS.map((kind) => (
                <option key={kind} value={kind} />
              ))}
            </datalist>

            <label htmlFor={field("reference")}>{t("add_field_reference")}</label>
            <input
              id={field("reference")}
              value={draft.reference}
              maxLength={INGEST_LIMITS.referenceChars}
              placeholder={t("add_reference_placeholder")}
              autoComplete="off"
              onChange={(event) => set("reference", event.target.value)}
            />

            <label htmlFor={field("tags")}>{t("add_field_tags")}</label>
            <input
              id={field("tags")}
              value={draft.tags}
              autoComplete="off"
              onChange={(event) => set("tags", event.target.value)}
            />
          </details>

          <details className="more" open={advancedOpen} onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}>
            <summary>{t("add_more")}</summary>
            <label htmlFor={field("id")}>{t("add_field_id")}</label>
            <input
              id={field("id")}
              value={draft.id}
              maxLength={INGEST_LIMITS.idChars}
              autoComplete="off"
              onChange={(event) => set("id", event.target.value)}
            />
            <p className="detail">{t("add_id_hint")}</p>
          </details>
        </fieldset>
      </form>}
      {mode !== "post" && <div aria-live="polite">
        {sending && <p role="status">{t("add_sending")}</p>}
        {problem && <p role="alert">{t(problem.id, problem.params)}</p>}
        {state.phase === "error" &&
          (state.error.code === "timeout" ? (
            <p role="alert">{t("add_timeout")}</p>
          ) : (
            <ErrorLine error={state.error} />
          ))}
        {state.phase === "ready" && <Outcome result={state.data} />}
      </div>}
    </section>
  );
}

export function Outcome({ result }: { result: IngestResult | Off }) {
  const t = useTranslate();
  if (result.off) return <OffNotice reason={result.reason} />;
  const outcome = outcomeOf(result);
  return (
    <>
      <p className="note" data-tone="ok">
        {t(outcome.id, outcome.params)}
      </p>
      {(result.pendingEmbeddings ?? 0) > 0 && !result.degraded && (
        <p className="detail">{t("add_indexing", { count: result.pendingEmbeddings ?? 0 })}</p>
      )}
      {result.degraded && (
        <p className="note" data-tone="unchecked">
          {t("add_degraded")}
          {result.reason && <span className="detail"> {result.reason}</span>}
        </p>
      )}
    </>
  );
}
