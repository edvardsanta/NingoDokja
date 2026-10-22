import type { ComponentType } from "react";

import type { Transport } from "../../shared/transport.js";
import type { MessageId } from "../i18n/i18n.js";
import { AssistantCard } from "./assistant.js";
import { CommunicationsCard } from "./communications.js";
import { HealthCard } from "./health.js";
import { KnowledgeWorkspace } from "./knowledge_workspace.js";
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
// Messages, where they are sent. Reading is how knowledge comes in, so the sources it reads are a
// view of Knowledge rather than a workspace of their own.
export const CARDS: readonly CardEntry[] = [
  { kind: "assistant", labelId: "nav_assistant", Card: AssistantCard },
  { kind: "knowledge", labelId: "kind_knowledge", Card: KnowledgeWorkspace, services: ["knowledge", "feeds"] },
  { kind: "memory", labelId: "kind_memory", Card: MemoryCard, services: ["memory"] },
  { kind: "communications", labelId: "nav_messages", Card: CommunicationsCard, services: ["discord", "whatsapp"] },
  { kind: "health", labelId: "nav_system", Card: HealthCard, foot: true },
];

export const START = "health";
