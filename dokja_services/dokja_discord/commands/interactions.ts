import { buildBridgePayload, dispatchToOrchestrator, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import { localizeNotice } from "../messages/notices.js";
import { adapterText } from "../messages/commands.js";
import type {
  DiscordButtonAction,
  DiscordButtonContext,
  DiscordComponentSpec,
  DiscordModalAction,
  DiscordModalContext,
  DiscordSelectAction,
  DiscordSelectContext,
} from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";

function ningoPanelComponents(locale?: string): DiscordComponentSpec[] {
  return [
    {
      kind: "button",
      customId: "ningo_status_button",
      label: adapterText(locale, "panel_status"),
      style: "secondary",
    },
    {
      kind: "button",
      customId: "meme_fetch_button",
      label: adapterText(locale, "panel_meme"),
      style: "primary",
    },
    {
      kind: "button",
      customId: "chat_modal_button",
      label: adapterText(locale, "panel_chat"),
      style: "success",
    },
    {
      kind: "button",
      customId: "radio_stop_button",
      label: adapterText(locale, "panel_radio_stop"),
      style: "danger",
    },
    {
      kind: "select",
      customId: "meme_fetch_select",
      placeholder: adapterText(locale, "panel_meme_count"),
      options: memeCounts(locale),
    },
  ];
}

function memeCounts(locale?: string) {
  return [
    { label: adapterText(locale, "panel_one_meme"), value: "1" },
    { label: adapterText(locale, "panel_memes", { count: 3 }), value: "3" },
    { label: adapterText(locale, "panel_memes", { count: 5 }), value: "5" },
  ];
}

export function buildNingoPanelReply(locale?: string) {
  return {
    content: adapterText(locale, "panel_title"),
    components: ningoPanelComponents(locale),
  } as const;
}

async function fetchMemeReply(
  context:
    | DiscordButtonContext
    | DiscordSelectContext
    | DiscordModalContext,
  orchestratorEndpoint: string,
  orchestratorTimeoutMs: number,
  limit?: number,
) {
  return await dispatchToOrchestrator(
    orchestratorEndpoint,
    orchestratorTimeoutMs,
    buildBridgePayload({
      eventType: "meme.fetch",
      limit,
      userId: context.userId,
      channelId: context.channelId,
      messageId: context.messageId,
      guildId: context.guildId,
    }),
  );
}

async function fetchChatReply(
  context:
    | DiscordButtonContext
    | DiscordModalContext,
  orchestratorEndpoint: string,
  orchestratorTimeoutMs: number,
  content: string,
  locale?: string,
) {
  const response = await dispatchToOrchestratorResponse(
    orchestratorEndpoint,
    orchestratorTimeoutMs,
    buildBridgePayload({
      eventType: "message.created",
      content,
      startSession: true,
      userId: context.userId,
      channelId: context.channelId,
      messageId: context.messageId,
      guildId: context.guildId,
    }),
  );
  const reply = localizeNotice(response.notice, response.reply, locale);
  return response.attachments?.length
    ? { content: reply, attachments: response.attachments }
    : reply;
}

export function createInteractionDefinitions(params: {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  voice?: DiscordVoiceController;
  locale?: string;
}): {
  buttons: DiscordButtonAction[];
  selects: DiscordSelectAction[];
  modals: DiscordModalAction[];
} {
  const { orchestratorEndpoint, orchestratorTimeoutMs, voice } = params;

  const chatModal: DiscordModalAction = {
    customId: "chat_prompt_modal",
    title: adapterText(params.locale, "modal_title"),
    defer: true,
    fields: [
      {
        customId: "prompt",
        label: adapterText(params.locale, "modal_label"),
        placeholder: adapterText(params.locale, "modal_placeholder"),
        required: true,
        style: "paragraph",
      },
    ],
    handle: async (context) => {
      console.log(
        "[dokja-discord] interaction chat modal handler",
        JSON.stringify({ customId: context.customId, messageId: context.messageId }),
      );
      const reply = await fetchChatReply(
        context,
        orchestratorEndpoint,
        orchestratorTimeoutMs,
        context.getText("prompt"),
        params.locale,
      );
      await context.reply(reply);
    },
  };

  return {
    buttons: [
      {
        customId: "ningo_status_button",
        label: adapterText(params.locale, "panel_status"),
        style: "secondary",
        defer: true,
        handle: async (context) => {
          console.log("[dokja-discord] interaction ningo status button", JSON.stringify({ messageId: context.messageId }));
          const reply = await dispatchToOrchestrator(
            orchestratorEndpoint,
            orchestratorTimeoutMs,
            buildBridgePayload({
              eventType: "ningo.status",
              userId: context.userId,
              channelId: context.channelId,
              messageId: context.messageId,
              guildId: context.guildId,
            }),
          );
          await context.update({
            content: reply,
            components: ningoPanelComponents(params.locale),
          });
        },
      },
      {
        customId: "meme_fetch_button",
        label: adapterText(params.locale, "panel_meme"),
        style: "primary",
        defer: true,
        handle: async (context) => {
          console.log("[dokja-discord] interaction meme fetch button", JSON.stringify({ messageId: context.messageId }));
          const reply = await fetchMemeReply(context, orchestratorEndpoint, orchestratorTimeoutMs, 1);
          await context.update({
            content: reply,
            components: ningoPanelComponents(params.locale),
          });
        },
      },
      {
        customId: "chat_modal_button",
        label: adapterText(params.locale, "panel_chat"),
        style: "success",
        defer: false,
        handle: async (context) => {
          console.log("[dokja-discord] interaction chat modal button", JSON.stringify({ messageId: context.messageId }));
          await context.showModal({
            customId: chatModal.customId,
            title: chatModal.title,
            fields: chatModal.fields,
          });
        },
      },
      {
        customId: "radio_stop_button",
        label: adapterText(params.locale, "panel_radio_stop"),
        style: "danger",
        defer: false,
        handle: async (context) => {
          if (!voice) {
            throw new Error("Voice controller is not configured");
          }
          const reply = await voice.stopRadio({ guildId: context.guildId });
          await context.reply(reply);
        },
      },
    ],
    selects: [
      {
        customId: "meme_fetch_select",
        placeholder: adapterText(params.locale, "panel_meme_count"),
        defer: true,
        options: memeCounts(params.locale),
        handle: async (context) => {
          console.log(
            "[dokja-discord] interaction meme fetch select",
            JSON.stringify({ messageId: context.messageId, values: context.values }),
          );
          const limit = Number.parseInt(context.values[0] ?? "1", 10);
          const reply = await fetchMemeReply(
            context,
            orchestratorEndpoint,
            orchestratorTimeoutMs,
            Number.isFinite(limit) ? limit : 1,
          );
          await context.update({
            content: reply,
            components: ningoPanelComponents(params.locale),
          });
        },
      },
    ],
    modals: [chatModal],
  };
}
