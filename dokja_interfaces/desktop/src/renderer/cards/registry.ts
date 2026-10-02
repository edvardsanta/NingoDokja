import type { ComponentType } from "react";

import type { Transport } from "../../shared/transport.js";
import { HealthCard } from "./health.js";
import { KnowledgeCard } from "./knowledge.js";
import { MemesCard } from "./memes.js";
import { MemoryCard } from "./memory.js";

export type CardEntry = { kind: string; Card: ComponentType<{ transport: Transport }> };

// Every card the screen knows, in the order it shows them. Adding a card is adding a line here
// (and its action to the allow-list in src/shared/actions.ts).
export const CARDS: readonly CardEntry[] = [
  { kind: "health", Card: HealthCard },
  { kind: "memory", Card: MemoryCard },
  { kind: "knowledge", Card: KnowledgeCard },
  { kind: "memes", Card: MemesCard },
];
