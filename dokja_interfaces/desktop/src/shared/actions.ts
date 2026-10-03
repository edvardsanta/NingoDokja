// The orchestrator actions the desktop interface may ask for. The main process refuses anything
// else before it touches the socket: the orchestrator has no authentication and also exposes
// administrative actions, so an action has to be listed here before the screen can ask for it.
// Almost all of them only read. The ones in WRITE_ACTION_TYPES change something, and the shell lets
// them through only just after a real click or key press, and the ones that cannot be undone also
// need the person to say yes in a window the shell opens. What else changes something (forget,
// record, switch, discord.send) is not here: communications.send is the one way to send.
export const ACTION_TYPES = [
  "communications.channels",
  "communications.history",
  "communications.send",
  "ningo.status",
  "memory.status",
  "memory.stats",
  "knowledge.status",
  "knowledge.search",
  "meme.status",
  "meme.list",
  "digest.status",
  "digest.items",
  "knowledge.ingest",
  "knowledge.list",
  "knowledge.delete",
  "knowledge.reindex",
] as const;

export type ActionType = (typeof ACTION_TYPES)[number];

export const WRITE_ACTION_TYPES: readonly ActionType[] = ["communications.send", "knowledge.ingest", "knowledge.delete", "knowledge.reindex"];

// The ones that cannot be undone: the shell asks the person first, in a window of its own.
export const CONFIRMED_ACTION_TYPES: readonly ActionType[] = ["communications.send", "knowledge.delete"];

export function needsConfirmation(type: ActionType): boolean {
  return CONFIRMED_ACTION_TYPES.includes(type);
}

export function isWriteAction(type: ActionType): boolean {
  return WRITE_ACTION_TYPES.includes(type);
}
