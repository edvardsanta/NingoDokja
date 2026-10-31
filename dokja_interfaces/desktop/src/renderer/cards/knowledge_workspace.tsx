import { useCallback, useEffect, useLayoutEffect, useRef, useState, type DragEvent } from "react";

import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { DigestCard } from "./digest.js";
import { DocumentView } from "./document_view.js";
import { Drawer } from "./drawer.js";
import { KnowledgeCard } from "./knowledge.js";
import { AddToBase } from "./knowledge_add.js";
import { LibraryContext, useLibraryState } from "./library_context.js";
import { useViewTabs } from "./view_tabs.js";
import { WorkspacePanel } from "./workspace_panel.js";

// Lights the panel while files are dragged over it and hands over the first file dropped. Entering and
// leaving fire for every child crossed, so they are counted rather than trusted one by one. Only a
// drag that carries files counts: dragged text is left to the fields. Where there is nothing to add
// to, a file is left alone.
function useFileDrop(onFile: (file: File) => void, enabled: boolean) {
  const [depth, setDepth] = useState(0);
  const carriesFiles = (event: DragEvent) => enabled && Array.from(event.dataTransfer.types).includes("Files");
  const handlers = {
    onDragEnter: (event: DragEvent) => {
      if (carriesFiles(event)) setDepth((value) => value + 1);
    },
    onDragLeave: (event: DragEvent) => {
      if (carriesFiles(event)) setDepth((value) => Math.max(0, value - 1));
    },
    onDragOver: (event: DragEvent) => {
      if (carriesFiles(event)) event.preventDefault();
    },
    onDrop: (event: DragEvent) => {
      setDepth(0);
      // the form took it already
      if (event.defaultPrevented || !enabled) return;
      const file = event.dataTransfer.files[0];
      if (!file) return;
      event.preventDefault();
      onFile(file);
    },
  };
  return { dragging: depth > 0 && enabled, handlers };
}

// What Ningo knows: the documents it keeps, and the readings the feeds bring in. Reading is how
// knowledge comes in, not a place of its own, so both live in one workspace, each a view of it. The
// documents opened from the list are tabs of the same strip, and Add is the workspace's one action:
// both belong to the Library, not to a view, so they stay put when the owner moves between views.
export function KnowledgeWorkspace({ transport }: { transport: Transport }) {
  const t = useTranslate();
  // the library opens a document's tab, which only the strip below can do
  const select = useRef<(id: string) => void>(() => {});
  const choose = useCallback((id: string) => select.current(id), []);
  const library = useLibraryState(transport, choose);

  const items = library.documents.map((document) => ({
    id: document.id,
    label: document.title,
    closeLabel: t("view_close_named", { title: document.title }),
    onClose: () => library.close(document.id),
  }));
  const { tabs, panels, currentLabel, currentItem, show, showItem } = useViewTabs(
    t("nav_library"),
    [
      { id: "documents", label: t("knowledge_view_documents") },
      { id: "sources", label: t("knowledge_view_sources") },
    ],
    items,
  );
  useLayoutEffect(() => {
    select.current = showItem;
  });

  // Ctrl+F is the Library's: it goes to the search of the view on screen, or to the Documents' when a
  // document is shown. Every workspace kept in the background listens too, so only the one on screen
  // answers, and nothing does while a dialog is open.
  const zone = useRef<HTMLDivElement>(null);
  const [asked, setAsked] = useState(0);
  const findable = () => zone.current?.querySelector<HTMLElement>('[role="tabpanel"]:not([hidden]) [data-library-search]');
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== "f") return;
      if (!zone.current || zone.current.closest("[hidden]") || document.querySelector('[aria-modal="true"]')) return;
      // a document is on screen: its search is in the list, behind it
      if (currentItem) show("documents");
      else if (!findable()) return;
      event.preventDefault();
      setAsked((count) => count + 1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
  useEffect(() => {
    if (asked === 0) return;
    const field = findable();
    field?.focus();
    if (field instanceof HTMLInputElement) field.select();
  }, [asked]);

  // A file dropped anywhere on the panel, on either view, opens the drawer on the way of adding a file.
  const drop = useFileDrop(library.drop, library.ready);
  const trail = currentItem
    ? [t("nav_library"), t("knowledge_view_documents"), currentItem.label]
    : [t("nav_library"), currentLabel];
  // the words on the button are short; its name still says where it adds
  const add = library.ready ? (
    <button type="button" className="library-add primary" aria-label={t("add_title")} aria-haspopup="dialog" aria-expanded={library.adding} onClick={library.startAdding}>
      {t("library_add")}
    </button>
  ) : undefined;

  return (
    <LibraryContext.Provider value={library}>
      <div ref={zone} className="drop-zone" data-dragging={drop.dragging ? "" : undefined} {...drop.handlers}>
        <WorkspacePanel trail={trail} views={tabs} actions={add}>
          {drop.dragging && (
            <p className="drop-hint" aria-hidden="true">
              {t("add_drop_hint")}
            </p>
          )}
          {panels(
            (id) => (id === "documents" ? <KnowledgeCard transport={transport} /> : <DigestCard transport={transport} />),
            (id) => {
              const document = library.documents.find((each) => each.id === id);
              return document && (
                <DocumentView transport={transport} id={document.id} classify={document.classify} />
              );
            },
          )}
        </WorkspacePanel>
      </div>
      {library.adding && (
        <Drawer title={t("add_title")} onClose={library.stopAdding}>
          <AddToBase
            transport={transport}
            onAdded={library.changed}
            dropped={library.dropped}
            titled={false}
            startMode={library.addMode}
            onMode={library.setAddMode}
          />
        </Drawer>
      )}
    </LibraryContext.Provider>
  );
}
