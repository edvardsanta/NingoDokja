import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

// A thing opened from a view that earns a tab of its own, after the views and a separator: a
// document of the Library. The owner closes it; the workspace says what is open.
export type ItemTab = {
  id: string;
  label: string;
  // The name of the close button, such as "Close {title}".
  closeLabel: string;
  onClose: () => void;
};

// What is on screen: one of the fixed views, or one of the items.
type Shown<Id extends string> = { item: false; id: Id } | { item: true; id: string };

// The views of a workspace. They are places, so they are tabs: they sit in the panel's header, the
// arrows move between them as in the sidebar, and a view is mounted the first time it opens and then
// kept, hidden while another is shown, so what it holds survives a look elsewhere. A view nobody
// opened asks nothing. Items opened from a view join the same strip after the views and are
// closable; an item is mounted while it exists and hidden while another tab is shown.
export function useViewTabs<Id extends string>(
  label: string,
  views: readonly { id: Id; label: string }[],
  items: readonly ItemTab[] = [],
) {
  const first = views[0]?.id;
  const [shown, setShown] = useState<Shown<Id> | undefined>(first === undefined ? undefined : { item: false, id: first });
  const [opened, setOpened] = useState<ReadonlySet<Id>>(() => new Set(first ? [first] : []));
  const baseId = useId();
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  const strip = useRef<HTMLDivElement>(null);

  // An item that was closed leaves the first view in its place.
  const current: Shown<Id> | undefined =
    shown?.item && !items.some((item) => item.id === shown.id) ? (first === undefined ? undefined : { item: false, id: first }) : shown;
  const currentItem = current?.item ? items.find((item) => item.id === current.id) : undefined;
  const currentView = current && !current.item ? current.id : undefined;

  const keyOf = (item: boolean, id: string) => `${item ? "item" : "view"}:${id}`;
  const focusTab = (item: boolean, id: string) => buttons.current.get(keyOf(item, id))?.focus();

  const show = (id: Id, focus = false) => {
    setShown({ item: false, id });
    setOpened((previous) => (previous.has(id) ? previous : new Set(previous).add(id)));
    if (focus) focusTab(false, id);
  };
  const showItem = (id: string, focus = false) => {
    setShown({ item: true, id });
    if (focus) focusTab(true, id);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    // from a tab or the strip itself: the close button beside a tab is not a place to move from
    if (event.target !== event.currentTarget && (event.target as HTMLElement).getAttribute("role") !== "tab") return;
    event.preventDefault();
    const order: Shown<Id>[] = [
      ...views.map((view): Shown<Id> => ({ item: false, id: view.id })),
      ...items.map((item): Shown<Id> => ({ item: true, id: item.id })),
    ];
    const index = order.findIndex((entry) => current !== undefined && entry.item === current.item && entry.id === current.id);
    const next = order[(index + (event.key === "ArrowRight" ? 1 : -1) + order.length) % order.length];
    if (!next) return;
    if (next.item) showItem(next.id, true);
    else show(next.id, true);
  };

  // Alt and a digit open the views in order (the items take no digit). Every workspace kept in the
  // background has its own strip, so only the one on screen answers.
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      if (!/^[1-9]$/.test(event.key)) return;
      if (!strip.current || strip.current.closest("[hidden]")) return;
      // a dialog is open: what is behind it is not for the keys
      if (document.querySelector('[aria-modal="true"]')) return;
      const view = views[Number(event.key) - 1];
      if (!view) return;
      event.preventDefault();
      show(view.id, true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [views]);

  const itemKey = (id: string) => encodeURIComponent(id);
  const track = (key: string) => (node: HTMLButtonElement | null) => {
    if (node) buttons.current.set(key, node);
    else buttons.current.delete(key);
  };

  // One view needs no tabs: there is nowhere else to go.
  const tabs =
    views.length > 1 ? (
      <div ref={strip} className="vtabs" role="tablist" aria-label={label} onKeyDown={onKeyDown}>
        {views.map((view, position) => (
          <button
            key={view.id}
            ref={track(keyOf(false, view.id))}
            type="button"
            role="tab"
            className="vtab"
            id={`${baseId}-tab-${view.id}`}
            aria-selected={currentView === view.id}
            aria-controls={`${baseId}-view-${view.id}`}
            aria-keyshortcuts={`Alt+${position + 1}`}
            tabIndex={currentView === view.id ? 0 : -1}
            onClick={() => show(view.id)}
          >
            {view.label}
          </button>
        ))}
        {items.length > 0 && <span className="vtab-sep" aria-hidden="true" />}
        {items.map((item) => {
          const selected = current?.item === true && current.id === item.id;
          return (
            <span key={item.id} className="vtab-item" role="presentation">
              <button
                ref={track(keyOf(true, item.id))}
                type="button"
                role="tab"
                className="vtab"
                id={`${baseId}-tab-item-${itemKey(item.id)}`}
                title={item.label}
                aria-selected={selected}
                aria-controls={`${baseId}-item-${itemKey(item.id)}`}
                tabIndex={selected ? 0 : -1}
                onClick={() => showItem(item.id)}
              >
                {item.label}
              </button>
              <button
                type="button"
                className="vtab-close"
                aria-label={item.closeLabel}
                title={item.closeLabel}
                tabIndex={selected ? 0 : -1}
                onClick={() => {
                  // the focus does not fall to the page with the tab that held it
                  if (selected && first !== undefined) focusTab(false, first);
                  item.onClose();
                }}
              >
                <span aria-hidden="true">×</span>
              </button>
            </span>
          );
        })}
      </div>
    ) : undefined;

  const panels = (renderView: (id: Id) => ReactNode, renderItem?: (id: string) => ReactNode) => [
    ...views.map((view) => (
      <div
        key={view.id}
        role={tabs ? "tabpanel" : undefined}
        id={`${baseId}-view-${view.id}`}
        aria-labelledby={tabs ? `${baseId}-tab-${view.id}` : undefined}
        hidden={currentView !== view.id}
      >
        {opened.has(view.id) && renderView(view.id)}
      </div>
    )),
    ...(renderItem
      ? items.map((item) => (
          <div
            key={`item:${item.id}`}
            role="tabpanel"
            id={`${baseId}-item-${itemKey(item.id)}`}
            aria-labelledby={`${baseId}-tab-item-${itemKey(item.id)}`}
            hidden={!(current?.item === true && current.id === item.id)}
          >
            {renderItem(item.id)}
          </div>
        ))
      : []),
  ];

  const currentLabel = views.find((view) => view.id === currentView)?.label ?? "";
  return { tabs, panels, currentLabel, currentView, currentItem, show, showItem };
}
