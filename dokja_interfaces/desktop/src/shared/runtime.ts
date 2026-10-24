// Book and Meme are Feeds plugins, not standalone desktop runtime modules.
export const RUNTIME_MODULES = ["orchestrator", "memory", "knowledge", "feeds", "voice", "communications", "whatsapp", "discord", "scheduler"] as const;
export type RuntimeSetting = { readOnly?: boolean; id: string; group: string; kind: string; options: string[]; value: string; affects: string[]; note: string; default: string };
export type RuntimeSettings = { settings: RuntimeSetting[]; webhookChannels: string[]; busy: boolean; error: string };

// Discord answers and the scheduler posts once running: starting one is confirmed after saying what it will do.
export const DELIVERY_MODULES: readonly string[] = ["discord", "scheduler"];
export type RuntimeModule = (typeof RUNTIME_MODULES)[number];
export type RuntimeState = "starting" | "ready" | "running" | "stopping" | "stopped" | "failed";
export type RuntimeSnapshot = { modules: { name: RuntimeModule; state: RuntimeState; error: string; dependencies: string[]; progress?: string }[]; busy: boolean; error: string };

export type ProviderRow = {
  id: string; name: string; state: string; reason: string; models: string[];
  enabled?: boolean; declaredModels?: string[];
  modelDiscovery: string; inferenceStatus: "not_tested"; truncated: boolean;
  // Only on Ollama: each listed model's size as the engine gives it, and the model memory and
  // knowledge embed with, which is no chat model.
  modelSizes?: Record<string, string>; embeddingModel?: string;
};

export type ModelPullState = "pulling" | "done" | "failed" | "cancelled";
export type ModelPullPhase = "starting" | "downloading" | "verifying" | "writing";
export type ModelPull = { model: string; state: ModelPullState; phase?: ModelPullPhase; percent?: number; error?: string };
export type ModelsStatus = { embeddingModel: string; pull?: ModelPull; engine?: "starting" | "ready" | "failed"; engineError?: string };
export type ProvidersView = { configurationRevision?: number; providers: ProviderRow[]; checkedAt: string };

export type ProviderRegistrationChange = { provider: string; configurationRevision: number; enabled: boolean; models: string[] };
export type ProviderRegistrationSaved = { provider: string; configurationRevision: number };

export type ChatBinding = { configurationRevision: number; provider: string; model: string };
