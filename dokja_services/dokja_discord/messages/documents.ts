import type { DiscordDocumentAttachment, DiscordMessageEmbed } from "../types.js";

// Forward metadata only. The orchestrator authorizes the turn before Feeds fetches bytes.
// Plain-text files join PDFs: the web reader handles text/plain the same bounded way.
export function documentAttachments(values: Iterable<unknown>): DiscordDocumentAttachment[] {
  const documents: DiscordDocumentAttachment[] = [];
  for (const value of values) {
    if (!value || typeof value !== "object") continue;
    const attachment = value as Record<string, unknown>;
    const name = attachment.name ?? attachment.filename;
    const contentType = attachment.contentType ?? attachment.content_type ?? "";
    if (typeof name !== "string" || typeof contentType !== "string"
      || (!name.toLowerCase().endsWith(".pdf") && contentType.toLowerCase() !== "application/pdf"
        && !name.toLowerCase().endsWith(".txt") && contentType.toLowerCase() !== "text/plain")
      || typeof attachment.url !== "string" || attachment.url.length > 2048
      || typeof attachment.size !== "number" || !Number.isSafeInteger(attachment.size)) continue;
    documents.push({ url: attachment.url, filename: name.slice(0, 200), content_type: contentType.slice(0, 100), size: attachment.size });
    if (documents.length === 10) break;
  }
  return documents;
}

// The pictures and videos a message carries, as metadata: a meme channel's tag proposals read them,
// nothing else does. The orchestrator authorizes the message before anything is fetched.
export function mediaAttachments(values: Iterable<unknown>): DiscordDocumentAttachment[] {
  const media: DiscordDocumentAttachment[] = [];
  for (const value of values) {
    if (!value || typeof value !== "object") continue;
    const attachment = value as Record<string, unknown>;
    const name = attachment.name ?? attachment.filename;
    const contentType = attachment.contentType ?? attachment.content_type ?? "";
    if (typeof name !== "string" || typeof contentType !== "string"
      || !/^(image|video)\//i.test(contentType)
      || typeof attachment.url !== "string" || attachment.url.length > 2048
      || typeof attachment.size !== "number" || !Number.isSafeInteger(attachment.size)) continue;
    media.push({ url: attachment.url, filename: name.slice(0, 200), content_type: contentType.slice(0, 100).toLowerCase(), size: attachment.size });
    if (media.length === 5) break;
  }
  return media;
}

// The rich embeds a message carries (a friend's game status card, a link preview), as text
// metadata: group-chat context for the chat, nothing else reads them. The game name on a
// status card is how the room knows what someone is playing.
export function richEmbeds(values: Iterable<unknown>): DiscordMessageEmbed[] {
  const embeds: DiscordMessageEmbed[] = [];
  for (const value of values) {
    if (!value || typeof value !== "object") continue;
    const embed = value as Record<string, unknown>;
    const author = embed.author && typeof embed.author === "object"
      ? (embed.author as Record<string, unknown>).name : undefined;
    const entry: DiscordMessageEmbed = {
      author: typeof author === "string" ? author.slice(0, 100) : undefined,
      title: typeof embed.title === "string" ? embed.title.slice(0, 200) : undefined,
      description: typeof embed.description === "string" ? embed.description.slice(0, 300) : undefined,
    };
    if (!entry.author && !entry.title && !entry.description) continue;
    embeds.push(entry);
    if (embeds.length === 5) break;
  }
  return embeds;
}
