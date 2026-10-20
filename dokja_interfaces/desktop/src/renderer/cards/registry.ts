import type { ComponentType } from "react";

import type { Transport } from "../../shared/transport.js";
import type { MessageId } from "../i18n/i18n.js";
import { CommunicationsCard } from "./communications.js";
import { DigestCard } from "./digest.js";
import { HealthCard } from "./health.js";
import { KnowledgeCard } from "./knowledge.js";
import { MemoryCard } from "./memory.js";

export type CardEntry = {
  kind: string;
  // The workspace's name in the sidebar.
  labelId: MessageId;
  Card: ComponentType<{ transport: Transport; active?: boolean }>;
  // The services and interfaces behind the workspace, by the names the status reports. The sidebar
  // shows the worst of their tones as a dot; a workspace with none known shows no dot.
  services?: readonly string[];
  // Kept apart at the foot of the sidebar.
  foot?: boolean;
};

// Every workspace the screen knows, in the order of the sidebar. Adding one is adding a line here
// (and its action to the allow-list in src/shared/actions.ts). System holds the switches and is the
// one that reads the status, so the screen opens on it. Memes have no workspace: they are picked in
// Messages, where they are sent.
export const CARDS: readonly CardEntry[] = [
  { kind: "digest", labelId: "kind_digest", Card: DigestCard },
  { kind: "knowledge", labelId: "kind_knowledge", Card: KnowledgeCard, services: ["knowledge"] },
  { kind: "memory", labelId: "kind_memory", Card: MemoryCard, services: ["memory"] },
  { kind: "communications", labelId: "nav_messages", Card: CommunicationsCard, services: ["discord", "whatsapp"] },
  { kind: "health", labelId: "nav_system", Card: HealthCard, foot: true },
];

export const START = "health";
