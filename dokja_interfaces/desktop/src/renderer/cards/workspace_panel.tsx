import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { useTranslate } from "../i18n/context.js";

type PanelValue = {
  register: (reload: () => void) => () => void;
  hold: () => () => void;
};

const PanelContext = createContext<PanelValue | null>(null);

// A section asks the panel it sits in to read it again when the owner refreshes, and holds the
// refresh while it is still reading. A section outside any panel registers nothing.
export function useWorkspaceRefresh(reload: () => void, busy = false) {
  const panel = useContext(PanelContext);
  useEffect(() => panel?.register(reload), [panel, reload]);
  // in the same commit as the section's own change, so the button never lags behind what it shows
  useLayoutEffect(() => (panel && busy ? panel.hold() : undefined), [panel, busy]);
}

type WorkspacePanelProps = {
  // Where the owner is, as the legend cut into the top edge shows it: the workspace, then the view.
  trail: readonly string[];
  // The views of the workspace, on the left of the header.
  views?: ReactNode;
  // What the workspace does, on the right of the header after the refresh: at most one primary.
  actions?: ReactNode;
  // Pinned under the body, which scrolls on its own: a composer stays put while a list grows.
  dock?: ReactNode;
  children: ReactNode;
};

// The one panel of a workspace. Its sections are not cards of their own: there is one legend, one
// refresh for everything the workspace shows, and only the body scrolls.
export function WorkspacePanel({ trail, views, actions, dock, children }: WorkspacePanelProps) {
  const t = useTranslate();
  const reloads = useRef(new Set<() => void>());
  // How many sections can be read again: a workspace with none (the conversation) shows no refresh.
  const [sections, setSections] = useState(0);
  const register = useCallback((reload: () => void) => {
    reloads.current.add(reload);
    setSections(reloads.current.size);
    return () => {
      reloads.current.delete(reload);
      setSections(reloads.current.size);
    };
  }, []);
  const [holds, setHolds] = useState(0);
  const hold = useCallback(() => {
    setHolds((count) => count + 1);
    return () => setHolds((count) => count - 1);
  }, []);
  const value = useMemo(() => ({ register, hold }), [register, hold]);
  const refresh = () => reloads.current.forEach((reload) => reload());

  return (
    <PanelContext.Provider value={value}>
      <section className="wp" aria-label={trail.join(" › ")}>
        <p className="wp-legend" aria-hidden="true">
          {trail.map((step, index) => (
            <span key={index} aria-current={index === trail.length - 1 ? "location" : undefined}>
              {step}
            </span>
          ))}
        </p>
        <header className="wp-head">
          <div className="wp-views">{views}</div>
          <div className="wp-actions">
            {sections > 0 && (
              <button type="button" className="wp-refresh" aria-label={t("refresh")} title={t("refresh")} disabled={holds > 0} onClick={refresh}>
                <span aria-hidden="true">⟳</span>
              </button>
            )}
            {actions}
          </div>
        </header>
        <div className="wp-body">{children}</div>
        {dock && <div className="wp-dock">{dock}</div>}
      </section>
    </PanelContext.Provider>
  );
}
