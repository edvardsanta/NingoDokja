import { useEffect, useId, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

import { useTranslate } from "../i18n/context.js";
import { citedParts, detailedSources, originOf, splitSources, type ReplySource } from "./reply_sources_model.js";

const CARD_WIDTH = 340;
const CARD_MARGIN = 8;
// Room the card needs above what it describes before it opens below it instead.
const CARD_ROOM = 240;

type Peek = { source: ReplySource; box: DOMRect };

// What a citation points at, on hover or focus: the document, where it came from and the passage the
// reply drew on. It holds nothing to press, so it follows the pointer's target and never takes focus.
function SourceCard({ id, peek, openable }: { id: string; peek: Peek; openable: boolean }) {
  const t = useTranslate();
  const { source, box } = peek;
  const origin = originOf(source.reference);
  const left = Math.max(CARD_MARGIN, Math.min(box.left, window.innerWidth - CARD_WIDTH - CARD_MARGIN));
  const place = box.top > CARD_ROOM ? { bottom: window.innerHeight - box.top + 6 } : { top: box.bottom + 6 };
  return createPortal(<div id={id} role="tooltip" className="source-card" style={{ left, ...place }}>
    <strong>{source.title || t("assistant_source_untitled")}</strong>
    {origin && <small>{origin}</small>}
    {source.excerpt && <p>{source.excerpt}</p>}
    {openable && <small className="source-card-hint">{t("assistant_source_open")}</small>}
  </div>, document.body);
}

// A way into a source: its card on hover or focus and, when the archive kept which document it is,
// the document on a press. Without that it says what it can, as text.
function SourcePoint({ source, className, cardId, peeking, onPeek, onOpen, children }: {
  source: ReplySource | undefined;
  className: string;
  cardId: string;
  peeking: boolean;
  onPeek: (peek: Peek | undefined) => void;
  onOpen: ((source: ReplySource) => void) | undefined;
  children: ReactNode;
}) {
  const t = useTranslate();
  if (!source) return <span className={className}>{children}</span>;
  if (!source.id || !onOpen) return <span className={className} title={source.title || undefined}>{children}</span>;
  const peek = (element: HTMLElement) => onPeek({ source, box: element.getBoundingClientRect() });
  return <button type="button" className={className} aria-label={t("assistant_source_open_named", { title: source.title || t("assistant_source_untitled") })}
    aria-describedby={peeking ? cardId : undefined}
    onMouseEnter={(event) => peek(event.currentTarget)} onMouseLeave={() => onPeek(undefined)}
    onFocus={(event) => peek(event.currentTarget)} onBlur={() => onPeek(undefined)}
    onKeyDown={(event) => { if (event.key === "Escape") onPeek(undefined); }}
    onClick={() => { onPeek(undefined); onOpen(source); }}>{children}</button>;
}

// What Ningo said, and under it the documents the reply drew on. Titles and passages come from
// documents, so they only ever reach the screen as text.
export function ReplyBody({ reply, kept, onOpen }: { reply: string; kept?: readonly ReplySource[]; onOpen?: (source: ReplySource) => void }) {
  const t = useTranslate();
  const cardId = useId();
  const [peek, setPeek] = useState<Peek>();
  // The card is placed where the citation was, so it goes when the reply scrolls away from there.
  useEffect(() => {
    if (!peek) return undefined;
    const close = () => setPeek(undefined);
    window.addEventListener("scroll", close, true);
    return () => window.removeEventListener("scroll", close, true);
  }, [peek]);
  const { body, sources: written } = splitSources(reply);
  const sources = detailedSources(written, kept);
  if (sources.length === 0) return <p>{reply}</p>;
  const sourceOf = (number: number | undefined) => sources.find((source) => source.number === number);
  const point = { cardId, onPeek: setPeek, onOpen };
  return <>
    {body && <p>{citedParts(body, sources).map((part, at) => {
      if (typeof part === "string") return part;
      const source = sourceOf(part.cite[0]);
      return <SourcePoint key={at} {...point} source={source} className="cite" peeking={peek?.source === source}>{part.text}</SourcePoint>;
    })}</p>}
    <ul className="reply-sources" aria-label={t("assistant_sources_label")}>
      {sources.map((source) => <li key={source.number} title={source.title || undefined}>
        <SourcePoint {...point} source={source} className="reply-source" peeking={peek?.source === source}>
          <span className="cite-n">[{source.number}]</span>
          <span className="reply-source-title">{source.title || t("assistant_source_untitled")}</span>
        </SourcePoint>
      </li>)}
    </ul>
    {peek && <SourceCard id={cardId} peek={peek} openable={Boolean(onOpen && peek.source.id)} />}
  </>;
}
