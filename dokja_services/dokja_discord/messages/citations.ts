// Render only the trailing citation block produced by the orchestrator. Keep
// original numbers and exact URLs (including passage fragments) for traceability.
export function formatCitations(reply: string): string {
  const footer = /(^|\n\n)📚 ([^\n]+)$/.exec(reply);
  if (!footer) return reply;
  const entries = footer[2].split(/ · (?=\[\d+\] )/);
  const groups = new Map<string, { numbers: string[]; title: string; url: string }>();
  const lines: Array<string | { numbers: string[]; title: string; url: string }> = [];
  for (const entry of entries) {
    const match = /^\[(\d+)\] (.+?) <(https?:\/\/[^<>\s]+)>$/.exec(entry);
    if (!match) {
      lines.push(entry);
      continue;
    }
    const [, number, label, address] = match;
    try { new URL(address); } catch { lines.push(entry); continue; }
    const existing = groups.get(address);
    if (existing) {
      if (!existing.numbers.includes(number)) existing.numbers.push(number);
      continue;
    }
    // Prefer the page title over the often-redundant product heading. Keep the
    // complete destination and citation numbers even when shortening the label.
    const pageTitle = label.split(" · ").map(part => part.trim()).find(Boolean) ?? label;
    const characters = Array.from(pageTitle);
    const title = characters.length > 100 ? characters.slice(0, 99).join("").trimEnd() + "…" : pageTitle;
    const group = { numbers: [number], title, url: address };
    groups.set(address, group);
    lines.push(group);
  }
  if (!groups.size) return reply;
  const rendered = lines.map(line => {
    if (typeof line === "string") return line;
    const title = line.title.replace(/[\\\[\]*_`~]/g, "\\$&").replace(/\|/g, "│").replace(/@/g, "＠");
    const url = line.url.replace(/\(/g, "%28").replace(/\)/g, "%29").replace(/\\/g, "%5C");
    return `[${line.numbers.join(", ")}] [${title}](${url})`;
  });
  return reply.slice(0, footer.index) + footer[1] + "📚\n" + rendered.join("\n");
}
