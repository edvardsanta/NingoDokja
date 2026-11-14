import { useCallback, useState } from "react";

import type { KnowledgeProposal, KnowledgeProposals } from "../../shared/replies.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, Problem } from "./card_frame.js";
import { Button } from "./ui.js";
import { useCard, type LoadResult } from "./use_card.js";
import { WorkspacePanel } from "./workspace_panel.js";

async function loadProposals(transport: Transport): Promise<LoadResult<KnowledgeProposals>> {
  const answer = await transport.request("knowledge.proposals.list", {});
  if (!answer.ok) return answer;
  const result = answer.result;
  if (!isRecord(result) || !Array.isArray(result.proposals)) {
    return { ok: false, error: { code: "unexpected", message: "unexpected proposals reply" } };
  }
  return { ok: true, data: result as KnowledgeProposals };
}

// The kinds and origins this screen knows by name; anything else shows its raw value.
const KIND_LABEL_IDS = {
  web_article: "proposals_kind_web_article",
  document: "proposals_kind_document",
  meme_hashtag: "proposals_kind_meme_hashtag",
} as const;
type KnownKind = keyof typeof KIND_LABEL_IDS;

function isKnownKind(kind: string): kind is KnownKind {
  return kind in KIND_LABEL_IDS;
}

const ORIGIN_LABEL_IDS = {
  owner: "proposals_origin_owner",
  group: "proposals_origin_group",
  api: "proposals_origin_api",
} as const;
type KnownOrigin = keyof typeof ORIGIN_LABEL_IDS;

function isKnownOrigin(origin: string): origin is KnownOrigin {
  return origin in ORIGIN_LABEL_IDS;
}

function textOf(value: unknown): string {
  return typeof value === "string" ? value : "";
}

// What was kept of a meme: the picture when it is small enough to have been kept, and the text read
// from it. A video is not copied: its link is the source message, which the owner can open.
function memeMedia(proposal: KnowledgeProposal): { image: string; text: string; link: boolean } {
  const media = isRecord(proposal.payload.media) ? proposal.payload.media : {};
  const encoded = textOf(media.image_b64);
  const contentType = textOf(media.content_type);
  return {
    image: encoded !== "" && /^image\/[a-z0-9.+-]+$/i.test(contentType) ? `data:${contentType};base64,${encoded}` : "",
    text: textOf(media.text),
    link: textOf(media.kind) === "link",
  };
}

function ProposalItem({ proposal, busy, edit, onEdit, textEdit, onTextEdit, onDecide }: {
  proposal: KnowledgeProposal;
  busy: boolean;
  edit: string;
  onEdit: (value: string) => void;
  textEdit: string;
  onTextEdit: (value: string) => void;
  onDecide: (proposal: KnowledgeProposal, action: "approve" | "reject") => void;
}) {
  const t = useTranslate();
  const kind = isKnownKind(proposal.kind) ? t(KIND_LABEL_IDS[proposal.kind]) : proposal.kind;
  const originLabel = (origin: string) => (isKnownOrigin(origin) ? t(ORIGIN_LABEL_IDS[origin]) : origin);
  const url = textOf(proposal.payload.url);
  const filename = textOf(proposal.payload.filename);
  const tag = textOf(proposal.payload.tag);
  const meme = proposal.kind === "meme_hashtag" ? memeMedia(proposal) : undefined;
  const who = proposal.sentBy !== "" && proposal.sentBy !== proposal.proposedBy
    ? t("proposals_proposed_by_two", { proposed: proposal.proposedBy, sent: proposal.sentBy })
    : t("proposals_proposed_by", { name: proposal.proposedBy });
  return (
    <section className="proposal" aria-label={kind}>
      <h3>{kind}</h3>
      <p className="proposal-content">
        {tag && <span className="proposal-tag">{tag.startsWith("#") ? tag : `#${tag}`}</span>}
        {tag && (url || filename) && " · "}
        {url && <span>{url}</span>}
        {filename && <span>{filename}</span>}
      </p>
      {meme?.image && <img className="proposal-preview" src={meme.image} alt={t("proposals_meme_preview")} />}
      {meme?.text && <p className="detail proposal-text">{t("proposals_meme_text", { text: meme.text })}</p>}
      {meme?.link && !meme.text && <p className="detail">{t("proposals_meme_link_note")}</p>}
      <p className="detail">
        {who}
        {proposal.source.channelId && ` ${t("proposals_channel", { channel: proposal.source.channelId })}`}
        {proposal.source.permalink && (
          <> · <a href={proposal.source.permalink}>{t("proposals_source_link")}</a></>
        )}
      </p>
      {proposal.current && (
        <p className="detail">
          {t("proposals_current", {
            title: proposal.current.title || "—",
            origin: originLabel(proposal.current.origin),
          })}
        </p>
      )}
      {tag !== "" && (
        <input
          className="proposal-edit"
          value={edit}
          placeholder={t("proposals_edit_placeholder")}
          aria-label={t("proposals_edit_placeholder")}
          onChange={(event) => onEdit(event.target.value)}
        />
      )}
      {meme && !meme.text && (
        <input
          className="proposal-edit"
          value={textEdit}
          placeholder={t("proposals_edit_text_placeholder")}
          aria-label={t("proposals_edit_text_placeholder")}
          onChange={(event) => onTextEdit(event.target.value)}
        />
      )}
      <div className="proposal-actions">
        <Button size="compact" disabled={busy} onClick={() => onDecide(proposal, "approve")}>{t("proposals_approve")}</Button>
        <Button variant="quiet" size="compact" disabled={busy} onClick={() => onDecide(proposal, "reject")}>{t("proposals_reject")}</Button>
      </div>
    </section>
  );
}

export function ProposalsPanel({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadProposals(transport), [transport]);
  const { state, reload } = useCard(load);
  const [busy, setBusy] = useState<string>("");
  const [note, setNote] = useState("");
  const [problem, setProblem] = useState<TransportError>();
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [textEdits, setTextEdits] = useState<Record<string, string>>({});

  const decide = async (proposal: KnowledgeProposal, action: "approve" | "reject") => {
    if (busy) return;
    setBusy(proposal.id);
    setNote("");
    setProblem(undefined);
    const tagEdit = edits[proposal.id]?.trim();
    const textEdit = textEdits[proposal.id]?.trim();
    const edit = { ...(tagEdit ? { tag: tagEdit } : {}), ...(textEdit ? { text: textEdit } : {}) };
    const answer = await transport.request(`knowledge.proposal.${action}`, {
      proposal_id: proposal.id,
      ...(action === "approve" && Object.keys(edit).length > 0 ? { edit } : {}),
    });
    setBusy("");
    if (!answer.ok) {
      if (answer.error.code !== "cancelled") setProblem(answer.error);
      return;
    }
    const result = answer.result;
    if (isRecord(result) && typeof result.reason === "string" && result.reason !== "") {
      setNote(t("proposals_refused", { reason: result.reason }));
    } else {
      setNote(action === "approve" ? t("proposals_approved") : t("proposals_rejected_note"));
    }
    reload();
  };

  return (
    <CardFrame title={t("proposals_title")} state={state} onReload={reload}>
      {(data) => <>
        <p className="detail kept-about">{t("proposals_about")}</p>
        {problem && <Problem error={problem} />}
        {note && <p className="detail" role="status">{note}</p>}
        {data.proposals.length === 0 && <p className="detail">{t("proposals_empty")}</p>}
        {data.proposals.map((proposal) => (
          <ProposalItem key={proposal.id} proposal={proposal} busy={busy === proposal.id}
            edit={edits[proposal.id] ?? ""}
            onEdit={(value) => setEdits((previous) => ({ ...previous, [proposal.id]: value }))}
            textEdit={textEdits[proposal.id] ?? ""}
            onTextEdit={(value) => setTextEdits((previous) => ({ ...previous, [proposal.id]: value }))}
            onDecide={(chosen, action) => void decide(chosen, action)} />
        ))}
      </>}
    </CardFrame>
  );
}

export function ProposalsCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  return <WorkspacePanel trail={[t("nav_proposals")]}><ProposalsPanel transport={transport} /></WorkspacePanel>;
}
