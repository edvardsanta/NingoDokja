// A meme the orchestrator posted itself carries up to three trailing parts after its title: the meme's
// own page, the channel it was posted in as a mention (`<#id>`) and the link to that message. The page
// and the mention are each optional; the message link is told apart by its Discord address. The title
// links to the page when there is one, else to the message, and the message keeps a link of its own.
const POSTED_PARTS = /^\[(\d+)\] (.+?)((?: <(?:https?:\/\/[^<>\s]+|#\d+)>)+)$/;
const MESSAGE_LINK = /^https:\/\/discord\.com\/channels\/[^/<>\s]*\/\d+\/\d+$/;

function postedMeme(entry: string): string | undefined {
  const match = POSTED_PARTS.exec(entry);
  if (!match) return undefined;
  const parts = [...match[3].matchAll(/ <([^<>\s]+)>/g)].map(part => part[1]);
  const message = parts.find(part => MESSAGE_LINK.test(part));
  if (!message) return undefined;
  const channel = parts.find(part => /^#\d+$/.test(part));
  const page = parts.find(part => part !== message && part !== channel);
  const title = Array.from(match[2].split(" · ")[0].trim()).slice(0, 100).join("")
    .replace(/[\\\[\]*_`~]/g, "\\$&").replace(/\|/g, "│").replace(/@/g, "＠");
  const link = (address: string) => address.replace(/\(/g, "%28").replace(/\)/g, "%29").replace(/\\/g, "%5C");
  const head = `[${match[1]}] [${title}](${link(page ?? message)})`;
  return [head, channel ? `<${channel}>` : "", page ? `[↗](${link(message)})` : ""].filter(Boolean).join(" ");
}

// Render only the trailing citation block produced by the orchestrator. Keep
// original numbers and exact URLs (including passage fragments) for traceability.
export function formatCitations(reply: string): string {
  const footer = /(^|\n\n)📚 ([^\n]+)$/.exec(reply);
  if (!footer) return reply;
  const entries = footer[2].split(/ · (?=\[\d+\] )/);
  const groups = new Map<string, { numbers: string[]; title: string; url: string }>();
  const lines: Array<string | { numbers: string[]; title: string; url: string }> = [];
  let postedCount = 0;
  for (const entry of entries) {
    const posted = postedMeme(entry);
    if (posted) { lines.push(posted); postedCount++; continue; }
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
    // Prefer the title over redundant headings, but keep the PDF page visible.
    // Reserve space for its suffix so long filenames cannot hide the locator.
    const pageTitle = label.split(" · ").map(part => part.trim()).find(Boolean) ?? label;
    const page = /^#page=([1-9][0-9]{0,4})$/.exec(new URL(address).hash)?.[1];
    const suffix = page ? ` · p. ${page}` : "";
    const titleLimit = 100 - suffix.length;
    const characters = Array.from(pageTitle);
    const title = (characters.length > titleLimit ? characters.slice(0, titleLimit - 1).join("").trimEnd() + "…" : pageTitle) + suffix;
    const group = { numbers: [number], title, url: address };
    groups.set(address, group);
    lines.push(group);
  }
  if (!groups.size && !postedCount) return reply;
  const rendered = lines.map(line => {
    if (typeof line === "string") return line;
    const title = line.title.replace(/[\\\[\]*_`~]/g, "\\$&").replace(/\|/g, "│").replace(/@/g, "＠");
    const url = line.url.replace(/\(/g, "%28").replace(/\)/g, "%29").replace(/\\/g, "%5C");
    return `[${line.numbers.join(", ")}] [${title}](${url})`;
  });
  return reply.slice(0, footer.index) + footer[1] + "📚\n" + rendered.join("\n");
}
