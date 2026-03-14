import { ApplicationCommandOptionType } from "discord-api-types/v10";

import { buildBridgePayload, dispatchToOrchestrator } from "../transport/bridge.js";
import type { DiscordCommandContext, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";
import { buildNingoPanelReply } from "./interactions.js";

type DokjaCommandParams = {
  name: string;
  description: string;
  options?: any[];
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  eventType: string;
  buildPayload?: (context: DiscordCommandContext) => Partial<Parameters<typeof buildBridgePayload>[0]>;
};

function createDokjaCommand(params: DokjaCommandParams): DiscordSlashCommand {
  const { name, description, options, orchestratorEndpoint, orchestratorTimeoutMs, eventType, buildPayload } =
    params;

  return {
    name,
    description,
    options,
    handle: async (context: DiscordCommandContext) => {
      try {
        const reply = (
          await dispatchToOrchestrator(
            orchestratorEndpoint,
            orchestratorTimeoutMs,
            buildBridgePayload({
              eventType,
              userId: context.userId,
              channelId: context.channelId,
              messageId: context.messageId,
              guildId: context.guildId,
              ...(buildPayload?.(context) ?? {}),
            }),
          )
        ).trim() || "No response.";
        await context.reply(reply);
      } catch (error) {
        console.error(`[dokja-discord] command ${name} failed`, error);
        await context.reply("Command failed.", { ephemeral: true });
      }
    },
  };
}

export function createCommands(
  orchestratorEndpoint: string,
  orchestratorTimeoutMs: number,
  voice?: DiscordVoiceController,
): DiscordSlashCommand[] {
  return [
    createDokjaCommand({
      name: "chat",
      description: "Send a chat message through Dokja.",
      orchestratorEndpoint,
      orchestratorTimeoutMs,
      eventType: "message.created",
      options: [
        {
          name: "prompt",
          description: "Message to send",
          type: ApplicationCommandOptionType.String,
          required: true,
        },
      ],
      buildPayload: (context) => ({
        content: context.getString("prompt"),
        startSession: true,
      }),
    }),
    createDokjaCommand({
      name: "meme_fetch",
      description: "Fetch memes from Dokja.",
      orchestratorEndpoint,
      orchestratorTimeoutMs,
      eventType: "meme.fetch",
      options: [
        {
          name: "limit",
          description: "Number of memes",
          type: ApplicationCommandOptionType.Number,
          required: false,
        },
      ],
      buildPayload: (context) => ({
        limit: context.getNumber("limit"),
      }),
    }),
    createDokjaCommand({
      name: "meme_status",
      description: "Show meme service status.",
      orchestratorEndpoint,
      orchestratorTimeoutMs,
      eventType: "meme.status",
    }),
    createDokjaCommand({
      name: "meme_refresh",
      description: "Refresh the meme pool.",
      orchestratorEndpoint,
      orchestratorTimeoutMs,
      eventType: "meme.pool.refresh",
      options: [
        {
          name: "max_items",
          description: "Max items per scraper",
          type: ApplicationCommandOptionType.Number,
          required: false,
        },
      ],
      buildPayload: (context) => ({
        maxItemsPerScraper: context.getNumber("max_items"),
      }),
    }),
    {
      name: "play_radio",
      description: "Play a radio stream in your current voice channel.",
      defer: false,
      options: [
        {
          name: "url",
          description: "Radio stream URL",
          type: ApplicationCommandOptionType.String,
          required: true,
        },
      ],
      async handle(context: DiscordCommandContext) {
        if (!voice) {
          await context.reply("Voice controller is not configured.", { ephemeral: true });
          return;
        }

        const streamUrl = context.getString("url");
        await context.reply("Starting radio playback...", { ephemeral: true });

        void voice
          .playRadio({
            guildId: context.guildId,
            userId: context.userId,
            streamUrl,
          })
          .then((reply) => {
            console.log(
              "[dokja-discord] command play_radio started",
              JSON.stringify({
                guildId: context.guildId,
                userId: context.userId,
                reply,
              }),
            );
            return context.updateReply(reply);
          })
          .catch((error) => {
            console.error("[dokja-discord] command play_radio failed", error);
            return context.updateReply(
              error instanceof Error ? error.message : "Could not start the radio.",
            );
          });
      },
    },
    {
      name: "stop_radio",
      description: "Stop the current radio playback in this server.",
      async handle(context: DiscordCommandContext) {
        try {
          if (!voice) {
            throw new Error("Voice controller is not configured");
          }
          const reply = await voice.stopRadio({ guildId: context.guildId });
          await context.reply(reply, { ephemeral: true });
        } catch (error) {
          console.error("[dokja-discord] command stop_radio failed", error);
          await context.reply("Could not stop the radio.", { ephemeral: true });
        }
      },
    },
    {
      name: "ningo_panel",
      description: "Open the Ningo interaction panel.",
      defer: false,
      async handle(context: DiscordCommandContext) {
        await context.reply(buildNingoPanelReply());
      },
    },
    {
      name: "chat_modal",
      description: "Open a chat prompt modal.",
      defer: false,
      async handle(context: DiscordCommandContext) {
        await context.showModal({
          customId: "chat_prompt_modal",
          title: "Chat With Dokja",
          fields: [
            {
              customId: "prompt",
              label: "Prompt",
              placeholder: "Ask Dokja something...",
              required: true,
              style: "paragraph",
            },
          ],
        });
      },
    },
  ];
}
