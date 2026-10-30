import { useEffect, useId, useRef, useState, type RefObject } from "react";

import { INGEST_LIMITS } from "../../shared/ingest.js";
import { isRecord } from "../../shared/records.js";
import type { PostMedia, PostRefusal, PostReply, PostView } from "../../shared/posts.js";
import type { IngestResult, Off } from "../../shared/replies.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { OffNotice, Problem as ErrorLine } from "./card_frame.js";
import { addToBase, checkPostLink, parsePostReply, postOutcome, postTitle } from "./knowledge_add_model.js";
import type { LoadResult } from "./use_card.js";

// The orchestrator asks a reader, and the shell then downloads every file before it answers.
const READ_TIMEOUT_MS = 120_000;

export async function readPost(transport: Transport, url: string): Promise<LoadResult<PostReply | Off>> {
  const answer = await transport.request("digest.link.read", { url }, { timeoutMs: READ_TIMEOUT_MS });
  if (!answer.ok) return answer;
  const reply = parsePostReply(answer.result);
  return reply
    ? { ok: true, data: reply }
    : { ok: false, error: { code: "unexpected", message: "unexpected post reply" } };
}

// A link nobody can read, a post behind a sign-in, a private or removed post are expected: a plain
// notice. A reader that broke, or none installed, is a problem.
const REFUSED: Record<PostRefusal, { id: MessageId; tone: "off" | "problem" }> = {
  unsupported: { id: "post_refused_unsupported", tone: "off" },
  login_required: { id: "post_refused_login", tone: "off" },
  unavailable: { id: "post_refused_unavailable", tone: "off" },
  no_reader: { id: "post_refused_no_reader", tone: "problem" },
  failed: { id: "post_refused_failed", tone: "problem" },
};

const UNAVAILABLE: LoadResult<never> = { ok: false, error: { code: "unavailable", message: "request failed" } };

// What the field says under itself when no post is shown.
type Note =
  | { kind: "link"; id: MessageId }
  | { kind: "refused"; refused: PostRefusal }
  | { kind: "off"; reason: string }
  | { kind: "error"; error: TransportError };

// Why a save did not go through. The owner saying no is not one: it says nothing.
type Trouble = { kind: "off"; reason: string } | { kind: "timeout" } | { kind: "error"; error: TransportError };

type View =
  | { phase: "idle" }
  | { phase: "reading" }
  | {
      phase: "shown";
      post: PostView;
      saving: boolean;
      localSaving?: boolean;
      localFiles?: number;
      localFailed?: boolean;
      folderFailed?: boolean;
      trouble?: Trouble;
    }
  | { phase: "saved"; id: "post_saved" | "add_unchanged"; title: string; files: number; documentId: string; folderFailed: boolean };

type Props = {
  transport: Transport;
  onAdded?: () => void;
  // The drawer keeps its mode buttons off while a post is being read or saved.
  onBusy?: (busy: boolean) => void;
  onUseAddress?: (url: string) => void;
};

// A link to one public post: read it, preview it, then save it locally or add it to the Library.
// Reading and saving ask the shell, which downloads the files and asks where local copies belong. A
// pressed button is about to be disabled, so the focus moves to the field first; it would
// otherwise fall out of the drawer.
export function PostPane({ transport, onAdded, onBusy, onUseAddress }: Props) {
  const t = useTranslate();
  const { day } = useTimes();
  const ids = useId();
  const [link, setLink] = useState("");
  const [note, setNote] = useState<Note | undefined>(undefined);
  const [view, setView] = useState<View>({ phase: "idle" });
  const field = useRef<HTMLInputElement>(null);
  const save = useRef<HTMLButtonElement>(null);
  const saveLocal = useRef<HTMLButtonElement>(null);
  const focusNext = useRef<"field" | "save" | "local" | undefined>(undefined);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const reading = view.phase === "reading";
  const saving = view.phase === "shown" && (view.saving || view.localSaving === true);
  const busy = reading || saving;
  useEffect(() => {
    onBusy?.(busy);
    return () => onBusy?.(false);
    // onBusy is a new function on every render of the drawer; what matters is the state
  }, [busy]);

  useEffect(() => {
    const next = focusNext.current;
    focusNext.current = undefined;
    if (next) (next === "save" ? save : next === "local" ? saveLocal : field).current?.focus();
  }, [view]);

  const arrived = (answer: LoadResult<PostReply | Off>) => {
    const refuse = (next: Note) => {
      setNote(next);
      setView({ phase: "idle" });
    };
    if (!answer.ok) return refuse({ kind: "error", error: answer.error });
    if ("off" in answer.data) return refuse({ kind: "off", reason: answer.data.reason });
    if (answer.data.status === "refused") return refuse({ kind: "refused", refused: answer.data.refused });
    setView({ phase: "shown", post: answer.data.post, saving: false });
  };

  // The request goes out inside the press that asked for it, with nothing awaited first: the shell
  // only reads a link on a real press.
  const show = () => {
    if (busy) return;
    const checked = checkPostLink(link);
    field.current?.focus();
    if (!checked.ok) {
      setView({ phase: "idle" });
      setNote({ kind: "link", id: checked.problem });
      return;
    }
    setNote(undefined);
    setView({ phase: "reading" });
    const settle = (answer: LoadResult<PostReply | Off>) => {
      if (alive.current) arrived(answer);
    };
    readPost(transport, checked.url).then(settle, () => settle(UNAVAILABLE));
  };

  const kept = (post: PostView, answer: LoadResult<IngestResult | Off>) => {
    const back = (trouble?: Trouble) => {
      focusNext.current = "save";
      setView((current) => ({
        phase: "shown",
        post,
        saving: false,
        ...(current.phase === "shown" && current.localFiles !== undefined ? { localFiles: current.localFiles } : {}),
        ...(current.phase === "shown" && current.localFailed ? { localFailed: true } : {}),
        ...(current.phase === "shown" && current.folderFailed ? { folderFailed: true } : {}),
        ...(trouble ? { trouble } : {}),
      }));
    };
    if (!answer.ok) {
      const { error } = answer;
      // The owner said no to the shell's question: nothing happened, so nothing is said.
      if (error.code === "cancelled") return back();
      return back(error.code === "timeout" ? { kind: "timeout" } : { kind: "error", error });
    }
    if (answer.data.off) return back({ kind: "off", reason: answer.data.reason });
    focusNext.current = "field";
    setLink("");
    setNote(undefined);
    setView({
      phase: "saved",
      id: postOutcome(answer.data),
      title: postTitle(post),
      files: post.media.length,
      documentId: post.documentId,
      folderFailed: false,
    });
  };

  const keep = (post: PostView) => {
    if (busy) return;
    field.current?.focus();
    setView((current) => ({
      phase: "shown",
      post,
      saving: true,
      ...(current.phase === "shown" && current.localFiles !== undefined ? { localFiles: current.localFiles } : {}),
      ...(current.phase === "shown" && current.folderFailed ? { folderFailed: true } : {}),
    }));
    const settle = (answer: LoadResult<IngestResult | Off>) => {
      // The Library's list is worth reading again even if the drawer was closed meanwhile.
      if (answer.ok && !answer.data.off && answer.data.created + answer.data.updated > 0) onAdded?.();
      if (alive.current) kept(post, answer);
    };
    addToBase(transport, { mode: "post", post: post.documentId }).then(settle, () => settle(UNAVAILABLE));
  };

  const saveOnComputer = (post: PostView) => {
    if (busy || !transport.savePostLocally) return;
    field.current?.focus();
    setView((current) => ({
      phase: "shown",
      post,
      saving: false,
      localSaving: true,
      ...(current.phase === "shown" && current.localFiles !== undefined ? { localFiles: current.localFiles } : {}),
    }));
    const settle = (result: Awaited<ReturnType<NonNullable<Transport["savePostLocally"]>>>) => {
      if (!alive.current) return;
      if (!result.ok && result.error.code === "cancelled") {
        focusNext.current = "local";
        setView((current) => current.phase === "shown" ? { ...current, localSaving: false } : current);
        return;
      }
      const files = result.ok && isRecord(result.result) ? result.result.files : undefined;
      if (typeof files !== "number" || !Number.isInteger(files) || files < 0) {
        focusNext.current = "local";
        setView((current) => current.phase === "shown" ? { ...current, localSaving: false, localFailed: true } : current);
        return;
      }
      focusNext.current = "local";
      setView((current) => current.phase === "shown"
        ? { ...current, localSaving: false, localFiles: files, localFailed: false, folderFailed: false }
        : current);
    };
    transport.savePostLocally(post.documentId).then(settle, () => {
      if (!alive.current) return;
      focusNext.current = "local";
      setView((current) => current.phase === "shown" ? { ...current, localSaving: false, localFailed: true } : current);
    });
  };

  const openFolder = (documentId: string) => {
    if (!transport.openSavedPost) return;
    const settle = (opened: boolean) => {
      if (alive.current) setView((current) => current.phase === "saved"
        ? { ...current, folderFailed: !opened }
        : current.phase === "shown" && current.localFiles !== undefined
          ? { ...current, folderFailed: !opened }
          : current);
    };
    transport.openSavedPost(documentId).then((result) => settle(result.ok), () => settle(false));
  };

  // Whatever is shown was read from the link as it was; once the link changes it no longer is.
  const edit = (value: string) => {
    if (busy) return;
    setLink(value);
    setNote(undefined);
    setView({ phase: "idle" });
  };

  const hintId = `${ids}-hint`;
  const noteId = `${ids}-note`;
  const shown = view.phase === "shown";
  return (
    <div className="add-post">
      <form
        className="add-form"
        onSubmit={(event) => {
          event.preventDefault();
          show();
        }}
      >
        <fieldset>
          <label htmlFor={`${ids}-link`}>{t("post_field_link")}</label>
          <input
            id={`${ids}-link`}
            ref={field}
            value={link}
            readOnly={busy}
            maxLength={INGEST_LIMITS.addressChars}
            inputMode="url"
            autoComplete="off"
            placeholder="https://"
            aria-invalid={note?.kind === "link" || note?.kind === "refused" ? true : undefined}
            aria-describedby={note ? `${hintId} ${noteId}` : hintId}
            onChange={(event) => edit(event.target.value)}
          />
          <p id={hintId} className="detail">
            {t("post_hint")}
          </p>
          {note && (
            <div id={noteId}>
              <FieldNote note={note} />
            </div>
          )}
          <div className="add-post-actions">
            <button type="submit" className={shown ? undefined : "primary"} disabled={busy}>
              {t("post_show")}
            </button>
            {note?.kind === "refused" && note.refused === "unsupported" && (
              <button type="button" className="quiet" onClick={() => onUseAddress?.(link.trim())}>
                {t("post_use_address")}
              </button>
            )}
          </div>
        </fieldset>
      </form>
      {/* The one live region: what the pane is doing, never the post itself. */}
      <div role="status" aria-busy={busy}>
        {reading && <p>{t("post_reading")}</p>}
        {view.phase === "shown" && (
          <p className="detail">
            {view.saving
              ? t("post_saving")
              : view.localSaving
                ? t("post_saving_local")
              : t("post_shown", { title: postTitle(view.post), count: view.post.media.length })}
          </p>
        )}
        {view.phase === "saved" && (
          <>
            <p className="note" data-tone="ok">
              {t(view.id, { title: view.title })}
            </p>
            {view.id === "post_saved" && view.files > 0 && (
              <p className="note" data-tone="ok">
                {t("post_files_copied", { count: view.files })}
              </p>
            )}
          </>
        )}
      </div>
      {view.phase === "saved" && view.files > 0 && transport.openSavedPost && (
        <div className="add-post-actions">
          <button type="button" className="quiet" onClick={() => openFolder(view.documentId)}>
            {t("post_open_folder")}
          </button>
        </div>
      )}
      {view.phase === "saved" && view.folderFailed && (
        <p role="alert" className="note" data-tone="problem">
          {t("post_open_folder_failed")}
        </p>
      )}
      {view.phase === "shown" && view.localFiles !== undefined && (
        <>
          <p className="note" data-tone="ok">
            {t("post_local_saved", { count: view.localFiles })}
          </p>
          {transport.openSavedPost && (
            <div className="add-post-actions">
              <button type="button" className="quiet" disabled={busy} onClick={() => openFolder(view.post.documentId)}>
                {t("post_open_folder")}
              </button>
            </div>
          )}
          {view.folderFailed && (
            <p role="alert" className="note" data-tone="problem">{t("post_open_folder_failed")}</p>
          )}
        </>
      )}
      {view.phase === "shown" && view.localFailed && (
        <p role="alert" className="note" data-tone="problem">{t("post_local_save_failed")}</p>
      )}
      {view.phase === "shown" && (
        <PostBlock
          post={view.post}
          saving={view.saving}
          localSaving={view.localSaving === true}
          trouble={view.trouble}
          saveRef={save}
          localSaveRef={saveLocal}
          canSaveLocally={Boolean(transport.savePostLocally)}
          when={view.post.published ? day(view.post.published) : ""}
          onSave={() => keep(view.post)}
          onSaveLocal={() => saveOnComputer(view.post)}
        />
      )}
    </div>
  );
}

function FieldNote({ note }: { note: Note }) {
  const t = useTranslate();
  switch (note.kind) {
    case "link":
      return (
        <p role="alert" className="note" data-tone="problem">
          {t(note.id)}
        </p>
      );
    case "refused":
      return (
        <p role="alert" className="note" data-tone={REFUSED[note.refused].tone}>
          {t(REFUSED[note.refused].id)}
        </p>
      );
    case "off":
      return <OffNotice reason={note.reason} />;
    case "error":
      return <ErrorLine error={note.error} />;
  }
}

function SaveTrouble({ trouble }: { trouble: Trouble }) {
  const t = useTranslate();
  switch (trouble.kind) {
    case "off":
      return <OffNotice reason={trouble.reason} />;
    case "timeout":
      return (
        <p role="alert" className="note" data-tone="unchecked">
          {t("add_timeout")}
        </p>
      );
    case "error":
      return (
        <>
          <p role="alert" className="note" data-tone="problem">
            {t("post_save_failed")}
          </p>
          <ErrorLine error={trouble.error} />
        </>
      );
  }
}

// The post preview: who, when, what it says, then the files. Save actions come before the files so
// the keyboard reaches them first.
function PostBlock({
  post,
  saving,
  localSaving,
  canSaveLocally,
  trouble,
  saveRef,
  localSaveRef,
  when,
  onSave,
  onSaveLocal,
}: {
  post: PostView;
  saving: boolean;
  localSaving: boolean;
  canSaveLocally: boolean;
  trouble: Trouble | undefined;
  saveRef: RefObject<HTMLButtonElement | null>;
  localSaveRef: RefObject<HTMLButtonElement | null>;
  when: string;
  onSave: () => void;
  onSaveLocal: () => void;
}) {
  const t = useTranslate();
  const title = postTitle(post);
  const meta = [when, post.media.length > 0 ? t("post_files", { count: post.media.length }) : ""]
    .filter((part) => part !== "")
    .join(", ");
  return (
    <article className="add-post-view">
      <h4 className="add-post-title">{title}</h4>
      {meta && <p className="detail">{meta}</p>}
      {post.caption.trim() === "" ? (
        <p className="detail">{t("post_no_caption")}</p>
      ) : (
        <div className="reading-preview-text add-post-caption" tabIndex={0} aria-label={t("post_caption_label")}>
          {post.caption}
        </div>
      )}
      {post.missing > 0 && (
        <p className="note" data-tone="unchecked">
          {t("post_media_missing", { count: post.missing })}
        </p>
      )}
      <div className="add-post-actions" aria-busy={saving || localSaving}>
        {canSaveLocally && (
          <button
            type="button"
            ref={localSaveRef}
            className="primary"
            aria-label={t("post_save_local_named", { title })}
            aria-busy={localSaving}
            disabled={saving || localSaving}
            onClick={onSaveLocal}
          >
            {localSaving ? t("post_saving_local") : t("post_save_local")}
          </button>
        )}
        <button
          type="button"
          ref={saveRef}
          className="quiet"
          aria-label={t("post_add_library_named", { title })}
          aria-busy={saving}
          disabled={saving || localSaving}
          onClick={onSave}
        >
          {t("post_add_library")}
        </button>
      </div>
      {trouble && <SaveTrouble trouble={trouble} />}
      {post.media.length > 0 && (
        <div className="add-post-media">
          {post.media.map((item, index) => (
            <PostFile key={`${index}-${item.src}`} item={item} index={index} total={post.media.length} />
          ))}
        </div>
      )}
    </article>
  );
}

// A picture or a clip, loaded at once, still: a clip never starts by itself. When it cannot be shown
// it says so where it was.
function PostFile({ item, index, total }: { item: PostMedia; index: number; total: number }) {
  const t = useTranslate();
  const [failed, setFailed] = useState(false);
  const label = item.kind === "video" ? t("post_video_n", { n: index + 1, total }) : t("post_image_n", { n: index + 1, total });
  if (failed) {
    return (
      <p className="note" data-tone="off">
        {label}. {t("post_media_error")}
      </p>
    );
  }
  return item.kind === "video" ? (
    <video src={item.src} aria-label={label} controls playsInline preload="metadata" onError={() => setFailed(true)} />
  ) : (
    <img src={item.src} alt={label} onError={() => setFailed(true)} />
  );
}
