export type BridgeRequest = {
  event_type: string;
  payload: {
    content?: string;
    limit?: number;
    maxItemsPerScraper?: number;
    userId: string;
    channelId: string;
    messageId: string;
    guildId?: string;
    sessionKey: string;
    startSession?: boolean;
    accountId: string;
  };
};

type BridgeResponse = {
  status?: string;
  reply?: string;
  result?: string;
  payload?: {
    reply?: string;
    result?: string;
  };
  error?: string;
};

export async function dispatchToOrchestrator(
  endpoint: string,
  timeoutMs: number,
  body: BridgeRequest,
): Promise<string> {
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
      headers: { "content-type": "application/json" },
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
    return reply;
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

export function buildBridgePayload(params: {
  eventType: string;
  content?: string;
  limit?: number;
  maxItemsPerScraper?: number;
  startSession?: boolean;
  userId: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  accountId?: string;
  sessionKey?: string;
}): BridgeRequest {
  const accountId = params.accountId?.trim() || "default";
  return {
    event_type: params.eventType,
    payload: {
      content: params.content,
      limit: params.limit,
      maxItemsPerScraper: params.maxItemsPerScraper,
      userId: params.userId,
      channelId: params.channelId,
      messageId: params.messageId,
      guildId: params.guildId,
      sessionKey: params.sessionKey?.trim() || `discord:${params.channelId}:${params.userId}`,
      startSession: params.startSession,
      accountId,
    },
  };
}
