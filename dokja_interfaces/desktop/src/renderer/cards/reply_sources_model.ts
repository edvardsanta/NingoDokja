// The documents a reply drew on. The orchestrator closes such a reply with one line, "📚 [1] Title ·
// [2] Title", and the archive keeps that line in the reply's text, so a reopened turn has nothing else
// to read it from. Talk takes the line out of the text and shows it as a list under the reply.

export type ReplySource = { number: number; title: string };

// A piece of a reply's text: plain, or a citation of documents that are listed under it.
export type ReplyPart = string | { cite: readonly number[]; text: string };

const LEAD = "📚 ";
// "[2]" opening an entry: at the start of the line or after " · ", and followed by its title.
const ENTRY = /(?:^| · )\[(\d+)\](?= |$)/g;
// A citation as the model writes it: [1], [1][2] and [1, 2].
const CITATION = /\[(\d+(?:\s*,\s*\d+)*)\]/g;
// A kept fact's line can follow the sources, so they can be in either of the last paragraphs.
const TAIL_PARAGRAPHS = 2;

// Documents are numbered in the order they were found, so a number that does not go up is part of a
// title that has " · [2] " in it. A line that does not read as sources at all gives none.
function entries(line: string): ReplySource[] {
  const found: { number: number; from: number; to: number }[] = [];
  for (const match of line.matchAll(ENTRY)) {
    const number = Number(match[1]);
    const last = found.at(-1);
    if ((last && number <= last.number) || (!last && match.index !== 0)) continue;
    found.push({ number, from: match.index, to: match.index + match[0].length });
  }
  if (found[0]?.from !== 0) return [];
  return found.map((entry, at) => ({
    number: entry.number,
    title: line.slice(entry.to, found[at + 1]?.from ?? line.length).replace(/\s+/g, " ").trim(),
  }));
}

// The reply without its sources line, and the sources it named. A reply without that line, or with
// one that cannot be read, comes back as it is.
export function splitSources(reply: string): { body: string; sources: ReplySource[] } {
  const paragraphs = reply.trimEnd().split("\n\n");
  for (let at = paragraphs.length - 1; at >= Math.max(0, paragraphs.length - TAIL_PARAGRAPHS); at--) {
    const line = paragraphs[at]!.trim();
    if (!line.startsWith(LEAD)) continue;
    const sources = entries(line.slice(LEAD.length));
    if (sources.length > 0) return { body: paragraphs.filter((_, each) => each !== at).join("\n\n"), sources };
  }
  return { body: reply, sources: [] };
}

// The reply's text with the citations of listed documents set apart; any other bracket stays text.
export function citedParts(body: string, sources: readonly ReplySource[]): ReplyPart[] {
  const listed = new Set(sources.map((source) => source.number));
  const parts: ReplyPart[] = [];
  let from = 0;
  for (const match of body.matchAll(CITATION)) {
    const cite = match[1]!.split(",").map((number) => Number(number.trim()));
    if (!cite.every((number) => listed.has(number))) continue;
    if (match.index > from) parts.push(body.slice(from, match.index));
    parts.push({ cite, text: match[0] });
    from = match.index + match[0].length;
  }
  if (from < body.length) parts.push(body.slice(from));
  return parts;
}
