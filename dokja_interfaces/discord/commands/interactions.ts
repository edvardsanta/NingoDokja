import { buildBridgePayload, dispatchToOrchestrator } from "../transport/bridge.js";
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

function ningoPanelComponents(): DiscordComponentSpec[] {
  return [
    {
      kind: "button",
      customId: "ningo_status_button",
      label: "Ningo Status",
      style: "secondary",
    },
    {
      kind: "button",
      customId: "meme_fetch_button",
      label: "Solta um meme ai",
      style: "primary",
    },
    {
      kind: "button",
      customId: "chat_modal_button",
      label: "Fale comigo, Dokja!",
      style: "success",
    },
    {
      kind: "button",
      customId: "radio_stop_button",
      label: "Stop Radio",
      style: "danger",
    },
    {
      kind: "select",
      customId: "meme_fetch_select",
      placeholder: "Escolha quantos memes",
      options: [
        { label: "1 meme", value: "1" },
        { label: "3 memes", value: "3" },
        { label: "5 memes", value: "5" },
      ],
    },
  ];
}

export function buildNingoPanelReply() {
  return {
    content: "Ningo controls",
    components: ningoPanelComponents(),
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
) {
  return await dispatchToOrchestrator(
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
}

export function createInteractionDefinitions(params: {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  voice?: DiscordVoiceController;
}): {
  buttons: DiscordButtonAction[];
  selects: DiscordSelectAction[];
  modals: DiscordModalAction[];
} {
  const { orchestratorEndpoint, orchestratorTimeoutMs, voice } = params;

  const chatModal: DiscordModalAction = {
    customId: "chat_prompt_modal",
    title: "Chat With Dokja",
    defer: true,
    fields: [
      {
        customId: "prompt",
        label: "Prompt",
        placeholder: "Ask Dokja something...",
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
      );
      await context.reply(reply);
    },
  };

  return {
    buttons: [
      {
        customId: "ningo_status_button",
        label: "Ningo Status",
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
            components: ningoPanelComponents(),
          });
        },
      },
      {
        customId: "meme_fetch_button",
        label: "Fetch Meme",
        style: "primary",
        defer: true,
        handle: async (context) => {
          console.log("[dokja-discord] interaction meme fetch button", JSON.stringify({ messageId: context.messageId }));
          const reply = await fetchMemeReply(context, orchestratorEndpoint, orchestratorTimeoutMs, 1);
          await context.update({
            content: reply,
            components: ningoPanelComponents(),
          });
        },
      },
      {
        customId: "chat_modal_button",
        label: "Chat Prompt",
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
        label: "Stop Radio",
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
        placeholder: "Choose meme fetch count",
        defer: true,
        options: [
          { label: "1 meme", value: "1" },
          { label: "3 memes", value: "3" },
          { label: "5 memes", value: "5" },
        ],
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
            components: ningoPanelComponents(),
          });
        },
      },
    ],
    modals: [chatModal],
  };
}
