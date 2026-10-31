import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

import type { KnowledgeDocument, KnowledgeStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import type { Dropped } from "./knowledge_add.js";
import type { AddMode } from "./knowledge_add_model.js";
import { parseKnowledgeStatus } from "./knowledge_model.js";
import { useCard, type CardState, type LoadResult } from "./use_card.js";

// The embedder behind a status can take a few seconds.
const STATUS_TIMEOUT_MS = 15_000;
const MAX_OPEN_DOCUMENTS = 8;

export async function loadKnowledge(transport: Transport): Promise<LoadResult<KnowledgeStatus | Off>> {
  const answer = await transport.request("knowledge.status", {}, { timeoutMs: STATUS_TIMEOUT_MS });
  if (!answer.ok) return answer;
  const status = parseKnowledgeStatus(answer.result);
  return status
    ? { ok: true, data: status }
    : { ok: false, error: { code: "unexpected", message: "unexpected knowledge reply" } };
}

// A document opened in a tab. It is read by its id, so a tab does not care how it was opened.
export type OpenDocument = { id: string; title: string; classify: boolean };

export type Library = {
  transport: Transport;
  // What the service reports, read when the Library opens and again with the panel's refresh.
  status: CardState<KnowledgeStatus | Off>;
  reload: () => void;
  poll: () => void;
  refreshing: boolean;
  // Whether there is a Library to add to: the status is in and the service is not switched off.
  ready: boolean;
  // Moves when something changed what the list holds, so the list reads again.
  revision: number;
  // Something was added, deleted or indexed: the list reads again and the counts follow, quietly.
  changed: () => void;
  // Documents opened from the list or from a search hit, each in a tab of the workspace's strip.
  documents: readonly OpenDocument[];
  open: (document: Pick<KnowledgeDocument, "id" | "title">, classify?: boolean) => void;
  close: (id: string) => void;
  // Adding is one action of the workspace, not a view of it: the drawer opens on demand, from the
  // header or by a file dropped anywhere on the panel.
  adding: boolean;
  startAdding: () => void;
  stopAdding: () => void;
  // The way of adding last used, since the drawer is made again each time it opens.
  addMode: AddMode;
  setAddMode: (mode: AddMode) => void;
  // A file dropped before the drawer opened; forgotten when it closes, or it would take that way back
  // on every opening.
  dropped: Dropped | undefined;
  drop: (file: File) => void;
};

export const LibraryContext = createContext<Library | null>(null);

// What the Library keeps alive across a read: it lives above the views, which come and go (and which
// a read of the status may blank), so the open documents, the drawer and the counts survive them.
// `select` brings the tab of a document to the front when it opens.
export function useLibraryState(transport: Transport, select: (id: string) => void): Library {
  const load = useCallback(() => loadKnowledge(transport), [transport]);
  const { state, reload, poll, refreshing } = useCard(load);
  const [revision, setRevision] = useState(0);
  const changed = useCallback(() => {
    setRevision((value) => value + 1);
    poll();
  }, [poll]);

  // Opening one already open goes back to its tab; the oldest goes first past the limit. Classify…
  // opens the document with its classification shown; asking again for an open document shows that.
  const [documents, setDocuments] = useState<OpenDocument[]>([]);
  const open = useCallback<Library["open"]>(
    (document, classify = false) => {
      setDocuments((previous) =>
        previous.some((each) => each.id === document.id)
          ? previous.map((each) => (each.id === document.id && classify ? { ...each, classify } : each))
          : [...previous, { id: document.id, title: document.title || document.id, classify }].slice(-MAX_OPEN_DOCUMENTS),
      );
      select(document.id);
    },
    [select],
  );
  const close = useCallback((id: string) => setDocuments((previous) => previous.filter((each) => each.id !== id)), []);

  const [adding, setAdding] = useState(false);
  const [addMode, setAddMode] = useState<AddMode>("note");
  const [dropped, setDropped] = useState<Dropped>();
  const startAdding = useCallback(() => setAdding(true), []);
  const stopAdding = useCallback(() => {
    setAdding(false);
    setDropped(undefined);
  }, []);
  const drop = useCallback((file: File) => {
    setAdding(true);
    setDropped((previous) => ({ file, count: (previous?.count ?? 0) + 1 }));
  }, []);

  return {
    transport,
    status: state,
    reload,
    poll,
    refreshing,
    ready: state.phase === "ready" && !state.data.off,
    revision,
    changed,
    documents,
    open,
    close,
    adding,
    startAdding,
    stopAdding,
    addMode,
    setAddMode,
    dropped,
    drop,
  };
}

// A view used outside the workspace (a test renders the Documents view alone) keeps its own library.
export function StandaloneLibrary({ transport, children }: { transport: Transport; children: ReactNode }) {
  const library = useLibraryState(transport, noSelect);
  return <LibraryContext.Provider value={library}>{children}</LibraryContext.Provider>;
}
const noSelect = () => {};

export function useLibrary(): Library {
  const library = useContext(LibraryContext);
  if (!library) throw new Error("the Library's views need a LibraryContext above them");
  return library;
}
