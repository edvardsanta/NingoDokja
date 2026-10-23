export type DiscordButtonSpec = {
  kind: "button";
  customId: string;
  label: string;
  style?: "primary" | "secondary" | "success" | "danger";
};

export type DiscordSelectSpec = {
  kind: "select";
  customId: string;
  placeholder?: string;
  options: Array<{
    label: string;
    value: string;
    description?: string;
  }>;
};

export type DiscordComponentSpec = DiscordButtonSpec | DiscordSelectSpec;

export type DiscordReply =
  | string
  | {
      content: string;
      ephemeral?: boolean;
      components?: DiscordComponentSpec[];
    };

export type DiscordModalSpec = {
  customId: string;
  title: string;
  fields: Array<{
    customId: string;
    label: string;
    placeholder?: string;
    required?: boolean;
    style?: "short" | "paragraph";
  }>;
};

export type DiscordCommandContext = {
  userId: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  getString(name: string): string;
  getNumber(name: string): number | undefined;
  reply(message: DiscordReply, opts?: { ephemeral?: boolean }): Promise<void>;
  updateReply(message: DiscordReply): Promise<void>;
  showModal(modal: DiscordModalSpec): Promise<void>;
};

export type DiscordMessageContext = {
  userId: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  content: string;
  isBot: boolean;
  isDirectMessage: boolean;
  mentionsBot: boolean;
  reply(message: DiscordReply): Promise<void>;
};

export type DiscordSlashCommand = {
  name: string;
  description: string;
  options?: any[];
  defer?: boolean;
  handle(context: DiscordCommandContext): Promise<void>;
};

export type DiscordMessageHandler = (context: DiscordMessageContext) => Promise<void>;

export type DiscordButtonContext = {
  customId: string;
  userId: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  reply(message: DiscordReply): Promise<void>;
  update(message: DiscordReply): Promise<void>;
  showModal(modal: DiscordModalSpec): Promise<void>;
};

export type DiscordSelectContext = {
  customId: string;
  values: string[];
  userId: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  reply(message: DiscordReply): Promise<void>;
  update(message: DiscordReply): Promise<void>;
};

export type DiscordModalContext = {
  customId: string;
  userId: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  getText(name: string): string;
  reply(message: DiscordReply, opts?: { ephemeral?: boolean }): Promise<void>;
};

export type DiscordButtonAction = {
  customId: string;
  label: string;
  style?: "primary" | "secondary" | "success" | "danger";
  defer?: boolean;
  handle(context: DiscordButtonContext): Promise<void>;
};

export type DiscordSelectAction = {
  customId: string;
  placeholder?: string;
  defer?: boolean;
  options: Array<{
    label: string;
    value: string;
    description?: string;
  }>;
  handle(context: DiscordSelectContext): Promise<void>;
};

export type DiscordModalAction = {
  customId: string;
  title: string;
  fields: DiscordModalSpec["fields"];
  defer?: boolean;
  handle(context: DiscordModalContext): Promise<void>;
};
