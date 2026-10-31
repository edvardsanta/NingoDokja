import { useTranslate } from "../i18n/context.js";
import { citedParts, splitSources } from "./reply_sources_model.js";

// What Ningo said, and under it the documents the reply drew on. Titles come from documents, so they
// only ever reach the screen as text. The citations in the text are marks in the face of the numbers of
// that list, and name the documents on hover.
export function ReplyBody({ reply }: { reply: string }) {
  const t = useTranslate();
  const { body, sources } = splitSources(reply);
  if (sources.length === 0) return <p>{reply}</p>;
  const titleOf = (number: number) => sources.find((source) => source.number === number)?.title ?? "";
  return <>
    {body && <p>{citedParts(body, sources).map((part, at) => typeof part === "string"
      ? part
      : <span key={at} className="cite" title={part.cite.map(titleOf).filter(Boolean).join("; ") || undefined}>{part.text}</span>)}</p>}
    <ul className="reply-sources" aria-label={t("assistant_sources_label")}>
      {sources.map((source) => <li key={source.number} title={source.title || undefined}>
        <span className="cite-n">[{source.number}]</span>
        <span className="reply-source-title">{source.title || t("assistant_source_untitled")}</span>
      </li>)}
    </ul>
  </>;
}
