import type { ComponentType } from "react";

import type { Transport } from "../../shared/transport.js";
import type { MessageId } from "../i18n/i18n.js";
import { HealthCard } from "./health.js";
import { KnowledgeCard } from "./knowledge.js";
import { MemesCard } from "./memes.js";
import { MemoryCard } from "./memory.js";

export type CardEntry = {
  kind: string;
  // The tab's name: the same word as the card's kind.
  labelId: MessageId;
  Card: ComponentType<{ transport: Transport }>;
};

// Every card the screen knows, one tab each, in the order of the tabs. Adding a card is adding a
// line here (and its action to the allow-list in src/shared/actions.ts).
export const CARDS: readonly CardEntry[] = [
  { kind: "health", labelId: "kind_health", Card: HealthCard },
  { kind: "memory", labelId: "kind_memory", Card: MemoryCard },
  { kind: "knowledge", labelId: "kind_knowledge", Card: KnowledgeCard },
  { kind: "memes", labelId: "kind_memes", Card: MemesCard },
];
