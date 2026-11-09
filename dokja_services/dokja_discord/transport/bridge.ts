import type { DiscordAttachment, DiscordDocumentAttachment } from "../types.js";

export type BridgeRequest = {
  event_type: string;
  payload: {
    realtime?: Record<string, unknown>;
    voice?: Record<string, unknown>;
    voiceTurns?: Array<{ userId: string; userName?: string; text: string }>;
    voiceChannelId?: string;
    voiceSessionId?: string;
    voiceStartedBy?: string;
    content?: string;
    documentAttachments?: DiscordDocumentAttachment[];
    locale?: string;
    replyToContent?: string;
    mentionedUserIds?: string[];
    limit?: number;
    maxItemsPerScraper?: number;
    query?: string;
    brand?: string;
    merchant?: string;
    userId: string;
    userName?: string;
    channelId: string;
    messageId: string;
    guildId?: string;
    sessionKey: string;
    startSession?: boolean;
    accountId: string;
    entryKind: "message" | "command";
    isDirectMessage: boolean;
    mentionsBot: boolean;
  };
};

type BridgeResponse = {
  status?: string;
  reply?: string;
  notice?: string;
  result?: string;
  payload?: {
    reply?: string;
    result?: string;
  };
  error?: string;
  attachments?: unknown;
  // a server's queue, after a music command
  music?: unknown;
  // a price search's result, after price.search
  price?: unknown;
  // a questionnaire run's result or refusal code, after quiz.take
  quiz?: unknown;
  realtime?: { accepted?: boolean };
};

export type OrchestratorReply = {
  reply: string;
  notice?: string;
  // a price search's result (price.search), rendered by the interface in its own language
  price?: unknown;
  // a questionnaire run (quiz.take): result link, label and comment, or a refusal code
  quiz?: unknown;
  attachments?: DiscordAttachment[];
  music?: unknown;
  realtime?: { accepted?: boolean };
};

export async function dispatchToOrchestrator(
  endpoint: string,
  timeoutMs: number,
  body: BridgeRequest,
): Promise<string> {
  const result = await dispatchToOrchestratorResponse(endpoint, timeoutMs, body);
  return result.reply;
}

export async function dispatchToOrchestratorResponse(
  endpoint: string,
  timeoutMs: number,
  body: BridgeRequest,
): Promise<OrchestratorReply> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    console.log(
      "[dokja-discord] bridge request",
      JSON.stringify({
        eventType: body.event_type,
        endpoint,
        channelId: body.payload.channelId,
        userId: body.payload.userId,
        messageId: body.payload.messageId,
      }),
    );
    const response = await fetch(endpoint, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${process.env.DOKJA_GATEWAY_INGRESS_TOKEN ?? ""}` },
      body: JSON.stringify(body),
      signal: controller.signal,
    });
    if (!response.ok) {
      console.error(
        "[dokja-discord] bridge http error",
        JSON.stringify({
          eventType: body.event_type,
          status: response.status,
        }),
      );
      throw new Error(`orchestrator http ${response.status}`);
    }

    const payload = (await response.json()) as BridgeResponse;
    const status = (payload.status ?? "ok").toLowerCase();
    if (status !== "ok") {
      console.error(
        "[dokja-discord] bridge payload error",
        JSON.stringify({
          eventType: body.event_type,
          status,
          error: payload.error,
        }),
      );
      throw new Error(payload.error ?? `orchestrator status=${status}`);
    }

    const reply = payload.reply ?? payload.result ?? payload.payload?.reply ?? payload.payload?.result ?? "";
    console.log(
      "[dokja-discord] bridge response",
      JSON.stringify({
        eventType: body.event_type,
        status,
        replyLength: reply.length,
      }),
    );
    const attachments = validatedAttachments(payload.attachments);
    return {
      reply,
      notice: payload.notice,
      ...(attachments.length ? { attachments } : {}),
      ...(payload.music !== undefined ? { music: payload.music } : {}),
      ...(payload.price !== undefined ? { price: payload.price } : {}),
      ...(payload.quiz !== undefined ? { quiz: payload.quiz } : {}),
      ...(payload.realtime?.accepted === true ? { realtime: {accepted: true} } : {}),
    };
  } catch (error) {
    console.error(
      "[dokja-discord] bridge dispatch failed",
      JSON.stringify({
        eventType: body.event_type,
        error: error instanceof Error ? error.message : String(error),
      }),
    );
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}

function validatedAttachments(value: unknown): DiscordAttachment[] {
  if (!Array.isArray(value)) return [];
  return value.slice(0, 3).filter((entry): entry is DiscordAttachment => {
    if (!entry || entry.content_type !== "image/png" || !/^formula-[1-3]\.png$/.test(entry.filename)
      || typeof entry.data_b64 !== "string" || entry.data_b64.length > 349528
      || !/^[A-Za-z0-9+/]+={0,2}$/.test(entry.data_b64)) return false;
    const data = Buffer.from(entry.data_b64, "base64");
    if (data.length < 24 || data.length > 262144 || data.toString("hex", 0, 8) !== "89504e470d0a1a0a"
      || data.toString("ascii", 12, 16) !== "IHDR") return false;
    const width = data.readUInt32BE(16);
    const height = data.readUInt32BE(20);
    return width > 0 && width <= 2232 && height > 0 && height <= 1032;
  });
}

export function buildBridgePayload(params: {
  eventType: string;
  content?: string;
  documentAttachments?: DiscordDocumentAttachment[];
  locale?: string;
  replyToContent?: string;
  mentionedUserIds?: string[];
  limit?: number;
  maxItemsPerScraper?: number;
  query?: string;
  brand?: string;
  merchant?: string;
  startSession?: boolean;
  userId: string;
  userName?: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  accountId?: string;
  sessionKey?: string;
  entryKind?: "message" | "command";
  isDirectMessage?: boolean;
  mentionsBot?: boolean;
}): BridgeRequest {
  const accountId = params.accountId?.trim() || "default";
  return {
    event_type: params.eventType,
    payload: {
      content: params.content,
      documentAttachments: params.documentAttachments?.slice(0, 10),
      locale: params.locale,
      replyToContent: params.replyToContent?.slice(0, 8000),
      mentionedUserIds: params.mentionedUserIds?.slice(0, 20),
      limit: params.limit,
      maxItemsPerScraper: params.maxItemsPerScraper,
      query: params.query?.slice(0, 100),
      brand: params.brand?.slice(0, 100),
      merchant: params.merchant?.slice(0, 80),
      userId: params.userId,
      userName: params.userName === undefined ? undefined : Array.from(params.userName).slice(0, 100).join(""),
      channelId: params.channelId,
      messageId: params.messageId,
      guildId: params.guildId,
      sessionKey: params.sessionKey?.trim() || `discord:${params.channelId}:${params.userId}`,
      startSession: params.startSession,
      accountId,
      entryKind: params.entryKind ?? "command",
      isDirectMessage: params.isDirectMessage ?? !params.guildId,
      mentionsBot: params.mentionsBot ?? false,
    },
  };
}
