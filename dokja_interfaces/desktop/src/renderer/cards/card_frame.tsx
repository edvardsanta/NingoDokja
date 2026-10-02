import { useId, type ReactNode } from "react";

import type { TransportError, TransportErrorCode } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId, Translate } from "../i18n/i18n.js";
import type { CardState } from "./use_card.js";

const ERROR_IDS: Record<TransportErrorCode, MessageId> = {
  unavailable: "error_unavailable",
  timeout: "error_timeout",
  denied: "error_denied",
  invalid: "error_invalid",
  orchestrator: "error_orchestrator",
  unexpected: "error_unexpected",
};

export function describeError(error: TransportError, t: Translate): string {
  return t(ERROR_IDS[error.code], { message: error.message });
}

// The three states every card body can be in besides showing its data.
export function Pending() {
  const t = useTranslate();
  return <p role="status">{t("loading")}</p>;
}

export function Problem({ error }: { error: TransportError }) {
  const t = useTranslate();
  return <p role="alert">{describeError(error, t)}</p>;
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
  kindLabel: string;
  title: string;
  state: CardState<T>;
  onReload: () => void;
  children: (data: T) => ReactNode;
};

// The anatomy every card shares: what kind it is, its title, then its body or why there is none.
export function CardFrame<T>({ kindLabel, title, state, onReload, children }: CardFrameProps<T>) {
  const t = useTranslate();
  const titleId = useId();
  return (
    <section className="card" aria-labelledby={titleId} aria-busy={state.phase === "loading"}>
      <span className="card-kind">{kindLabel}</span>
      <h2 id={titleId}>{title}</h2>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && <Problem error={state.error} />}
      {state.phase === "ready" && children(state.data)}
      <footer>
        <button type="button" onClick={onReload} disabled={state.phase === "loading"}>
          {state.phase === "error" ? t("retry") : t("refresh")}
        </button>
      </footer>
    </section>
  );
}
