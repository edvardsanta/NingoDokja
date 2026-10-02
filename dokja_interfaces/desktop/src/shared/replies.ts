// What the main process hands to the renderer for each action: a projection of the orchestrator
// reply with only the fields a card reads, so channel IDs, provider profiles and other detail
// never reach the screen.

export type ServiceState = {
  status: string;
  detail: string;
  enabled?: boolean;
};

export type StatusReply = {
  status: string;
  services: Record<string, ServiceState>;
};
