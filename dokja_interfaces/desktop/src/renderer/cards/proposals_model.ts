import type { KnowledgeProposal } from "../../shared/replies.js";

// viaLabelOf names how a proposal entered the queue: Ningo's web search, or a link
// someone sent. Anything unrecognized is not a via tag.
export function viaLabelOf(tags: unknown): string {
  if (!Array.isArray(tags)) return "";
  for (const raw of tags) {
    const tag = typeof raw === "string" ? raw.trim() : "";
    if (tag === "via:search" || tag === "via:link") return tag;
  }
  return "";
}

// baseName strips the verified account id a proposal appends to a person's name,
// so "Tiago <42>" and "Tiago" filter as one person.
export function baseName(value: unknown): string {
  return typeof value === "string" ? value.replace(/ <[^>]*>$/, "").trim() : "";
}

// proposalPersons lists who the visible proposals involve, owner and Ningo first,
// everyone else sorted, so the person filter can offer exactly who is there.
export function proposalPersons(proposals: KnowledgeProposal[]): string[] {
  const seen = new Set<string>();
  for (const proposal of proposals) {
    for (const value of [baseName(proposal.proposedBy), baseName(proposal.sentBy)]) {
      if (value) seen.add(value);
    }
  }
  const rest = [...seen]
    .filter((name) => name !== "owner" && name !== "Ningo")
    .sort((a, b) => a.localeCompare(b));
  return [
    ...(seen.has("owner") ? ["owner"] : []),
    ...(seen.has("Ningo") ? ["Ningo"] : []),
    ...rest,
  ];
}

// proposalMatchesPerson is true when the person proposed the item or it was sent
// for them: a search Ningo ran for Tiago matches both Ningo and Tiago.
export function proposalMatchesPerson(proposal: KnowledgeProposal, person: string): boolean {
  return baseName(proposal.proposedBy) === person || baseName(proposal.sentBy) === person;
}
