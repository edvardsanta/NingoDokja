export const RUNTIME_MODULES = ["orchestrator", "memory", "knowledge", "meme", "book", "chat_ai", "feeds", "voice", "communications", "whatsapp", "discord", "scheduler"] as const;
export type RuntimeSetting = { id: string; group: string; kind: string; options: string[]; value: string; affects: string[]; note: string; default: string };
export type RuntimeSettings = { settings: RuntimeSetting[]; webhookChannels: string[]; busy: boolean; error: string };

// Discord answers and the scheduler posts once running: starting one is confirmed after saying what it will do.
export const DELIVERY_MODULES: readonly string[] = ["discord", "scheduler"];
export type RuntimeModule = (typeof RUNTIME_MODULES)[number];
export type RuntimeState = "starting" | "ready" | "running" | "stopping" | "stopped" | "failed";
export type RuntimeSnapshot = { modules: { name: RuntimeModule; state: RuntimeState; error: string; dependencies: string[] }[]; busy: boolean; error: string };
