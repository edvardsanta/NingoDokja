// A link code printed in a log (the WhatsApp interface draws one with block characters) cannot be
// scanned there: every line carries the log's prefix. Each run of such rows becomes one line that
// says where to link instead. Anything else in the log stays as it came.

// The log's own prefix, as Compose writes it: "[service] | 2026-10-25T12:16:16.918145966-03:00 ".
const PREFIX = /^(\[[^\]]*\]\s*\|\s*\S+\s?)?/;
// A drawn row: block characters and spaces only, and enough of them to be a code, not a divider.
const DRAWN = /^[  █▀▄]+$/;
const MIN_BLOCKS = 10;
// Fewer rows than this is not a code.
const MIN_ROWS = 5;

function drawnRow(line: string): boolean {
  const content = line.replace(PREFIX, "").trimEnd();
  return DRAWN.test(content) && (content.match(/[█▀▄]/g)?.length ?? 0) >= MIN_BLOCKS;
}

export function withoutLinkCodes(text: string, note: string): string {
  const lines = text.split("\n");
  const kept: string[] = [];
  let start = 0;
  while (start < lines.length) {
    let end = start;
    while (end < lines.length && drawnRow(lines[end] ?? "")) end++;
    if (end - start >= MIN_ROWS) {
      kept.push(`${(lines[start] ?? "").match(PREFIX)?.[0] ?? ""}${note}`);
      start = end;
    } else {
      kept.push(...lines.slice(start, Math.max(end, start + 1)));
      start = Math.max(end, start + 1);
    }
  }
  return kept.join("\n");
}
