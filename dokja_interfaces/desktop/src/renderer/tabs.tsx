import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";

import type { Transport } from "../shared/transport.js";
import type { CardEntry } from "./cards/registry.js";
import { useTranslate } from "./i18n/context.js";

const TYPING = new Set(["INPUT", "TEXTAREA", "SELECT"]);

// One tab per card. A card is mounted the first time its tab opens and then kept, hidden while
// another tab is open: it keeps what it showed and is not asked for again, and the services behind
// a tab nobody opened are never asked at all (the orchestrator answers one request at a time).
export function Tabs({ entries, transport }: { entries: readonly CardEntry[]; transport: Transport }) {
  const t = useTranslate();
  const baseId = useId();
  const [selected, setSelected] = useState(entries[0]?.kind ?? "");
  const [opened, setOpened] = useState<ReadonlySet<string>>(() => new Set(selected ? [selected] : []));
  const tabs = useRef(new Map<string, HTMLButtonElement>());

  const select = (kind: string, focus = false) => {
    setSelected(kind);
    setOpened((previous) => (previous.has(kind) ? previous : new Set(previous).add(kind)));
    if (focus) tabs.current.get(kind)?.focus();
  };

  // Arrow keys, Home and End move between tabs and open the one they reach.
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    const index = entries.findIndex((entry) => entry.kind === selected);
    let target: number;
    switch (event.key) {
      case "ArrowRight":
        target = index + 1;
        break;
      case "ArrowLeft":
        target = index - 1;
        break;
      case "Home":
        target = 0;
        break;
      case "End":
        target = entries.length - 1;
        break;
      default:
        return;
    }
    event.preventDefault();
    const next = entries[(target + entries.length) % entries.length];
    if (next) select(next.kind, true);
  };

  // The digits open the tabs in order, as in the TUI, unless a field is being typed in.
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.ctrlKey || event.altKey || event.metaKey) return;
      // a dialog is open: what is behind it is not for the keys
      if (document.querySelector('[aria-modal="true"]')) return;
      const origin = event.target;
      if (origin instanceof HTMLElement && (origin.isContentEditable || TYPING.has(origin.tagName))) return;
      const entry = entries[Number(event.key) - 1];
      if (entry) select(entry.kind, true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [entries]);

  return (
    <>
      <div className="tabs" role="tablist" aria-label={t("tabs_label")} onKeyDown={onKeyDown}>
        {entries.map((entry, position) => (
          <button
            key={entry.kind}
            ref={(node) => {
              if (node) tabs.current.set(entry.kind, node);
              else tabs.current.delete(entry.kind);
            }}
            type="button"
            role="tab"
            className="tab"
            id={`${baseId}-tab-${entry.kind}`}
            aria-selected={selected === entry.kind}
            aria-controls={`${baseId}-panel-${entry.kind}`}
            aria-keyshortcuts={String(position + 1)}
            tabIndex={selected === entry.kind ? 0 : -1}
            onClick={() => select(entry.kind)}
          >
            {t(entry.labelId)}
          </button>
        ))}
      </div>
      {entries.map(({ kind, Card }) => (
        <div
          key={kind}
          role="tabpanel"
          id={`${baseId}-panel-${kind}`}
          aria-labelledby={`${baseId}-tab-${kind}`}
          hidden={selected !== kind}
        >
          {opened.has(kind) && <Card transport={transport} />}
        </div>
      ))}
    </>
  );
}
