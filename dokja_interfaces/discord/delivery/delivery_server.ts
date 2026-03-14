import { createServer } from "node:http";
import { basename } from "node:path";

import { env } from "../config.js";

export type DiscordDeliveryRequest = {
  channel_id: string;
  content?: string;
  attachment_url?: string;
};

export type DiscordDeliveryResponse = {
  status: "ok" | "error";
  channel_id?: string;
  message_id?: string;
  has_attachment?: boolean;
  message?: string;
};

type DeliveryHandlerDependencies = {
  apiBaseUrl: string;
  token: string;
  fetchImpl: typeof fetch;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

type DeliveryServerOptions = {
  token: string;
  port: number;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
  fetchImpl?: typeof fetch;
};

export function startDiscordDeliveryServer(options: DeliveryServerOptions) {
  const apiBaseUrl = env("DOKJA_DISCORD_API_BASE_URL", "https://discord.com/api/v10").replace(/\/$/, "");
  const fetchImpl = options.fetchImpl ?? fetch;

  const server = createServer(async (req, res) => {
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
  if (!channelId || (!content && !attachmentUrl)) {
    return {
      statusCode: 400,
      payload: {
        status: "error",
        message: "channel_id and either content or attachment_url are required",
      },
    };
  }

  deps.log?.(JSON.stringify({
    delivery: "request",
    channelId,
    contentLength: content.length,
    hasAttachment: attachmentUrl.length > 0,
  }));

  const response = attachmentUrl
    ? await sendMessageWithAttachment(deps.fetchImpl, deps.apiBaseUrl, deps.token, channelId, content, attachmentUrl)
    : await deps.fetchImpl(`${deps.apiBaseUrl}/channels/${channelId}/messages`, {
        method: "POST",
        headers: {
          Authorization: `Bot ${deps.token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ content }),
      });

  if (!response.ok) {
    const errorText = await response.text();
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
      has_attachment: attachmentUrl.length > 0,
    },
  };
}

async function sendMessageWithAttachment(
  fetchImpl: typeof fetch,
  apiBaseUrl: string,
  token: string,
  channelId: string,
  content: string,
  attachmentUrl: string,
) {
  const attachmentResponse = await fetchImpl(attachmentUrl);
  if (!attachmentResponse.ok) {
    throw new Error(`attachment fetch failed (${attachmentResponse.status})`);
  }

  const bytes = await attachmentResponse.arrayBuffer();
  const filename = buildAttachmentFilename(
    attachmentUrl,
    attachmentResponse.headers.get("content-type"),
  );

  const form = new FormData();
  form.append("payload_json", JSON.stringify({
    content,
    attachments: [{ id: 0, filename }],
  }));
  form.append(
    "files[0]",
    new Blob([bytes], { type: attachmentResponse.headers.get("content-type") ?? "application/octet-stream" }),
    filename,
  );

  return fetchImpl(`${apiBaseUrl}/channels/${channelId}/messages`, {
    method: "POST",
    headers: {
      Authorization: `Bot ${token}`,
    },
    body: form,
  });
}

function parseDeliveryRequest(body: Record<string, unknown>): DiscordDeliveryRequest {
  return {
    channel_id: String(body.channel_id ?? "").trim(),
    content: String(body.content ?? "").trim() || undefined,
    attachment_url: String(body.attachment_url ?? "").trim() || undefined,
  };
}

async function readDiscordMessage(response: Response): Promise<{ id?: string }> {
  const raw = await response.text();
  if (!raw) {
    return {};
  }
  try {
    return JSON.parse(raw) as { id?: string };
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
