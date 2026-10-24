import type { ProviderRow } from "../../shared/runtime.js";

// CLI adapters cannot list account models. These are documented model IDs and
// aliases, not proof that the operator's account can run them.
const CLI_MODELS: Record<string, readonly string[]> = {
  codex: ["gpt-6-luna", "gpt-6.1-sol", "gpt-6-astra"],
  claude_code: ["sonnet", "opus"],
};

// The models a provider could chat with. Ollama's embedding model is listed with the others but is no
// chat model, so it is never offered.
export function availableProviderModels(row: ProviderRow): string[] {
  if (row.id === "ollama") return row.modelDiscovery === "listed" ? row.models.filter((model) => model !== row.embeddingModel) : [];
  return [...(CLI_MODELS[row.id] ?? [])];
}
