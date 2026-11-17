import { createServer } from "node:http";
import { basename } from "node:path";

import { env } from "../config.js";

const browserUserAgent =
  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/133.0 Safari/537.36";

export type DiscordDeliveryEmbed = {
  title?: string;
  description?: string;
  thumbnail_url?: string;
  color?: number;
  fields?: Array<{ name: string; value: string; inline?: boolean }>;
  footer?: string;
  // the page the title links to; kept only when it is an http(s) address
  url?: string;
};

export type DiscordDeliveryRequest = {
  channel_id: string;
  content?: string;
  attachment_url?: string;
  // Set by callers (the orchestrator, backed by the meme service) that already
  // downloaded the attachment themselves, for hosts this interface cannot reach
  // directly (Cloudflare and similar bot checks). attachment_url is still sent, but
  // only to derive a filename; it is not fetched again when this is set.
  attachment_data_b64?: string;
  attachment_content_type?: string;
  // Posts the message as a reply to this earlier channel message (a deferred
  // reading settling its promise). Only the bot-token path can reply; the
  // webhook path ignores it.
  reply_to_message_id?: string;
  // Rich content (a price notification's follow-up). Both the bot-token and the
  // webhook path render them; content stays required nowhere when embeds exist.
  embeds?: DiscordDeliveryEmbed[];
};

export type DiscordDeliveryResponse = {
  status: "ok" | "error";
  channel_id?: string;
  message_id?: string;
  // Present when Discord reports it (guild channels); DM channels have none.
  guild_id?: string;
  has_attachment?: boolean;
  message?: string;
};

type DeliveryHandlerDependencies = {
  apiBaseUrl: string;
  token: string;
  webhooks?: Record<string, string>;
  fetchImpl: typeof fetch;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

type DeliveryServerOptions = {
  token: string;
  port: number;
  // The state of the interface, answered on GET /status. Only the level, a state from a closed
  // list and a fixed sentence: never an id or an address.
  status?: () => { status: "ok" | "degraded" | "error"; state: string; detail: string };
  webhooks?: Record<string, string>;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
  fetchImpl?: typeof fetch;
};

export function startDiscordDeliveryServer(options: DeliveryServerOptions) {
  const apiBaseUrl = env("DOKJA_DISCORD_API_BASE_URL", "https://discord.com/api/v10").replace(/\/$/, "");
  const lifetime = new AbortController();
  const upstreamFetch = options.fetchImpl ?? fetch;
  const fetchImpl: typeof fetch = (input, init) => upstreamFetch(input, {
    ...init,
    signal: init?.signal ? AbortSignal.any([lifetime.signal, init.signal]) : lifetime.signal,
  });

  const server = createServer(async (req, res) => {
    if (req.method === "GET" && req.url === "/status" && options.status) {
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify(options.status()));
      return;
    }
    if (req.method !== "POST" || req.url !== "/deliver") {
      res.writeHead(404, { "content-type": "application/json" });
      res.end(JSON.stringify({ status: "error", message: "not found" }));
      return;
    }

    try {
      const { statusCode, payload } = await handleDiscordDeliveryRequest(
        parseDeliveryRequest(await readJSON(req)),
        {
          apiBaseUrl,
          token: options.token,
          webhooks: options.webhooks,
          fetchImpl,
          log: options.log,
          error: options.error,
        },
      );
      res.writeHead(statusCode, { "content-type": "application/json" });
      res.end(JSON.stringify(payload));
    } catch (error) {
      options.error?.(
        JSON.stringify({ delivery: "exception", error: error instanceof Error ? error.message : String(error) }),
      );
      res.writeHead(500, { "content-type": "application/json" });
      res.end(JSON.stringify({
        status: "error",
        message: error instanceof Error ? error.message : "delivery failed",
      } satisfies DiscordDeliveryResponse));
    }
  });

  server.once("close", () => lifetime.abort());

  server.listen(options.port, "0.0.0.0", () => {
    options.log?.(JSON.stringify({ delivery: "listening", port: options.port }));
  });

  return server;
}

export async function handleDiscordDeliveryRequest(
  body: DiscordDeliveryRequest,
  deps: DeliveryHandlerDependencies,
): Promise<{ statusCode: number; payload: DiscordDeliveryResponse }> {
  const channelId = body.channel_id;
  const content = body.content ?? "";
  const attachmentUrl = body.attachment_url ?? "";
  const attachmentDataB64 = body.attachment_data_b64 ?? "";
  const embeds = cleanEmbeds(body.embeds);
  if (!channelId || (!content && !attachmentUrl && embeds.length === 0)) {
    return {
      statusCode: 400,
      payload: {
        status: "error",
        message: "channel_id and either content, attachment_url or embeds are required",
      },
    };
  }

  const webhookUrl = deps.webhooks?.[channelId];
  const target = webhookUrl
    ? { url: `${webhookUrl}${webhookUrl.includes("?") ? "&" : "?"}wait=true`, headers: {} as Record<string, string> }
    : { url: `${deps.apiBaseUrl}/channels/${channelId}/messages`, headers: { Authorization: `Bot ${deps.token}` } };

  deps.log?.(JSON.stringify({
    delivery: "request",
    channelId,
    via: webhookUrl ? "webhook" : "bot",
    contentLength: content.length,
    hasAttachment: attachmentUrl.length > 0,
  }));

  const response = attachmentDataB64
    ? await sendMessageWithAttachmentBytes(
        deps.fetchImpl,
        target,
        content,
        attachmentDataB64,
        body.attachment_content_type ?? null,
        attachmentUrl,
        embeds,
      )
    : attachmentUrl
    ? await sendMessageWithAttachment(deps.fetchImpl, target, content, attachmentUrl, embeds)
    : await sendPlainMessage(deps.fetchImpl, target, content, body.reply_to_message_id, !webhookUrl, embeds);

  if (!response.ok) {
    const errorText = await response.text();
    // A reply reference the originator cannot resolve (a slash command's interaction
    // id, a deleted message) must not sink the delivery: post once without it.
    if (!attachmentUrl && !attachmentDataB64 && !webhookUrl
      && response.status === 400 && errorText.includes("MESSAGE_REFERENCE_UNKNOWN_MESSAGE")) {
      deps.log?.(JSON.stringify({ delivery: "reply_reference_unknown", channelId }));
      return handleDiscordDeliveryRequest({ ...body, reply_to_message_id: undefined }, deps);
    }
    deps.error?.(
      JSON.stringify({ delivery: "error", channelId, status: response.status, errorText }),
    );
    return {
      statusCode: 502,
      payload: {
        status: "error",
        channel_id: channelId,
        has_attachment: attachmentUrl.length > 0,
        message: `discord returned ${response.status}`,
      },
    };
  }

  const message = await readDiscordMessage(response);
  deps.log?.(JSON.stringify({
    delivery: "sent",
    channelId,
    messageId: message.id ?? "",
    hasAttachment: attachmentUrl.length > 0,
  }));
  return {
    statusCode: 200,
    payload: {
      status: "ok",
      channel_id: channelId,
      message_id: message.id ?? "",
      ...(message.guild_id ? { guild_id: message.guild_id } : {}),
      has_attachment: attachmentUrl.length > 0,
    },
  };
}

// sendPlainMessage posts text, referencing an earlier channel message on the
// bot-token path when the caller asked for it (webhooks cannot reply).
async function sendPlainMessage(
  fetchImpl: typeof fetch,
  target: { url: string; headers: Record<string, string> },
  content: string,
  replyToMessageID: string | undefined,
  canReply: boolean,
  embeds: DiscordDeliveryEmbed[],
) {
  return fetchImpl(target.url, {
    method: "POST",
    headers: { ...target.headers, "Content-Type": "application/json" },
    body: JSON.stringify({
      content,
      ...(embeds.length > 0 ? { embeds } : {}),
      ...(replyToMessageID && canReply ? { message_reference: { message_id: replyToMessageID } } : {}),
    }),
  });
}

// cleanEmbeds keeps a bounded, Discord-valid subset; anything else is dropped so a
// bad embed never sinks an otherwise good delivery.
export function cleanEmbeds(raw: unknown): DiscordDeliveryEmbed[] {
  if (!Array.isArray(raw)) return [];
  const embeds: DiscordDeliveryEmbed[] = [];
  for (const entry of raw.slice(0, 4)) {
    if (!entry || typeof entry !== "object") continue;
    const value = entry as Record<string, unknown>;
    const embed: DiscordDeliveryEmbed = {};
    if (typeof value.title === "string" && value.title.trim()) embed.title = value.title.trim().slice(0, 256);
    if (typeof value.description === "string" && value.description.trim()) embed.description = value.description.trim().slice(0, 4096);
    if (typeof value.footer === "string" && value.footer.trim()) embed.footer = value.footer.trim().slice(0, 2048);
    if (typeof value.thumbnail_url === "string" && /^https:\/\//.test(value.thumbnail_url)) embed.thumbnail_url = value.thumbnail_url.slice(0, 2048);
    if (typeof value.url === "string" && /^https:\/\//.test(value.url)) embed.url = value.url.slice(0, 2048);
    if (typeof value.color === "number" && Number.isInteger(value.color) && value.color >= 0 && value.color <= 0xffffff) embed.color = value.color;
    if (Array.isArray(value.fields)) {
      const fields: Array<{ name: string; value: string; inline?: boolean }> = [];
      for (const rawField of value.fields.slice(0, 25)) {
        if (!rawField || typeof rawField !== "object") continue;
        const field = rawField as Record<string, unknown>;
        if (typeof field.name !== "string" || typeof field.value !== "string") continue;
        fields.push({
          name: field.name.trim().slice(0, 256),
          value: field.value.trim().slice(0, 1024),
          ...(field.inline === true ? { inline: true } : {}),
        });
      }
      if (fields.length > 0) embed.fields = fields;
    }
    // normalize to Discord's shape: footer carries {text}; thumbnail carries {url}
    const { footer, thumbnail_url, ...rest } = embed;
    const discordEmbed: Record<string, unknown> = { ...rest };
    if (footer) discordEmbed.footer = { text: footer };
    if (thumbnail_url) discordEmbed.thumbnail = { url: thumbnail_url };
    if (Object.keys(discordEmbed).length > 0) embeds.push(discordEmbed as unknown as DiscordDeliveryEmbed);
  }
  return embeds;
}

async function sendMessageWithAttachment(
  fetchImpl: typeof fetch,
  target: { url: string; headers: Record<string, string> },
  content: string,
  attachmentUrl: string,
  embeds: DiscordDeliveryEmbed[],
) {
  const attachmentResponse = await fetchImpl(attachmentUrl, {
    headers: attachmentRequestHeaders(attachmentUrl),
  });
  if (!attachmentResponse.ok) {
    throw new Error(`attachment fetch failed (${attachmentResponse.status})`);
  }

  const bytes = await attachmentResponse.arrayBuffer();
  return postAttachment(fetchImpl, target, content, bytes, attachmentResponse.headers.get("content-type"), attachmentUrl, embeds);
}

// sendMessageWithAttachmentBytes attaches bytes the caller already downloaded (the meme
// service, for hosts a plain HTTP client cannot reach directly, such as Cloudflare and
// similar bot checks), instead of fetching attachmentUrl again. attachmentUrl is kept
// only to derive a filename.
async function sendMessageWithAttachmentBytes(
  fetchImpl: typeof fetch,
  target: { url: string; headers: Record<string, string> },
  content: string,
  dataB64: string,
  contentType: string | null,
  attachmentUrl: string,
  embeds: DiscordDeliveryEmbed[],
) {
  const bytes = Buffer.from(dataB64, "base64");
  return postAttachment(fetchImpl, target, content, bytes, contentType, attachmentUrl, embeds);
}

async function postAttachment(
  fetchImpl: typeof fetch,
  target: { url: string; headers: Record<string, string> },
  content: string,
  bytes: ArrayBuffer | Buffer,
  contentType: string | null,
  attachmentUrl: string,
  embeds: DiscordDeliveryEmbed[],
) {
  const filename = buildAttachmentFilename(attachmentUrl, contentType);

  const form = new FormData();
  form.append("payload_json", JSON.stringify({
    content,
    ...(embeds.length > 0 ? { embeds } : {}),
    attachments: [{ id: 0, filename }],
  }));
  form.append(
    "files[0]",
    new Blob([bytes], { type: contentType ?? "application/octet-stream" }),
    filename,
  );

  return fetchImpl(target.url, {
    method: "POST",
    headers: target.headers,
    body: form,
  });
}

function attachmentRequestHeaders(attachmentUrl: string): Record<string, string> {
  const headers: Record<string, string> = { "User-Agent": browserUserAgent };
  try {
    headers.Referer = `${new URL(attachmentUrl).origin}/`;
  } catch {
    // Validation and the fetch call will report malformed URLs to the caller.
  }
  return headers;
}

function parseDeliveryRequest(body: Record<string, unknown>): DiscordDeliveryRequest {
  return {
    channel_id: String(body.channel_id ?? "").trim(),
    content: String(body.content ?? "").trim() || undefined,
    attachment_url: String(body.attachment_url ?? "").trim() || undefined,
    attachment_data_b64: String(body.attachment_data_b64 ?? "").trim() || undefined,
    attachment_content_type: String(body.attachment_content_type ?? "").trim() || undefined,
    reply_to_message_id: String(body.reply_to_message_id ?? "").trim() || undefined,
    embeds: Array.isArray(body.embeds) ? (body.embeds as DiscordDeliveryEmbed[]) : undefined,
  };
}

async function readDiscordMessage(response: Response): Promise<{ id?: string; guild_id?: string }> {
  const raw = await response.text();
  if (!raw) {
    return {};
  }
  try {
    return JSON.parse(raw) as { id?: string; guild_id?: string };
  } catch {
    return {};
  }
}

function buildAttachmentFilename(attachmentUrl: string, contentType: string | null) {
  const cleanPath = attachmentUrl.split("?")[0] ?? "";
  const fromPath = basename(cleanPath);
  if (fromPath && fromPath !== "/" && fromPath !== ".") {
    return fromPath;
  }

  switch (contentType) {
    case "image/jpeg":
      return "meme.jpg";
    case "image/png":
      return "meme.png";
    case "image/gif":
      return "meme.gif";
    case "image/webp":
      return "meme.webp";
    default:
      return "meme.bin";
  }
}

function readJSON(req: Parameters<typeof createServer>[0]): Promise<Record<string, unknown>> {
  return new Promise((resolve, reject) => {
    let data = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => {
      data += chunk;
    });
    req.on("end", () => {
      try {
        resolve(data ? JSON.parse(data) as Record<string, unknown> : {});
      } catch (error) {
        reject(error);
      }
    });
    req.on("error", reject);
  });
}
