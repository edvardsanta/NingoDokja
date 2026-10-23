import { createContext, useContext, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import type { Transport } from "../shared/transport.js";
import type { Tone } from "./cards/health_model.js";
import type { CardEntry } from "./cards/registry.js";
import { useTranslate } from "./i18n/context.js";
import { useStatus, type Tones } from "./status_context.js";

// Worst first: the dot of a workspace shows the worst tone among the services behind it.
const SEVERITY: readonly Tone[] = ["problem", "unchecked", "off", "ok", "other"];

function toneFor(entry: CardEntry, tones: Tones): Tone | undefined {
  const known = (entry.services ?? []).map((name) => tones[name]).filter((tone) => tone !== undefined);
  return SEVERITY.find((tone) => known.includes(tone));
}

// Opens a workspace by its kind, as a click on its entry would, and gives its entry the focus. A
// workspace uses it to send the owner where something is done (the conversation sends to System).
const OpenWorkspace = createContext<(kind: string) => void>(() => {});

export function useOpenWorkspace(): (kind: string) => void {
  return useContext(OpenWorkspace);
}

type SidebarProps = {
  entries: readonly CardEntry[];
  transport: Transport;
  // The workspace open when the screen starts; the first one when left out.
  start?: string;
  // The workspace that opens instead when the status says nothing answers, as long as the owner has
  // not moved yet: with the orchestrator down, the front door has nothing to say.
  fallback?: string;
  // What sits on top of the sidebar, above the workspaces.
  header?: ReactNode;
};

// One workspace per card, listed in a sidebar, the open one filling the work area. A card is
// mounted the first time its workspace opens and then kept, hidden while another is open: it keeps
// what it showed and is not asked for again, and the services behind a workspace nobody opened are
// never asked at all (the orchestrator answers one request at a time).
export function Sidebar({ entries, transport, start, fallback, header }: SidebarProps) {
  const t = useTranslate();
  const { tones, pulse } = useStatus();
  const baseId = useId();
  const [selected, setSelected] = useState(start ?? entries[0]?.kind ?? "");
  const [opened, setOpened] = useState<ReadonlySet<string>>(() => new Set(selected ? [selected] : []));
  const tabs = useRef(new Map<string, HTMLButtonElement>());
  const moved = useRef(false);
  const problems = Object.values(tones).filter((tone) => tone === "problem").length;
  const lost = pulse === "lost";

  const show = (kind: string) => {
    moved.current = true;
    setSelected(kind);
    setOpened((previous) => (previous.has(kind) ? previous : new Set(previous).add(kind)));
  };

  const select = (kind: string, focus = false) => {
    show(kind);
    if (focus) tabs.current.get(kind)?.focus();
  };

  useEffect(() => {
    if (lost && fallback && !moved.current) show(fallback);
  }, [lost, fallback]);

  // The up and down arrows, Home and End move between workspaces and open the one they reach.
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    const index = entries.findIndex((entry) => entry.kind === selected);
    let target: number;
    switch (event.key) {
      case "ArrowDown":
        target = index + 1;
        break;
      case "ArrowUp":
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

  // Ctrl and a digit open the workspaces in order, from anywhere, a field included: a bare digit
  // belongs to whatever is being typed.
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!event.ctrlKey || event.altKey || event.metaKey || event.shiftKey) return;
      // a dialog is open: what is behind it is not for the keys
      if (document.querySelector('[aria-modal="true"]')) return;
      if (!/^[1-9]$/.test(event.key)) return;
      const entry = entries[Number(event.key) - 1];
      if (!entry) return;
      event.preventDefault();
      select(entry.kind, true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [entries]);

  return (
    <div className="workspace">
      <aside className="sidebar">
        {header}
        <div
          className="workspaces"
          role="tablist"
          aria-orientation="vertical"
          aria-label={t("workspaces_label")}
          onKeyDown={onKeyDown}
        >
          {entries.map((entry, position) => {
            const tone = toneFor(entry, tones);
            // A word only when something is not well; when it is, the dot is enough.
            const word = entry.foot
              ? lost ? undefined : problems > 0 ? t("tab_need_attention") : undefined
              : tone === "problem" ? t("tab_needs_attention") : tone === "unchecked" ? t("tab_not_checked") : undefined;
            return (
              <button
                key={entry.kind}
                ref={(node) => {
                  if (node) tabs.current.set(entry.kind, node);
                  else tabs.current.delete(entry.kind);
                }}
                type="button"
                role="tab"
                className="tab"
                data-foot={entry.foot ? "" : undefined}
                id={`${baseId}-tab-${entry.kind}`}
                aria-selected={selected === entry.kind}
                aria-controls={`${baseId}-panel-${entry.kind}`}
                aria-keyshortcuts={`Control+${position + 1}`}
                aria-description={word}
                tabIndex={selected === entry.kind ? 0 : -1}
                onClick={() => select(entry.kind)}
              >
                <span className="tab-label">
                  {t(entry.labelId)}
                  {word && (
                    <span className="tab-word" data-tone={entry.foot ? "problem" : tone} aria-hidden="true">
                      {word}
                    </span>
                  )}
                </span>
                {/* the words for each state are on the System workspace; here they are a glance */}
                {tone && <span className="tab-dot" data-tone={tone} aria-hidden="true" />}
                {/* nothing answers: a count of the services would be a guess */}
                {entry.foot && lost && (
                  <span className="tab-count" data-tone="problem" aria-hidden="true">
                    {t("status_offline")}
                  </span>
                )}
                {entry.foot && !lost && problems > 0 && (
                  <span className="tab-count" data-tone="problem" aria-hidden="true">
                    {problems}
                  </span>
                )}
              </button>
            );
          })}
        </div>
      </aside>
      <div className="work-area">
        <OpenWorkspace.Provider value={(kind) => select(kind, true)}>
        {entries.map(({ kind, Card }) => (
          <div
            key={kind}
            role="tabpanel"
            id={`${baseId}-panel-${kind}`}
            aria-labelledby={`${baseId}-tab-${kind}`}
            hidden={selected !== kind}
          >
            {opened.has(kind) && <Card transport={transport} active={selected === kind} />}
          </div>
        ))}
        </OpenWorkspace.Provider>
      </div>
    </div>
  );
}
