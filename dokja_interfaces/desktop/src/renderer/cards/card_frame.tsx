import { useId, type ReactNode } from "react";

import type { TransportError, TransportErrorCode } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId, Translate } from "../i18n/i18n.js";
import type { CardState } from "./use_card.js";
import { useWorkspaceRefresh } from "./workspace_panel.js";

const ERROR_IDS: Record<TransportErrorCode, MessageId> = {
  unavailable: "error_unavailable",
  timeout: "error_timeout",
  denied: "error_denied",
  invalid: "error_invalid",
  orchestrator: "error_orchestrator",
  unexpected: "error_unexpected",
  cancelled: "error_cancelled",
};

// Failures whose cause and fix are known are said plainly; the rest pass on the orchestrator's words.
export function describeError(error: TransportError, t: Translate): string {
  const known = knownFailure(error);
  if (known) return t(known.id, known.values);
  return t(ERROR_IDS[error.code], { message: error.message });
}

type Known = { id: MessageId; values?: Record<string, string> };

export function knownFailure(error: TransportError): Known | undefined {
  if (error.code !== "orchestrator") return undefined;
  // Feeds has no image reader: the media-perception plugin is not installed or not enabled.
  if (/no enabled provider of media\.perceive\.v1/.test(error.message)) return { id: "error_media_reader_missing" };
  // The image's host refused the download that went around the missing reader.
  const blocked = /attachment fetch failed \((\d{3})\)/.exec(error.message);
  if (blocked) return { id: "error_attachment_blocked", values: { status: blocked[1] ?? "" } };
  return undefined;
}

// Reading a meme's text failed in a way the owner can make up for by writing the text.
export function needsWrittenText(error: TransportError): boolean {
  return knownFailure(error)?.id === "error_media_reader_missing" || /provide explicit text/.test(error.message);
}

// The three states every card body can be in besides showing its data.
export function Pending() {
  const t = useTranslate();
  return <p role="status">{t("loading")}</p>;
}

export function Problem({ error }: { error: TransportError }) {
  const t = useTranslate();
  return <p role="alert" title={knownFailure(error) ? error.message : undefined}>{describeError(error, t)}</p>;
}

// The operator switched the service off: an answer, not a failure.
export function OffNotice({ reason }: { reason: string }) {
  const t = useTranslate();
  return (
    <p className="note" data-tone="off">
      {t("service_off")}
      {reason && <span className="detail"> ({reason})</span>}
    </p>
  );
}

type CardFrameProps<T> = {
  title: string;
  state: CardState<T>;
  onReload: () => void;
  children: (data: T) => ReactNode;
  supplement?: ReactNode;
};

// The anatomy every section of a workspace shares: its title, then its body or why there is none.
// It is not a card of its own: the workspace's panel frames it and its one refresh reads this
// section again too. When the read failed, the section offers to try again where the failure is.
export function CardFrame<T>({ title, state, onReload, children, supplement }: CardFrameProps<T>) {
  const t = useTranslate();
  const titleId = useId();
  useWorkspaceRefresh(onReload, state.phase === "loading");
  return (
    <section className="wp-section" aria-labelledby={titleId} aria-busy={state.phase === "loading"}>
      <h2 id={titleId}>{title}</h2>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          <button type="button" className="retry" onClick={onReload}>
            {t("retry")}
          </button>
        </>
      )}
      {state.phase === "ready" && children(state.data)}
      {supplement}
    </section>
  );
}
