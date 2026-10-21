export const RUNTIME_MODULES = ["orchestrator", "memory", "knowledge", "meme", "book", "chat_ai", "feeds", "voice", "whatsapp"] as const;
export type RuntimeModule = (typeof RUNTIME_MODULES)[number];
export type RuntimeState = "starting" | "ready" | "running" | "stopping" | "stopped" | "failed";
export type RuntimeSnapshot = { modules: { name: RuntimeModule; state: RuntimeState; error: string; dependencies: string[] }[]; busy: boolean; error: string };
