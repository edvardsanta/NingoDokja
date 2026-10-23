import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

// The views of a workspace. They are places, so they are tabs: they sit in the panel's header, the
// arrows move between them as in the sidebar, and a view is mounted the first time it opens and then
// kept, hidden while another is shown, so what it holds survives a look elsewhere. A view nobody
// opened asks nothing.
export function useViewTabs<Id extends string>(label: string, views: readonly { id: Id; label: string }[]) {
  const first = views[0]?.id;
  const [current, setCurrent] = useState<Id | undefined>(first);
  const [opened, setOpened] = useState<ReadonlySet<Id>>(() => new Set(first ? [first] : []));
  const baseId = useId();
  const buttons = useRef(new Map<Id, HTMLButtonElement>());

  const show = (id: Id, focus = false) => {
    setCurrent(id);
    setOpened((previous) => (previous.has(id) ? previous : new Set(previous).add(id)));
    if (focus) buttons.current.get(id)?.focus();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    event.preventDefault();
    const index = views.findIndex((view) => view.id === current) + (event.key === "ArrowRight" ? 1 : -1);
    const next = views[(index + views.length) % views.length];
    if (next) show(next.id, true);
  };

  // One view needs no tabs: there is nowhere else to go.
  const tabs =
    views.length > 1 ? (
      <div className="vtabs" role="tablist" aria-label={label} onKeyDown={onKeyDown}>
        {views.map((view) => (
          <button
            key={view.id}
            ref={(node) => {
              if (node) buttons.current.set(view.id, node);
              else buttons.current.delete(view.id);
            }}
            type="button"
            role="tab"
            className="vtab"
            id={`${baseId}-tab-${view.id}`}
            aria-selected={current === view.id}
            aria-controls={`${baseId}-view-${view.id}`}
            tabIndex={current === view.id ? 0 : -1}
            onClick={() => show(view.id)}
          >
            {view.label}
          </button>
        ))}
      </div>
    ) : undefined;

  const panels = (render: (id: Id) => ReactNode) =>
    views.map((view) => (
      <div
        key={view.id}
        role={tabs ? "tabpanel" : undefined}
        id={`${baseId}-view-${view.id}`}
        aria-labelledby={tabs ? `${baseId}-tab-${view.id}` : undefined}
        hidden={current !== view.id}
      >
        {opened.has(view.id) && render(view.id)}
      </div>
    ));

  const currentLabel = views.find((view) => view.id === current)?.label ?? "";
  return { tabs, panels, currentLabel };
}
