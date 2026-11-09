import { observerCommands, startVoiceChatMirror } from "./observer_commands.js";
import { conversationReply } from "../voice/conversation_reply.js";
import { ApplicationCommandOptionType } from "discord-api-types/v10";
import { randomUUID } from "node:crypto";

import { buildBridgePayload, dispatchToOrchestrator, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordCommandContext, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";
import { createMusic, type MusicFeature } from "../music/commands.js";
import { buildNingoPanelReply } from "./interactions.js";
import { localizeNotice } from "../messages/notices.js";
import { adapterText, translateCommand } from "../messages/commands.js";
import { createCompareCommand, createPriceCommand } from "./price_command.js";
import { createQuizCommand } from "./quiz_command.js";

type DokjaCommandParams = {
  name: string;
  description: string;
  options?: any[];
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  eventType: string;
  ephemeralResponse?: boolean;
  locale?: string;
  buildPayload?: (context: DiscordCommandContext) => Partial<Parameters<typeof buildBridgePayload>[0]>;
};

function createDokjaCommand(params: DokjaCommandParams): DiscordSlashCommand {
  const {
    name,
    description,
    options,
    orchestratorEndpoint,
    orchestratorTimeoutMs,
    eventType,
    ephemeralResponse = true,
    locale,
    buildPayload,
  } =
    params;

  return {
    name,
    description,
    options,
    handle: async (context: DiscordCommandContext) => {
      try {
        const response = await dispatchToOrchestratorResponse(
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
        );
        const reply = localizeNotice(response.notice, response.reply.trim() || adapterText(locale, "no_response"), locale);
        await context.reply(response.attachments?.length
          ? { content: reply, attachments: response.attachments }
          : reply, { ephemeral: ephemeralResponse });
      } catch (error) {
        console.error(`[dokja-discord] command ${name} failed`, error);
        await context.reply(adapterText(locale, "command_failed"), { ephemeral: true });
      }
    },
  };
}

export function createCommands(
  orchestratorEndpoint: string,
  orchestratorTimeoutMs: number,
  voice?: DiscordVoiceController,
  options?: {
    logVoiceConversation?: boolean;
    locale?: string;
    // the music feature whose buttons the runtime also serves; one is made when left out
    music?: MusicFeature;
  },
): DiscordSlashCommand[] {
  const locale = options?.locale;
  return [
    ...observerCommands(orchestratorEndpoint, orchestratorTimeoutMs, voice, options?.locale),
    createDokjaCommand({
      name: "chat",
      locale: options?.locale,
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
      locale,
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
      name: "meme_refresh",
      locale,
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
      name: "speak",
      description: "Speak a text in your current voice channel.",
      defer: false,
      options: [
        {
          name: "prompt",
          description: "Text to be spoken",
          type: ApplicationCommandOptionType.String,
          required: true,
        },
      ],
      async handle(context: DiscordCommandContext) {
        if (!voice) {
          await context.reply(adapterText(locale, "voice_missing"), { ephemeral: true });
          return;
        }

        const prompt = context.getString("prompt");
        await context.reply(adapterText(locale, "speech_starting"), { ephemeral: true });

        void voice
          .speakText({
            guildId: context.guildId,
            userId: context.userId,
            text: prompt,
            originChannelId: context.channelId,
          })
          .then((reply) => context.updateReply(reply))
          .catch((error) => {
            console.error("[dokja-discord] command speak failed", error);
            return context.updateReply(
              error instanceof Error ? error.message : adapterText(locale, "speech_failed"),
            );
          });
      },
    },
    {
      name: "voice_chat_start",
      description: "Join the live conversation with everyone in your voice channel.",
      defer: false,
      options: [
        {
          name: "mode",
          description: "When Ningo answers",
          type: ApplicationCommandOptionType.String,
          required: false,
          choices: [
            { name: "Always", value: "always" },
            { name: "Only after the wake word", value: "wakeword" },
          ],
        },
      ],
      async handle(context: DiscordCommandContext) {
        // Always answering is the default; the wake word makes Ningo wait to be called.
        const activationMode = context.getString("mode") === "wakeword" ? "wakeword" : "always";
        if (!voice) {
          await context.reply(adapterText(locale, "voice_missing"), { ephemeral: true });
          return;
        }

        await context.reply(adapterText(locale, activationMode === "wakeword" ? "wake_starting" : "voice_starting"), { ephemeral: true });

        const voiceSessionId = randomUUID();

        // What is said is also shown live in the owner's Desktop realtime view.
        const mirror = await startVoiceChatMirror(orchestratorEndpoint, orchestratorTimeoutMs, context, voice);
        void voice
          .startConversation({
            guildId: context.guildId,
            userId: context.userId,
            activationMode,
            originChannelId: context.channelId,
            onHeard: mirror?.heard,
            onStopped: mirror?.stop,
            onTurn: async (_transcript, turn) => {
              if (!turn) throw new Error("Voice speaker metadata is required");
              // Ningo's words join the live transcript as each sentence starts playing.
              const said = mirror?.reply(Math.max(0, ...turn.turns.map((heard) => heard.sequence)));
              const reply = await conversationReply(orchestratorEndpoint, orchestratorTimeoutMs, {
                guildId: context.guildId, originChannelId: context.channelId, turn,
                voiceSessionId, voiceStartedBy: context.userId,
                onSpeech: async (sentence) => {
                  await turn.waitForFloor?.();
                  const speech = { guildId: context.guildId, userId: turn.userId,
                    channelId: turn.channelId, originChannelId: context.channelId,
                    text: sentence, signal: turn.signal, trace: turn.trace };
                  said?.said(sentence);
                  if (voice.speakReply) await voice.speakReply(speech);
                  else await voice.speakText(speech);
                },
              });
              turn.signal.throwIfAborted();
              if (reply.trim()) {
                const speech = {
                  guildId: context.guildId, userId: turn.userId, channelId: turn.channelId,
                  originChannelId: context.channelId, text: reply, signal: turn.signal, trace: turn.trace,
                  waitForFloor: turn.waitForFloor,
                };
                await turn.waitForFloor?.();
                said?.said(reply);
                if (voice.speakReply) await voice.speakReply(speech);
                else await voice.speakText(speech);
              }
            },
          })
          .then((reply) => context.updateReply(reply))
          .catch((error) => {
            mirror?.stop();
            console.error("[dokja-discord] command voice_chat_start failed", error);
            return context.updateReply(
              error instanceof Error ? error.message : adapterText(locale, "voice_failed"),
            );
          });
      },
    },
    {
      name: "voice_chat_stop",
      description: "Stop the live voice conversation in this server.",
      async handle(context: DiscordCommandContext) {
        try {
          if (!voice) {
            throw new Error("Voice controller is not configured");
          }
          const reply = await voice.stopConversation({ guildId: context.guildId });
          await context.reply(reply, { ephemeral: true });
        } catch (error) {
          console.error("[dokja-discord] command voice_chat_stop failed", error);
          await context.reply(adapterText(locale, "voice_stop_failed"), { ephemeral: true });
        }
      },
    },
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
          await context.reply(adapterText(locale, "voice_missing"), { ephemeral: true });
          return;
        }

        const streamUrl = context.getString("url");
        await context.reply(adapterText(locale, "radio_starting"), { ephemeral: true });

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
              error instanceof Error ? error.message : adapterText(locale, "radio_failed"),
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
          await context.reply(adapterText(locale, "radio_stop_failed"), { ephemeral: true });
        }
      },
    },
    {
      name: "ningo_panel",
      description: "Open the Ningo interaction panel.",
      defer: false,
      async handle(context: DiscordCommandContext) {
        await context.reply(buildNingoPanelReply(locale), { ephemeral: true });
      },
    },
    createPriceCommand(orchestratorEndpoint, orchestratorTimeoutMs, options?.locale),
    createCompareCommand(orchestratorEndpoint, orchestratorTimeoutMs, options?.locale),
    createQuizCommand(orchestratorEndpoint, options?.locale),
    // Music in a voice channel: the queue lives in the orchestrator.
    ...(options?.music ?? createMusic({ orchestratorEndpoint, orchestratorTimeoutMs, voice })).commands,
  ].map(translateCommand);
}
