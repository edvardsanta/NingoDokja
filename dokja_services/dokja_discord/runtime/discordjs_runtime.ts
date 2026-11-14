import {
  ActionRowBuilder,
  ButtonBuilder,
  ButtonStyle,
  ChannelType,
  Client,
  Events,
  GatewayIntentBits,
  InteractionEditReplyOptions,
  InteractionReplyOptions,
  MessageFlags,
  ModalBuilder,
  REST,
  Routes,
  StringSelectMenuBuilder,
  TextInputBuilder,
  TextInputStyle,
  type ButtonInteraction,
  type ChatInputCommandInteraction,
  type Message,
  type ModalSubmitInteraction,
  type StringSelectMenuInteraction,
} from "discord.js";

import { pdfAttachments } from "../messages/documents.js";
import type {
  DiscordButtonAction,
  DiscordButtonContext,
  DiscordChoice,
  DiscordEmbedSpec,
  DiscordModalAction,
  DiscordModalContext,
  DiscordMessageContext,
  DiscordMessageHandler,
  DiscordReply,
  DiscordSelectAction,
  DiscordSelectContext,
  DiscordSlashCommand,
} from "../types.js";
import { trackGatewayState, type GatewayStateTracker } from "./gateway_state.js";

type StartDiscordJsRuntimeParams = {
  token: string;
  signal?: AbortSignal;
  onClientCreated?: (client: Client) => void;
  applicationId?: string;
  devGuildIds?: string[];
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
  commands: DiscordSlashCommand[];
  onMessage: DiscordMessageHandler;
  buttons?: DiscordButtonAction[];
  selects?: DiscordSelectAction[];
  modals?: DiscordModalAction[];
  onReady?: (client: Client, meta: { botUserId?: string }) => Promise<void> | void;
  // Told whether the gateway is connected, for the interface's status.
  gateway?: GatewayStateTracker;
};

export async function startDiscordJsRuntime(params: StartDiscordJsRuntimeParams): Promise<void> {
  const client = new Client({
    intents: [
      GatewayIntentBits.Guilds,
      GatewayIntentBits.GuildMessages,
      GatewayIntentBits.GuildVoiceStates,
      GatewayIntentBits.MessageContent,
      GatewayIntentBits.DirectMessages,
    ],
  });

  params.onClientCreated?.(client);

  const buttons = params.buttons ?? [];
  const selects = params.selects ?? [];
  const modals = params.modals ?? [];

  client.on(Events.ClientReady, async (readyClient) => {
    if (params.signal?.aborted) return;
    const applicationId =
      params.applicationId?.trim() ||
      readyClient.application?.id ||
      readyClient.user.id;
    params.log?.(
      "[dokja-discord] discord.js ready",
      JSON.stringify({ userId: readyClient.user.id, applicationId }),
    );
    try {
      await deployCommands({
        ...params,
        applicationId,
      });
    } catch (error) {
      params.error?.(
        "[dokja-discord] command deploy failed",
        JSON.stringify({
          applicationId,
          guildIds: params.devGuildIds,
          error: error instanceof Error ? error.message : String(error),
        }),
      );
    }
    if (params.signal?.aborted) return;
    await params.onReady?.(client, { botUserId: readyClient.user.id });
  });

  client.on(Events.MessageCreate, async (message) => {
    try {
      if (params.signal?.aborted) return;
      await params.onMessage(await toMessageContext(message, client.user?.id));
    } catch (error) {
      params.error?.("[dokja-discord] message listener failed", error);
    }
  });

  client.on(Events.InteractionCreate, async (interaction) => {
    if (params.signal?.aborted) return;
    try {
      if (interaction.isChatInputCommand()) {
        const command = params.commands.find((entry) => entry.name === interaction.commandName);
        if (!command) {
          return;
        }
        await handleChatInputCommand(command, interaction, buttons, selects, modals, params);
        return;
      }
      if (interaction.isAutocomplete()) {
        const command = params.commands.find((entry) => entry.name === interaction.commandName);
        if (command) await answerAutocomplete(command, interaction, params);
        return;
      }
      if (interaction.isButton()) {
        const button = buttons.find((entry) => entry.customId === interaction.customId);
        if (!button) {
          return;
        }
        await handleButton(button, interaction, buttons, selects, modals, params);
        return;
      }
      if (interaction.isStringSelectMenu()) {
        const select = selects.find((entry) => entry.customId === interaction.customId);
        if (!select) {
          return;
        }
        await handleSelect(select, interaction, buttons, selects, modals, params);
        return;
      }
      if (interaction.isModalSubmit()) {
        const modal = modals.find((entry) => entry.customId === interaction.customId);
        if (!modal) {
          return;
        }
        await handleModal(modal, interaction, buttons, selects, modals, params);
      }
    } catch (error) {
      params.error?.("[dokja-discord] interaction failed", error);
    }
  });

  client.on(Events.Error, (error) => {
    params.error?.("[dokja-discord] discord.js runtime error", error);
  });
  if (params.gateway) {
    trackGatewayState(client, params.gateway);
  }

  if (!params.signal?.aborted) await client.login(params.token);
}

type CommandRest = { put(route: `/${string}`, options: { body: unknown }): Promise<unknown> };

export async function deployCommands(
  params: Pick<StartDiscordJsRuntimeParams, "token" | "applicationId" | "devGuildIds" | "commands" | "log">,
  rest: CommandRest = new REST({ version: "10" }).setToken(params.token),
) {
  const applicationId = params.applicationId?.trim();
  if (!applicationId) {
    throw new Error("discord application id is required for command deployment");
  }
  const payload = params.commands.map(serializeCommand);
  // Commands registered on a server appear there at once; each configured server gets them.
  const guildIds = params.devGuildIds ?? [];
  if (guildIds.length > 0) {
    for (const guildId of guildIds) {
      await rest.put(Routes.applicationGuildCommands(applicationId, guildId), {
        body: payload,
      });
      params.log?.(
        "[dokja-discord] command deploy",
        JSON.stringify({ mode: "guild", guildId, count: payload.length }),
      );
    }
    return;
  }
  await rest.put(Routes.applicationCommands(applicationId), { body: payload });
  params.log?.(
    "[dokja-discord] command deploy",
    JSON.stringify({ mode: "global", count: payload.length }),
  );
}

// The command as Discord's API takes it. Every option field the commands declare passes through
// (integer bounds, autocomplete, choices and the translations people see in their own language),
// so nothing a command declares is dropped on the way.
const OPTION_FIELDS = ["type", "name", "description", "required", "autocomplete", "min_value", "max_value",
  "min_length", "max_length", "choices", "name_localizations", "description_localizations"] as const;

export function serializeCommand(command: DiscordSlashCommand) {
  const options = (command.options ?? []).map((option) => Object.fromEntries(
    OPTION_FIELDS.filter((field) => option[field] !== undefined).map((field) => [field, option[field]]),
  ));
  return {
    type: 1,
    name: command.name,
    description: command.description,
    ...(command.name_localizations ? { name_localizations: command.name_localizations } : {}),
    ...(command.description_localizations ? { description_localizations: command.description_localizations } : {}),
    ...(options.length > 0 ? { options } : {}),
  };
}

// Discord's limits for suggestions: 25 of them, 100 characters each.
const MAX_CHOICES = 25;
const MAX_CHOICE_TEXT = 100;

type AutocompleteInteraction = {
  commandName: string;
  user: { id: string };
  channelId: string | null;
  guildId: string | null;
  options: { getFocused(full: true): { name: string; value: unknown } };
  respond(choices: DiscordChoice[]): Promise<unknown>;
};

// Answers a suggestion request. A failing or slow suggester answers nothing rather than leaving
// Discord waiting, and what it suggests is cut to Discord's limits.
export async function answerAutocomplete(definition: DiscordSlashCommand, interaction: AutocompleteInteraction, params: Pick<StartDiscordJsRuntimeParams, "error">) {
  let choices: DiscordChoice[] = [];
  if (definition.autocomplete) {
    const focused = interaction.options.getFocused(true);
    try {
      choices = await definition.autocomplete({
        option: focused.name, value: String(focused.value ?? ""),
        userId: interaction.user.id, channelId: interaction.channelId ?? "", guildId: interaction.guildId ?? undefined,
      });
    } catch (error) {
      params.error?.("[dokja-discord] autocomplete failed", error);
    }
  }
  const bounded = choices
    .filter((choice) => choice.name.trim() && choice.value.trim())
    .slice(0, MAX_CHOICES)
    .map((choice) => ({ name: choice.name.slice(0, MAX_CHOICE_TEXT), value: choice.value.slice(0, MAX_CHOICE_TEXT) }));
  await interaction.respond(bounded);
}

export async function handleChatInputCommand(
  definition: DiscordSlashCommand,
  interaction: ChatInputCommandInteraction,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
  modals: DiscordModalAction[],
  params: StartDiscordJsRuntimeParams,
) {
  params.log?.(
    "[dokja-discord] slash command",
    JSON.stringify({
      name: definition.name,
      userId: interaction.user.id,
      channelId: interaction.channelId,
      guildId: interaction.guildId ?? undefined,
    }),
  );

  if (definition.defer ?? true) {
    await interaction.deferReply();
  }

    const context = {
    userId: interaction.user.id,
    channelId: interaction.channelId,
    messageId: interaction.id,
    guildId: interaction.guildId ?? undefined,
    // Discord itself enforces required options; an optional one left empty reads as "", as
    // in the Carbon runtime, instead of throwing.
    getString: (name: string) => interaction.options.getString(name) ?? "",
    getNumber: (name: string) => interaction.options.getNumber(name) ?? undefined,
    reply: async (message: DiscordReply, opts?: { ephemeral?: boolean }) => {
      params.log?.(
        "[dokja-discord] slash reply",
        JSON.stringify({
          name: definition.name,
          ephemeral: opts?.ephemeral,
          hasComponents: typeof message !== "string" && Boolean(message.components?.length),
        }),
      );
      await sendCommandReply(interaction, message, opts, buttons, selects);
    },
    updateReply: async (message: DiscordReply) => {
      await editCommandReply(interaction, message, buttons, selects);
    },
    deleteReply: async () => {
      await interaction.deleteReply();
    },
    showModal: async (modal: DiscordModalAction["fields"] extends never ? never : any) => {
      params.log?.(
        "[dokja-discord] slash modal",
        JSON.stringify({ name: definition.name, customId: modal.customId }),
      );
      await interaction.showModal(buildModal(modal));
    },
  };

  await definition.handle(context);
  params.log?.("[dokja-discord] slash command completed", JSON.stringify({ name: definition.name }));
}

async function handleButton(
  definition: DiscordButtonAction,
  interaction: ButtonInteraction,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
  modals: DiscordModalAction[],
  params: StartDiscordJsRuntimeParams,
) {
  params.log?.(
    "[dokja-discord] button interaction",
    JSON.stringify({
      customId: definition.customId,
      userId: interaction.user.id,
      channelId: interaction.channelId,
    }),
  );

  if (definition.defer) {
    await interaction.deferUpdate();
  }

  const context: DiscordButtonContext = {
    customId: definition.customId,
    userId: interaction.user.id,
    channelId: interaction.channelId,
    messageId: interaction.message.id,
    guildId: interaction.guildId ?? undefined,
    reply: async (message) => {
      await sendComponentReply(interaction, message, buttons, selects);
    },
    update: async (message) => {
      await sendComponentUpdate(interaction, message, buttons, selects);
    },
    showModal: async (modal) => {
      await interaction.showModal(buildModal(modal));
    },
  };

  await definition.handle(context);
  params.log?.("[dokja-discord] button interaction completed", JSON.stringify({ customId: definition.customId }));
}

async function handleSelect(
  definition: DiscordSelectAction,
  interaction: StringSelectMenuInteraction,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
  _modals: DiscordModalAction[],
  params: StartDiscordJsRuntimeParams,
) {
  params.log?.(
    "[dokja-discord] select interaction",
    JSON.stringify({
      customId: definition.customId,
      userId: interaction.user.id,
      channelId: interaction.channelId,
      values: interaction.values,
    }),
  );

  if (definition.defer) {
    await interaction.deferUpdate();
  }

  const context: DiscordSelectContext = {
    customId: definition.customId,
    values: interaction.values,
    userId: interaction.user.id,
    channelId: interaction.channelId,
    messageId: interaction.message.id,
    guildId: interaction.guildId ?? undefined,
    reply: async (message) => {
      await sendComponentReply(interaction, message, buttons, selects);
    },
    update: async (message) => {
      await sendComponentUpdate(interaction, message, buttons, selects);
    },
  };

  await definition.handle(context);
  params.log?.("[dokja-discord] select interaction completed", JSON.stringify({ customId: definition.customId }));
}

async function handleModal(
  definition: DiscordModalAction,
  interaction: ModalSubmitInteraction,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
  _modals: DiscordModalAction[],
  params: StartDiscordJsRuntimeParams,
) {
  params.log?.(
    "[dokja-discord] modal submit",
    JSON.stringify({
      customId: definition.customId,
      userId: interaction.user.id,
      channelId: interaction.channelId,
    }),
  );

  if (definition.defer) {
    await interaction.deferReply();
  }

  const context: DiscordModalContext = {
    customId: definition.customId,
    userId: interaction.user.id,
    channelId: interaction.channelId,
    messageId: interaction.id,
    guildId: interaction.guildId ?? undefined,
    getText: (name: string) => interaction.fields.getTextInputValue(name),
    reply: async (message, opts) => {
      await sendModalReply(interaction, message, opts, buttons, selects);
    },
  };

  await definition.handle(context);
  params.log?.("[dokja-discord] modal submit completed", JSON.stringify({ customId: definition.customId }));
}

export async function toMessageContext(message: Message, botUserId?: string): Promise<DiscordMessageContext> {
  let replyToContent: string | undefined;
  if (!message.author.bot && message.reference?.messageId
    && message.reference.channelId === message.channelId) {
    try {
      const referenced = await message.fetchReference();
      if (referenced.channelId === message.channelId && referenced.guildId === message.guildId) {
        replyToContent = referenced.content?.slice(0, 8000) || undefined;
      }
    } catch {
      // Deleted or inaccessible references must not prevent the new message being handled.
    }
  }
  return {
    userId: message.author.id,
    userName: message.member?.displayName ?? message.author.globalName ?? message.author.username,
    channelId: message.channelId,
    messageId: message.id,
    guildId: message.guildId ?? undefined,
    content: message.content?.trim() ?? "",
    documentAttachments: pdfAttachments(message.attachments?.values() ?? []),
    replyToContent,
    mentionedUserIds: Array.from(message.mentions.users.values(), user => user.id).slice(0, 20),
    mentionedUsers: Array.from(message.mentions.users.values(), user => ({
      id: user.id,
      name: (message.mentions.members?.get(user.id)?.displayName ?? user.globalName ?? user.username ?? "").slice(0, 100),
    })).slice(0, 20),
    isBot: Boolean(message.author.bot),
    isDirectMessage: message.channel.type === ChannelType.DM,
    mentionsBot: botUserId ? message.mentions.users.has(botUserId) : true,
    reply: async (content) => {
      await sendMessageReply(message, content);
    },
  };
}

const DISCORD_MESSAGE_LIMIT = 2000;

export function splitDiscordReply(message: DiscordReply): DiscordReply[] {
  const base = typeof message === "string" ? { content: message } : message;
  const parts = splitDiscordContent(base.content);
  if (parts.length <= 1) {
    return [message];
  }
  return parts.map((content, index) => ({
    content,
    ephemeral: base.ephemeral,
    components: index === 0 ? base.components : undefined,
    attachments: index === 0 ? base.attachments : undefined,
    embeds: index === 0 ? base.embeds : undefined,
  }));
}

function splitDiscordContent(content: string): string[] {
  const trimmed = content ?? "";
  if (trimmed.length <= DISCORD_MESSAGE_LIMIT) {
    return [trimmed];
  }

  const chunks: string[] = [];
  let chunk = "";
  let fence: { marker: string; opening: string } | undefined;
  let hasContent = false;

  const flush = () => {
    // These newlines belong to the wrapper, not the original source code.
    chunks.push(chunk + (fence ? `\n${fence.marker}` : ""));
    chunk = fence ? `${fence.opening}\n` : "";
    hasContent = false;
  };

  for (const line of trimmed.match(/[^\n]*\n|[^\n]+$/g) ?? []) {
    const match = line.match(/^ {0,3}(`{3,}|~{3,})([^\r\n]*)\r?\n?$/);
    let nextFence = fence;
    if (match) {
      if (!fence && line.length + match[1].length + 3 < DISCORD_MESSAGE_LIMIT / 2
        && !match[2].includes(match[1][0])) {
        nextFence = { marker: match[1], opening: line.replace(/\r?\n$/, "") };
      } else if (fence && match[1][0] === fence.marker[0]
        && match[1].length >= fence.marker.length && !match[2].trim()) {
        nextFence = undefined;
      }
    }

    // Prefer whole lines so indentation and blank lines survive unchanged.
    const reserve = Math.max(fence?.marker.length ?? 0, nextFence?.marker.length ?? 0) + 1;
    if (hasContent && chunk.length + line.length + reserve > DISCORD_MESSAGE_LIMIT) {
      flush();
    }
    let remaining = line;
    while (remaining.length > 0) {
      const capacity = DISCORD_MESSAGE_LIMIT - chunk.length - reserve;
      let length = Math.min(remaining.length, capacity);
      // Keep UTF-16 surrogate pairs together when a very long line must split.
      if (length < remaining.length && /[\uD800-\uDBFF]/.test(remaining[length - 1])) {
        length--;
      }
      chunk += remaining.slice(0, length);
      remaining = remaining.slice(length);
      hasContent = true;
      if (remaining.length > 0) flush();
    }
    fence = nextFence;
  }
  if (hasContent) chunks.push(chunk + (fence ? `\n${fence.marker}` : ""));
  return chunks;
}

async function sendMessageReply(message: Message, reply: DiscordReply) {
  const parts = splitDiscordReply(reply);
  await message.reply(toMessageReply(parts[0]));
  for (const part of parts.slice(1)) {
    await message.channel.send(toMessageReply(part));
  }
}

async function sendCommandReply(
  interaction: ChatInputCommandInteraction,
  message: DiscordReply,
  opts: { ephemeral?: boolean } | undefined,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
) {
  const parts = splitDiscordReply(applyReplyOptions(message, opts));
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply(toInteractionEditReply(parts[0], buttons, selects));
  } else {
    await interaction.reply(toInteractionReply(parts[0], opts, buttons, selects));
  }
  for (const part of parts.slice(1)) {
    await interaction.followUp(toInteractionReply(part, opts, buttons, selects));
  }
}

async function editCommandReply(
  interaction: ChatInputCommandInteraction,
  message: DiscordReply,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
) {
  const parts = splitDiscordReply(message);
  await interaction.editReply(toInteractionEditReply(parts[0], buttons, selects));
  for (const part of parts.slice(1)) {
    await interaction.followUp(toInteractionReply(part, undefined, buttons, selects));
  }
}

async function sendComponentReply(
  interaction: ButtonInteraction | StringSelectMenuInteraction,
  message: DiscordReply,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
) {
  const parts = splitDiscordReply(message);
  if (interaction.deferred || interaction.replied) {
    await interaction.followUp(toInteractionReply(parts[0], undefined, buttons, selects));
  } else {
    await interaction.reply(toInteractionReply(parts[0], undefined, buttons, selects));
  }
  for (const part of parts.slice(1)) {
    await interaction.followUp(toInteractionReply(part, undefined, buttons, selects));
  }
}

async function sendComponentUpdate(
  interaction: ButtonInteraction | StringSelectMenuInteraction,
  message: DiscordReply,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
) {
  const parts = splitDiscordReply(message);
  if (interaction.deferred) {
    await interaction.editReply(toInteractionEditReply(parts[0], buttons, selects));
  } else {
    await interaction.update(toInteractionEditReply(parts[0], buttons, selects));
  }
  for (const part of parts.slice(1)) {
    await interaction.followUp(toInteractionReply(part, undefined, buttons, selects));
  }
}

async function sendModalReply(
  interaction: ModalSubmitInteraction,
  message: DiscordReply,
  opts: { ephemeral?: boolean } | undefined,
  buttons: DiscordButtonAction[],
  selects: DiscordSelectAction[],
) {
  const parts = splitDiscordReply(applyReplyOptions(message, opts));
  if (interaction.deferred || interaction.replied) {
    await interaction.editReply(toInteractionEditReply(parts[0], buttons, selects));
  } else {
    await interaction.reply(toInteractionReply(parts[0], opts, buttons, selects));
  }
  for (const part of parts.slice(1)) {
    await interaction.followUp(toInteractionReply(part, opts, buttons, selects));
  }
}

function applyReplyOptions(
  message: DiscordReply,
  opts?: { ephemeral?: boolean },
): DiscordReply {
  if (!opts?.ephemeral) {
    return message;
  }
  if (typeof message === "string") {
    return { content: message, ephemeral: true };
  }
  return { ...message, ephemeral: message.ephemeral ?? true };
}

export function toMessageReply(message: DiscordReply) {
  if (typeof message === "string") {
    return { content: message };
  }
  return {
    content: message.content,
    components: buildRows(message.components ?? []),
    files: attachmentFiles(message),
    ...(message.embeds ? { embeds: message.embeds.map(toEmbed) } : {}),
  };
}

// An embed in Discord's own shape; a picture only from an http(s) address. Text is cut to
// Discord's limits: 25 fields, 256 characters per field name, 1024 per value, 2048 for the footer.
export function toEmbed(embed: DiscordEmbedSpec) {
  const fields = (embed.fields ?? [])
    .filter((field) => field.name.trim() && field.value.trim())
    .slice(0, 25)
    .map((field) => ({ name: field.name.slice(0, 256), value: field.value.slice(0, 1024), inline: field.inline === true }));
  return {
    ...(embed.title ? { title: embed.title.slice(0, 256) } : {}),
    ...(embed.description ? { description: embed.description.slice(0, 4096) } : {}),
    ...(embed.thumbnailUrl && /^https?:\/\//.test(embed.thumbnailUrl) ? { thumbnail: { url: embed.thumbnailUrl } } : {}),
    ...(Number.isInteger(embed.color) && embed.color! >= 0 && embed.color! <= 0xffffff ? { color: embed.color } : {}),
    ...(fields.length ? { fields } : {}),
    ...(embed.footer ? { footer: { text: embed.footer.slice(0, 2048) } } : {}),
    ...(embed.url && /^https?:\/\//.test(embed.url) ? { url: embed.url } : {}),
  };
}

export function toInteractionReply(
  message: DiscordReply,
  opts?: { ephemeral?: boolean },
  buttons: DiscordButtonAction[] = [],
  selects: DiscordSelectAction[] = [],
): InteractionReplyOptions {
  if (typeof message === "string") {
    return {
      content: message,
      flags: opts?.ephemeral ? MessageFlags.Ephemeral : undefined,
    };
  }

  return {
    content: message.content,
    components: buildRows(message.components ?? [], buttons, selects),
    flags: message.ephemeral || opts?.ephemeral ? MessageFlags.Ephemeral : undefined,
    files: attachmentFiles(message),
    ...(message.embeds ? { embeds: message.embeds.map(toEmbed) } : {}),
  };
}

export function toInteractionEditReply(
  message: DiscordReply,
  buttons: DiscordButtonAction[] = [],
  selects: DiscordSelectAction[] = [],
): InteractionEditReplyOptions {
  if (typeof message === "string") {
    return { content: message };
  }

  return {
    content: message.content,
    components: buildRows(message.components ?? [], buttons, selects),
    files: attachmentFiles(message),
    ...(message.embeds ? { embeds: message.embeds.map(toEmbed) } : {}),
  };
}

function attachmentFiles(message: Exclude<DiscordReply, string>) {
  return message.attachments?.map(file => ({
    name: file.filename,
    attachment: Buffer.from(file.data_b64, "base64"),
  }));
}

function buildRows(
  components: NonNullable<Exclude<DiscordReply, string>["components"]>,
  buttons: DiscordButtonAction[] = [],
  selects: DiscordSelectAction[] = [],
) {
  const rows: Array<ActionRowBuilder<ButtonBuilder | StringSelectMenuBuilder>> = [];
  const buttonSpecs = components.filter((entry) => entry.kind === "button");
  if (buttonSpecs.length > 0) {
    rows.push(
      new ActionRowBuilder<ButtonBuilder>().addComponents(
        buttonSpecs.map((entry) => {
          const registered = buttons.find((button) => button.customId === entry.customId);
          return new ButtonBuilder()
            .setCustomId(entry.customId)
            .setLabel(entry.label ?? registered?.label ?? entry.customId)
            .setStyle(toButtonStyle(entry.style ?? registered?.style));
        }),
      ),
    );
  }
  for (const component of components.filter((entry) => entry.kind === "select")) {
    const registered = selects.find((entry) => entry.customId === component.customId);
    rows.push(
      new ActionRowBuilder<StringSelectMenuBuilder>().addComponents(
        new StringSelectMenuBuilder()
          .setCustomId(component.customId)
          .setPlaceholder(component.placeholder ?? registered?.placeholder ?? "Choose")
          .addOptions(component.options.map((option) => ({
            label: option.label,
            value: option.value,
            description: option.description,
          }))),
      ),
    );
  }
  return rows;
}

function buildModal(modal: { customId: string; title: string; fields: Array<{ customId: string; label: string; placeholder?: string; required?: boolean; style?: "short" | "paragraph" }> }) {
  return new ModalBuilder()
    .setCustomId(modal.customId)
    .setTitle(modal.title)
    .addComponents(
      modal.fields.map((field) =>
        new ActionRowBuilder<TextInputBuilder>().addComponents(
          new TextInputBuilder()
            .setCustomId(field.customId)
            .setLabel(field.label)
            .setPlaceholder(field.placeholder ?? "")
            .setRequired(field.required ?? true)
            .setStyle(field.style === "paragraph" ? TextInputStyle.Paragraph : TextInputStyle.Short),
        ),
      ),
    );
}

function toButtonStyle(style?: "primary" | "secondary" | "success" | "danger") {
  switch (style) {
    case "secondary":
      return ButtonStyle.Secondary;
    case "success":
      return ButtonStyle.Success;
    case "danger":
      return ButtonStyle.Danger;
    default:
      return ButtonStyle.Primary;
  }
}
