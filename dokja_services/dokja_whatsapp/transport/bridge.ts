export type IncomingDocument = {
  filename: string;
  content_type: string;
  size: number;
  data_b64?: string;
};
export type ReplyMedia = {filename: string; content_type: string; data_b64: string};
export type WhatsAppContentReply = {
  reply: string;
  attachments?: ReplyMedia[];
  content_error?: string;
  document_required?: boolean;
  summary_partial?: boolean;
};

export type WhatsAppBridgeRequest = {
  event_type: "message.created";
  interface: "whatsapp";
  payload: {
    content: string;
    document?: IncomingDocument;
    locale?: string;
    userId: string;
    channelId: string;
    messageId: string;
    sessionKey: string;
    accountId: string;
    entryKind: "message";
    chatKind: "direct" | "group" | "other";
    senderNumbers: string[];
    isSelfChat: boolean;
    isOwnAccount: boolean;
    mentionsBot: boolean;
  };
};

type BridgeResponse = {
  attachments?: ReplyMedia[];
  content_error?: string;
  document_required?: boolean;
  summary_partial?: boolean;
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
  document?: IncomingDocument;
  locale?: string;
  userId: string;
  chatId: string;
  messageId: string;
  chatKind: "direct" | "group" | "other";
  senderNumbers: string[];
  isSelfChat: boolean;
  // WhatsApp marked the message as the linked account's own.
  isOwnAccount: boolean;
  mentionsBot: boolean;
}): WhatsAppBridgeRequest {
  return {
    event_type: "message.created",
    interface: "whatsapp",
    payload: {
      content: params.content,
      ...(params.document ? {document: params.document} : {}),
      ...(params.locale ? {locale: params.locale} : {}),
      userId: params.userId,
      channelId: params.chatId,
      messageId: params.messageId,
      sessionKey: `whatsapp:${params.chatId}:${params.userId}`,
      accountId: "default",
      entryKind: "message",
      chatKind: params.chatKind,
      senderNumbers: params.senderNumbers,
      isSelfChat: params.isSelfChat,
      isOwnAccount: params.isOwnAccount,
      mentionsBot: params.mentionsBot,
    },
  };
}

export async function dispatchToOrchestratorResponse(
  endpoint: string,
  timeoutMs: number,
  body: WhatsAppBridgeRequest,
  fetchImpl: typeof fetch = fetch,
): Promise<WhatsAppContentReply> {
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
    const text = payload.reply ?? payload.result ?? payload.payload?.reply ?? payload.payload?.result ?? "";
    return {
      reply: typeof text === "string" ? text : "",
      attachments: Array.isArray(payload.attachments) ? payload.attachments : undefined,
      content_error: typeof payload.content_error === "string" ? payload.content_error : undefined,
      document_required: payload.document_required === true,
      summary_partial: payload.summary_partial === true,
    };

  } finally {
    clearTimeout(timeout);
  }
}

// Retained for callers that only need ordinary text replies.
export async function dispatchToOrchestrator(
  endpoint: string,
  timeoutMs: number,
  body: WhatsAppBridgeRequest,
  fetchImpl: typeof fetch = fetch,
): Promise<string> {
  return (await dispatchToOrchestratorResponse(endpoint, timeoutMs, body, fetchImpl)).reply;
}
