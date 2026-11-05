import type { DiscordDocumentAttachment } from "../types.js";

// Forward metadata only. The orchestrator authorizes the turn before Feeds fetches bytes.
export function pdfAttachments(values: Iterable<unknown>): DiscordDocumentAttachment[] {
  const documents: DiscordDocumentAttachment[] = [];
  for (const value of values) {
    if (!value || typeof value !== "object") continue;
    const attachment = value as Record<string, unknown>;
    const name = attachment.name ?? attachment.filename;
    const contentType = attachment.contentType ?? attachment.content_type ?? "";
    if (typeof name !== "string" || typeof contentType !== "string"
      || (!name.toLowerCase().endsWith(".pdf") && contentType.toLowerCase() !== "application/pdf")
      || typeof attachment.url !== "string" || attachment.url.length > 2048
      || typeof attachment.size !== "number" || !Number.isSafeInteger(attachment.size)) continue;
    documents.push({ url: attachment.url, filename: name.slice(0, 200), content_type: contentType.slice(0, 100), size: attachment.size });
    if (documents.length === 10) break;
  }
  return documents;
}
