export type WhatsAppBridgeRequest = {
  event_type: "message.created";
  interface: "whatsapp";
  payload: {
    content: string;
    userId: string;
    channelId: string;
    messageId: string;
    sessionKey: string;
    accountId: string;
    entryKind: "message";
    chatKind: "direct" | "group" | "other";
    senderNumbers: string[];
    isSelfChat: boolean;
    mentionsBot: boolean;
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

// The event type is fixed here on purpose: the text of a WhatsApp message must never be able to
// choose how the orchestrator routes the request.
export function buildWhatsAppBridgeRequest(params: {
  content: string;
  userId: string;
  chatId: string;
  messageId: string;
  chatKind: "direct" | "group" | "other";
  senderNumbers: string[];
  isSelfChat: boolean;
  mentionsBot: boolean;
}): WhatsAppBridgeRequest {
  return {
    event_type: "message.created",
    interface: "whatsapp",
    payload: {
      content: params.content,
      userId: params.userId,
      channelId: params.chatId,
      messageId: params.messageId,
      sessionKey: `whatsapp:${params.chatId}:${params.userId}`,
      accountId: "default",
      entryKind: "message",
      chatKind: params.chatKind,
      senderNumbers: params.senderNumbers,
      isSelfChat: params.isSelfChat,
      mentionsBot: params.mentionsBot,
    },
  };
}

export async function dispatchToOrchestrator(
  endpoint: string,
  timeoutMs: number,
  body: WhatsAppBridgeRequest,
  fetchImpl: typeof fetch = fetch,
): Promise<string> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetchImpl(endpoint, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${process.env.DOKJA_GATEWAY_INGRESS_TOKEN ?? ""}` },
      body: JSON.stringify(body),
      signal: controller.signal,
    });
    if (!response.ok) {
      throw new Error(`orchestrator http ${response.status}`);
    }

    const payload = (await response.json()) as BridgeResponse;
    const status = (payload.status ?? "ok").toLowerCase();
    if (status !== "ok") {
      throw new Error(payload.error ?? `orchestrator status=${status}`);
    }
    return payload.reply ?? payload.result ?? payload.payload?.reply ?? payload.payload?.result ?? "";
  } finally {
    clearTimeout(timeout);
  }
}
