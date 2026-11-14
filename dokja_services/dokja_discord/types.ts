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

export type DiscordAttachment = {
  filename: string;
  content_type: "image/png";
  data_b64: string;
};

export type DiscordReply =
  | string
  | {
      content: string;
      ephemeral?: boolean;
      components?: DiscordComponentSpec[];
      attachments?: DiscordAttachment[];
      embeds?: DiscordEmbedSpec[];
    };

// A card under a message: a title, some text and a small picture beside it, with an
// optional side colour, short labelled fields and a footer line.
export type DiscordEmbedSpec = {
  title?: string;
  description?: string;
  thumbnailUrl?: string;
  color?: number;
  fields?: Array<{ name: string; value: string; inline?: boolean }>;
  footer?: string;
  // the page the title links to; kept only when it is an http(s) address
  url?: string;
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
  // Dismiss the current command acknowledgement after its result reached the shared panel.
  deleteReply?(): Promise<void>;
  showModal(modal: DiscordModalSpec): Promise<void>;
};

export type DiscordMessageContext = {
  documentAttachments?: DiscordDocumentAttachment[];
  userId: string;
  userName?: string;
  channelId: string;
  messageId: string;
  guildId?: string;
  content: string;
  replyToContent?: string;
  mentionedUserIds?: string[];
  // The names the gateway shows for the tagged accounts, so the text can name them instead of an id.
  mentionedUsers?: Array<{ id: string; name: string }>;
  isBot: boolean;
  isDirectMessage: boolean;
  mentionsBot: boolean;
  reply(message: DiscordReply): Promise<void>;
};

export type DiscordDocumentAttachment = {
  url: string;
  filename: string;
  content_type: string;
  size: number;
};

export type DiscordSlashCommand = {
  name: string;
  description: string;
  // Discord's translations, by locale: people see their own client's language.
  name_localizations?: Record<string, string>;
  description_localizations?: Record<string, string>;
  options?: any[];
  defer?: boolean;
  handle(context: DiscordCommandContext): Promise<void>;
  // Suggests values for an option declared with autocomplete: true, as the person types. Discord
  // shows at most 25 and waits about three seconds.
  autocomplete?(context: DiscordAutocompleteContext): Promise<DiscordChoice[]>;
};

export type DiscordAutocompleteContext = {
  option: string;
  value: string;
  userId: string;
  channelId: string;
  guildId?: string;
};

export type DiscordChoice = { name: string; value: string };

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
