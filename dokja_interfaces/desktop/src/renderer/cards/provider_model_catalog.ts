import type { ProviderRow } from "../../shared/runtime.js";

// CLI adapters cannot list account models. These are documented model IDs and
// aliases, not proof that the operator's account can run them.
const CLI_MODELS: Record<string, readonly string[]> = {
  codex: ["gpt-6-luna", "gpt-6.1-sol", "gpt-6-astra"],
  claude_code: ["sonnet", "opus"],
};

export function availableProviderModels(row: ProviderRow): string[] {
  if (row.id === "ollama") return row.modelDiscovery === "listed" ? row.models : [];
  return [...(CLI_MODELS[row.id] ?? [])];
}
