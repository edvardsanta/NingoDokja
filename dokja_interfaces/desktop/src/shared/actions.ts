// The orchestrator actions the desktop interface may ask for. The main process refuses anything
// else before it touches the socket: the orchestrator has no authentication and also exposes
// administrative actions, so an action has to be listed here before the screen can ask for it.
// Almost all of them only read. The ones in WRITE_ACTION_TYPES change something, and the shell lets
// them through only just after a real click or key press, and the ones that cannot be undone also
// need the person to say yes in a window the shell opens. The switches (services.set and
// scheduler.jobs.set) are changes too, but the operator can switch them back at once, so they ask
// for no window. Linking WhatsApp is the same: reading the code (communications.link.status) is
// only a read, asking for a new one is a change, and starting over also unlinks the device and
// erases the saved session, so it is asked in a window. Experience feedback (memory.resolve) also requires a gesture and confirmation because the first verdict is retained. What else changes something (forget, record, discord.send, the scheduler's own
// heartbeat) is not here: communications.send is the one way to send.
export const ACTION_TYPES = [
  "realtime.snapshot",
  "runtime.modules",
  "runtime.logs",
  "message.created",
  "runtime.set",
  "runtime.providers",
  "runtime.chat",
  "runtime.providers.configure",
  "runtime.chat.configure",
  "runtime.settings",
  "runtime.settings.set",
  // The local engine's models: reading their state is a read; starting the engine, downloading,
  // cancelling and removing are changes, and a download or a removal is asked in a window.
  "runtime.models.status",
  "runtime.models.engine.start",
  "runtime.models.pull",
  "runtime.models.cancel",
  "runtime.models.remove",
  "communications.channels",
  "communications.history",
  "communications.send",
  "meme.hashtag.tag",
  "communications.link.status",
  "communications.link.start",
  "communications.link.reset",
  "services.set",
  "scheduler.jobs.set",
  "ningo.status",
  "memory.status",
  "memory.stats",
  "memory.list",
  "memory.get",
  "memory.resolve",
  "knowledge.status",
  "knowledge.search",
  "meme.status",
  "meme.list",
  // Fetching new memes from the sources: a change, on a real press, never asked in a window.
  "meme.pool.refresh",
  "digest.status",
  "digest.items",
  "knowledge.ingest",
  "knowledge.list",
  "knowledge.classify",
  "knowledge.delete",
  "knowledge.reindex",
] as const;

export type ActionType = (typeof ACTION_TYPES)[number];

export const WRITE_ACTION_TYPES: readonly ActionType[] = [
  "memory.resolve",
  "runtime.set",
  "runtime.providers.configure",
  "runtime.chat.configure",
  "runtime.settings.set",
  "runtime.models.engine.start",
  "runtime.models.pull",
  "runtime.models.cancel",
  "runtime.models.remove",
  "message.created",
  "communications.send",
  "meme.hashtag.tag",
  "meme.pool.refresh",
  "communications.link.start",
  "communications.link.reset",
  "services.set",
  "scheduler.jobs.set",
  "knowledge.ingest",
  "knowledge.delete",
  "knowledge.reindex",
];

// The ones that cannot be undone: the shell asks the person first, in a window of its own.
export const CONFIRMED_ACTION_TYPES: readonly ActionType[] = [
  "memory.resolve",
  "communications.send",
  "communications.link.reset",
  "knowledge.delete",
  "runtime.models.pull",
  "runtime.models.remove",
];

export function needsConfirmation(type: ActionType, payload?: Record<string, unknown>): boolean {
  if (type === "communications.send") return payload?.force_nsfw === true;
  return CONFIRMED_ACTION_TYPES.includes(type);
}

export function isWriteAction(type: ActionType): boolean {
  return WRITE_ACTION_TYPES.includes(type);
}
