import {
  Button,
  ButtonStyle,
  Client,
  Command,
  Label,
  MessageCreateListener,
  Modal,
  Row,
  StringSelectMenu,
  TextInput,
  TextInputStyle,
} from "@buape/carbon";
import { GatewayPlugin, GatewayIntents } from "@buape/carbon/gateway";

import { getDiscordGatewayEmitter, waitForDiscordGatewayStop } from "./gateway.js";
import { probeDiscordApplication } from "./probe.js";
import type {
  DiscordButtonAction,
  DiscordButtonContext,
  DiscordCommandContext,
  DiscordComponentSpec,
  DiscordModalAction,
  DiscordModalContext,
  DiscordMessageContext,
  DiscordMessageHandler,
  DiscordReply,
  DiscordSelectAction,
  DiscordSelectContext,
  DiscordSlashCommand,
} from "../types.js";

type StartDiscordRuntimeParams = {
  token: string;
  applicationId?: string;
  devGuildId?: string;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
  commands: DiscordSlashCommand[];
  onMessage: DiscordMessageHandler;
  buttons?: DiscordButtonAction[];
  selects?: DiscordSelectAction[];
  modals?: DiscordModalAction[];
  onReady?: (client: Client, meta: { botUserId?: string }) => Promise<void> | void;
};

type StartDiscordRuntimeDeps = {
  fetchApplicationId?: typeof probeDiscordApplication;
  createClient?: (params: {
    applicationId: string;
    token: string;
    devGuildId?: string;
    commands: DiscordSlashCommand[];
    buttons?: DiscordButtonAction[];
    selects?: DiscordSelectAction[];
    modals?: DiscordModalAction[];
  }) => Client;
  getGatewayEmitter?: typeof getDiscordGatewayEmitter;
  waitForGatewayStop?: typeof waitForDiscordGatewayStop;
};

function gatewayIntents(): number {
  return (
    GatewayIntents.Guilds |
    GatewayIntents.GuildMessages |
    GatewayIntents.GuildVoiceStates |
    GatewayIntents.MessageContent |
    GatewayIntents.DirectMessages
  );
}

function commandFromDefinition(
  definition: DiscordSlashCommand,
  buttonDefinitions: DiscordButtonAction[],
  selectDefinitions: DiscordSelectAction[],
  modalDefinitions: DiscordModalAction[],
) {
  return new (class extends Command {
    name = definition.name;
    description = definition.description;
    defer = definition.defer ?? true;
    ephemeral = false;
    options = definition.options;

    async run(interaction: any) {
      console.log(
        "[dokja-discord] slash command",
        JSON.stringify({
          name: definition.name,
          userId: String(interaction.user?.id ?? ""),
          channelId: String(interaction.channel?.id ?? ""),
          guildId: interaction.guild?.id ? String(interaction.guild.id) : undefined,
        }),
      );
      const context: DiscordCommandContext = {
        userId: String(interaction.user?.id ?? ""),
        channelId: String(interaction.channel?.id ?? ""),
        messageId: String(interaction.rawData?.id ?? interaction.id ?? ""),
        guildId: interaction.guild?.id ? String(interaction.guild.id) : undefined,
        getString: (name: string) => interaction.options.getString(name) ?? "",
        getNumber: (name: string) => interaction.options.getNumber(name) ?? undefined,
        reply: async (message: DiscordReply, opts?: { ephemeral?: boolean }) => {
          console.log(
            "[dokja-discord] slash reply",
            JSON.stringify({
              name: definition.name,
              ephemeral: opts?.ephemeral,
              hasComponents: typeof message !== "string" && Boolean(message.components?.length),
            }),
          );
          await interaction.reply(
            toMessagePayload(message, opts, buttonDefinitions, selectDefinitions, modalDefinitions),
          );
        },
        showModal: async (modal) => {
          console.log(
            "[dokja-discord] slash modal",
            JSON.stringify({
              name: definition.name,
              customId: modal.customId,
            }),
          );
          await interaction.showModal(modalFromDefinition(modal, modalDefinitions));
        },
      };
      try {
        await definition.handle(context);
        console.log("[dokja-discord] slash command completed", JSON.stringify({ name: definition.name }));
      } catch (error) {
        console.error(
          "[dokja-discord] slash command failed",
          JSON.stringify({
            name: definition.name,
            error: error instanceof Error ? error.message : String(error),
          }),
        );
        throw error;
      }
    }
  })();
}

class DokjaCarbonMessageListener extends MessageCreateListener {
  constructor(
    private readonly botUserId: string | undefined,
    private readonly handler: DiscordMessageHandler,
  ) {
    super();
  }

  async handle(data: Parameters<MessageCreateListener["handle"]>[0], _client: Client) {
    const message = data.message;
    const author = data.author ?? message.author;
    if (!message || !author) {
      return;
    }

    const context: DiscordMessageContext = {
      userId: author.id,
      channelId: message.channelId,
      messageId: message.id,
      guildId: data.guild?.id,
      content: message.content?.trim() ?? "",
      isBot: Boolean(author.bot),
      isDirectMessage: !message.rawData.guild_id,
      mentionsBot: this.botUserId
        ? message.mentionedUsers.some((user) => user.id === this.botUserId)
        : true,
      reply: async (content: DiscordReply) => {
        await message.reply(toMessagePayload(content));
      },
    };

    await this.handler(context);
  }
}

function toMessagePayload(
  message: DiscordReply,
  opts?: { ephemeral?: boolean },
  buttonDefinitions: DiscordButtonAction[] = [],
  selectDefinitions: DiscordSelectAction[] = [],
  modalDefinitions: DiscordModalAction[] = [],
) {
  if (typeof message === "string") {
    return {
      content: message,
      ...(opts?.ephemeral !== undefined ? { ephemeral: opts.ephemeral } : {}),
    };
  }

  return {
    content: message.content,
    components: message.components?.length
      ? buildComponentRows(message.components, buttonDefinitions, selectDefinitions, modalDefinitions)
      : undefined,
    ...(message.ephemeral !== undefined ? { ephemeral: message.ephemeral } : {}),
    ...(opts?.ephemeral !== undefined ? { ephemeral: opts.ephemeral } : {}),
  };
}

function buildComponentRows(
  components: DiscordComponentSpec[],
  buttonDefinitions: DiscordButtonAction[],
  selectDefinitions: DiscordSelectAction[],
  modalDefinitions: DiscordModalAction[],
) {
  const rows: Row<any>[] = [];
  const buttons = components.filter((component) => component.kind === "button");
  if (buttons.length > 0) {
    rows.push(
      new Row(
        buttons.map((component) =>
          componentFromSpec(component, buttonDefinitions, selectDefinitions, modalDefinitions),
        ),
      ),
    );
  }
  const selects = components.filter((component) => component.kind === "select");
  for (const select of selects) {
    rows.push(
      new Row([
        componentFromSpec(select, buttonDefinitions, selectDefinitions, modalDefinitions),
      ]),
    );
  }
  return rows;
}

function componentFromSpec(
  spec: DiscordComponentSpec,
  buttonDefinitions: DiscordButtonAction[],
  selectDefinitions: DiscordSelectAction[],
  modalDefinitions: DiscordModalAction[],
) {
  if (spec.kind === "button") {
    const registered = buttonDefinitions.find((definition) => definition.customId === spec.customId);
    if (registered) {
      return buttonFromDefinition(
        {
          ...registered,
          label: spec.label,
          style: spec.style ?? registered.style,
        },
        buttonDefinitions,
        selectDefinitions,
        modalDefinitions,
      );
    }
    return buttonFromSpec(spec);
  }
  const registered = selectDefinitions.find((definition) => definition.customId === spec.customId);
  if (registered) {
    return selectFromDefinition(
      {
        ...registered,
        placeholder: spec.placeholder ?? registered.placeholder,
        options: spec.options,
      },
      buttonDefinitions,
      selectDefinitions,
      modalDefinitions,
    );
  }
  return selectFromSpec(spec);
}

function buttonFromSpec(spec: DiscordComponentSpec) {
  if (spec.kind !== "button") {
    throw new Error("invalid button spec");
  }

  return new (class extends Button {
    customId = spec.customId;
    label = spec.label;
    style =
      spec.style === "success"
        ? ButtonStyle.Success
        : spec.style === "danger"
          ? ButtonStyle.Danger
          : spec.style === "secondary"
            ? ButtonStyle.Secondary
            : ButtonStyle.Primary;
  })();
}

function selectFromSpec(spec: DiscordComponentSpec) {
  if (spec.kind !== "select") {
    throw new Error("invalid select spec");
  }

  return new (class extends StringSelectMenu {
    customId = spec.customId;
    placeholder = spec.placeholder;
    options = spec.options;
  })();
}

function modalFromDefinition(
  definition: { customId: string; title: string; fields: Array<{ customId: string; label: string; placeholder?: string; required?: boolean; style?: "short" | "paragraph" }> },
  modalDefinitions: DiscordModalAction[],
) {
  return new (class extends Modal {
    title = definition.title;
    customId = definition.customId;
    components = definition.fields.map((field) => new (class extends Label {
      label = field.label;
      constructor() {
        super(
          new (class extends TextInput {
            customId = field.customId;
            placeholder = field.placeholder;
            required = field.required;
            style = field.style === "paragraph" ? TextInputStyle.Paragraph : TextInputStyle.Short;
          })(),
        );
      }
    })());

    async run(interaction: any) {
      const handler = modalDefinitions.find((entry) => entry.customId === definition.customId);
      if (!handler) {
        console.error(
          "[dokja-discord] modal handler missing",
          JSON.stringify({ customId: definition.customId }),
        );
        return interaction.reply({ content: "Modal handler not found.", ephemeral: true });
      }

      console.log(
        "[dokja-discord] modal submit",
        JSON.stringify({
          customId: definition.customId,
          userId: String(interaction.user?.id ?? ""),
          channelId: String(interaction.channel?.id ?? ""),
        }),
      );

      if (handler.defer) {
        console.log(
          "[dokja-discord] modal defer",
          JSON.stringify({ customId: definition.customId }),
        );
        await interaction.defer();
      }

      const context: DiscordModalContext = {
        customId: definition.customId,
        userId: String(interaction.user?.id ?? ""),
        channelId: String(interaction.channel?.id ?? ""),
        messageId: String(interaction.rawData?.id ?? interaction.id ?? ""),
        guildId: interaction.guild?.id ? String(interaction.guild.id) : undefined,
        getText: (name: string) => interaction.fields.getText(name, true),
        reply: async (message: DiscordReply, opts?: { ephemeral?: boolean }) => {
          console.log(
            "[dokja-discord] modal reply",
            JSON.stringify({
              customId: definition.customId,
              ephemeral: opts?.ephemeral,
            }),
          );
          await interaction.reply(toMessagePayload(message, opts));
        },
      };
      try {
        await handler.handle(context);
        console.log("[dokja-discord] modal submit completed", JSON.stringify({ customId: definition.customId }));
      } catch (error) {
        console.error(
          "[dokja-discord] modal submit failed",
          JSON.stringify({
            customId: definition.customId,
            error: error instanceof Error ? error.message : String(error),
          }),
        );
        throw error;
      }
    }
  })();
}

function buttonFromDefinition(
  definition: DiscordButtonAction,
  buttonDefinitions: DiscordButtonAction[],
  selectDefinitions: DiscordSelectAction[],
  modalDefinitions: DiscordModalAction[],
) {
  return new (class extends Button {
    customId = definition.customId;
    label = definition.label;
    defer = definition.defer ?? false;
    style =
      definition.style === "success"
        ? ButtonStyle.Success
        : definition.style === "danger"
          ? ButtonStyle.Danger
          : definition.style === "secondary"
            ? ButtonStyle.Secondary
            : ButtonStyle.Primary;

    async run(interaction: any) {
      console.log(
        "[dokja-discord] button interaction",
        JSON.stringify({
          customId: definition.customId,
          userId: String(interaction.user?.id ?? ""),
          channelId: String(interaction.channel?.id ?? ""),
        }),
      );
      const context: DiscordButtonContext = {
        customId: definition.customId,
        userId: String(interaction.user?.id ?? ""),
        channelId: String(interaction.channel?.id ?? ""),
        messageId: String(interaction.rawData?.id ?? interaction.id ?? ""),
        guildId: interaction.guild?.id ? String(interaction.guild.id) : undefined,
        reply: async (message: DiscordReply) => {
          console.log("[dokja-discord] button reply", JSON.stringify({ customId: definition.customId }));
          await interaction.reply(
            toMessagePayload(message, undefined, buttonDefinitions, selectDefinitions, modalDefinitions),
          );
        },
        update: async (message: DiscordReply) => {
          console.log("[dokja-discord] button update", JSON.stringify({ customId: definition.customId }));
          const payload = toMessagePayload(
            message,
            undefined,
            buttonDefinitions,
            selectDefinitions,
            modalDefinitions,
          );
          if (interaction._deferred) {
            await interaction.reply(payload);
            return;
          }
          await interaction.update(payload);
        },
        showModal: async (modal) => {
          console.log(
            "[dokja-discord] button modal",
            JSON.stringify({ customId: definition.customId, modalCustomId: modal.customId }),
          );
          await interaction.showModal(modalFromDefinition(modal, modalDefinitions));
        },
      };
      try {
        await definition.handle(context);
        console.log("[dokja-discord] button interaction completed", JSON.stringify({ customId: definition.customId }));
      } catch (error) {
        console.error(
          "[dokja-discord] button interaction failed",
          JSON.stringify({
            customId: definition.customId,
            error: error instanceof Error ? error.message : String(error),
          }),
        );
        throw error;
      }
    }
  })();
}

function selectFromDefinition(
  definition: DiscordSelectAction,
  buttonDefinitions: DiscordButtonAction[],
  selectDefinitions: DiscordSelectAction[],
  modalDefinitions: DiscordModalAction[],
) {
  return new (class extends StringSelectMenu {
    customId = definition.customId;
    placeholder = definition.placeholder;
    defer = definition.defer ?? false;
    options = definition.options;

    async run(interaction: any) {
      console.log(
        "[dokja-discord] select interaction",
        JSON.stringify({
          customId: definition.customId,
          userId: String(interaction.user?.id ?? ""),
          channelId: String(interaction.channel?.id ?? ""),
          values: interaction.values ?? [],
        }),
      );
      const context: DiscordSelectContext = {
        customId: definition.customId,
        values: interaction.values ?? [],
        userId: String(interaction.user?.id ?? ""),
        channelId: String(interaction.channel?.id ?? ""),
        messageId: String(interaction.rawData?.id ?? interaction.id ?? ""),
        guildId: interaction.guild?.id ? String(interaction.guild.id) : undefined,
        reply: async (message: DiscordReply) => {
          console.log("[dokja-discord] select reply", JSON.stringify({ customId: definition.customId }));
          await interaction.reply(
            toMessagePayload(message, undefined, buttonDefinitions, selectDefinitions, modalDefinitions),
          );
        },
        update: async (message: DiscordReply) => {
          console.log("[dokja-discord] select update", JSON.stringify({ customId: definition.customId }));
          const payload = toMessagePayload(
            message,
            undefined,
            buttonDefinitions,
            selectDefinitions,
            modalDefinitions,
          );
          if (interaction._deferred) {
            await interaction.reply(payload);
            return;
          }
          await interaction.update(payload);
        },
      };
      try {
        await definition.handle(context);
        console.log("[dokja-discord] select interaction completed", JSON.stringify({ customId: definition.customId }));
      } catch (error) {
        console.error(
          "[dokja-discord] select interaction failed",
          JSON.stringify({
            customId: definition.customId,
            error: error instanceof Error ? error.message : String(error),
          }),
        );
        throw error;
      }
    }
  })();
}

export async function startDiscordRuntime(
  params: StartDiscordRuntimeParams,
  deps: StartDiscordRuntimeDeps = {},
): Promise<void> {
  const fetchApplicationId = deps.fetchApplicationId ?? probeDiscordApplication;
  const createClient = deps.createClient ?? defaultCreateClient;
  const getGatewayEmitter = deps.getGatewayEmitter ?? getDiscordGatewayEmitter;
  const waitForGatewayStop = deps.waitForGatewayStop ?? waitForDiscordGatewayStop;

  const applicationId =
    params.applicationId ||
    (await fetchApplicationId(params.token, 4000)).id;
  if (!applicationId) {
    const probe = await fetchApplicationId(params.token, 4000);
    const details = [probe.error, probe.status ? `status=${probe.status}` : "", probe.body]
      .filter(Boolean)
      .join(" ");
    throw new Error(
      details
        ? `Failed to resolve Discord application id: ${details}`
        : "Failed to resolve Discord application id",
    );
  }

  const client = createClient({
    applicationId,
    token: params.token,
    devGuildId: params.devGuildId,
    commands: params.commands,
    buttons: params.buttons,
    selects: params.selects,
    modals: params.modals,
  });

  const originalHandleEvent = client.eventHandler.handleEvent.bind(client.eventHandler);
  client.eventHandler.handleEvent = ((payload: unknown, type: string) => {
    if (type === "VOICE_STATE_UPDATE" || type === "VOICE_SERVER_UPDATE") {
      params.log?.(
        "[dokja-discord] raw gateway dispatch",
        JSON.stringify({
          type,
          payload,
        }),
      );
    }
    return originalHandleEvent(payload as never, type as never);
  }) as typeof client.eventHandler.handleEvent;

  await client.handleDeployRequest();
  const botUser = await client.fetchUser("@me");
  client.listeners.push(
    new DokjaCarbonMessageListener(botUser?.id ? String(botUser.id) : undefined, params.onMessage),
  );
  await params.onReady?.(client, { botUserId: botUser?.id ? String(botUser.id) : undefined });

  const abortController = new AbortController();
  const stop = () => abortController.abort();
  process.on("SIGINT", stop);
  process.on("SIGTERM", stop);

  const gateway = client.getPlugin("gateway");
  const gatewayEmitter = getGatewayEmitter(gateway);
  await waitForGatewayStop({
    gateway: gateway
      ? {
          emitter: gatewayEmitter,
          disconnect: () => gateway.disconnect(),
        }
      : undefined,
    abortSignal: abortController.signal,
    onGatewayError: (error) => {
      params.error?.("[dokja-discord] gateway error", error);
    },
    shouldStopOnError: (error) => {
      const message = String(error);
      return message.includes("Max reconnect attempts") || message.includes("Fatal Gateway error");
    },
  });
}

function defaultCreateClient(params: {
  applicationId: string;
  token: string;
  devGuildId?: string;
  commands: DiscordSlashCommand[];
  buttons?: DiscordButtonAction[];
  selects?: DiscordSelectAction[];
  modals?: DiscordModalAction[];
}): Client {
  return new Client(
    {
      baseUrl: "http://localhost",
      deploySecret: "a",
      clientId: params.applicationId,
      publicKey: "a",
      token: params.token,
      autoDeploy: false,
      devGuilds: params.devGuildId ? [params.devGuildId] : undefined,
    },
    {
      commands: params.commands.map((definition) =>
        commandFromDefinition(
          definition,
          params.buttons ?? [],
          params.selects ?? [],
          params.modals ?? [],
        ),
      ),
      listeners: [],
      components: [
        ...(params.buttons ?? []).map((definition) =>
          buttonFromDefinition(
            definition,
            params.buttons ?? [],
            params.selects ?? [],
            params.modals ?? [],
          ),
        ),
        ...(params.selects ?? []).map((definition) =>
          selectFromDefinition(
            definition,
            params.buttons ?? [],
            params.selects ?? [],
            params.modals ?? [],
          ),
        ),
      ],
      modals: (params.modals ?? []).map((definition) => modalFromDefinition(definition, params.modals ?? [])),
    },
    [
      new GatewayPlugin({
        reconnect: { maxAttempts: 50 },
        intents: gatewayIntents(),
        autoInteractions: true,
      }),
    ],
  );
}
