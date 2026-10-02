// The orchestrator actions the desktop interface may ask for. The main process refuses anything
// else before it touches the socket: the orchestrator has no authentication and also exposes
// administrative actions, so a card has to be listed here before it can read data.
export const ACTION_TYPES = ["ningo.status"] as const;

export type ActionType = (typeof ACTION_TYPES)[number];
