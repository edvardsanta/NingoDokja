import { useEffect, useId, useRef, type KeyboardEvent, type ReactNode } from "react";

import { useTranslate } from "../i18n/context.js";

const FOCUSABLE = "button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [href], [tabindex]:not([tabindex='-1'])";

// A side panel over the right of the window for a task that is not the view itself, such as adding to
// the Library. While it is open the focus stays inside it; Esc, × or a press on the shade behind it
// closes it, and the focus goes back to what opened it.
export function Drawer({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const t = useTranslate();
  const id = useId();
  const panel = useRef<HTMLElement>(null);
  const opener = useRef<HTMLElement | null>(null);

  useEffect(() => {
    opener.current = document.activeElement as HTMLElement | null;
    // The first field, not the close button: the drawer opens to be used.
    const fields = [...(panel.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])];
    (fields.find((field) => !field.classList.contains("drawer-close")) ?? fields[0])?.focus();
    return () => opener.current?.focus?.();
  }, []);

  const keep = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      onClose();
      return;
    }
    if (event.key !== "Tab") return;
    const fields = [...(panel.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])];
    const first = fields[0];
    const last = fields.at(-1);
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  };

  return <>
    <div className="drawer-shade" aria-hidden="true" onClick={onClose} />
    <aside ref={panel} className="drawer" role="dialog" aria-modal="true" aria-labelledby={`${id}-title`} onKeyDown={keep}>
      <header className="drawer-head">
        <h3 id={`${id}-title`}>{title}</h3>
        <button type="button" className="drawer-close" aria-label={t("drawer_close_named", { title })} onClick={onClose}>×</button>
      </header>
      <div className="drawer-body">{children}</div>
    </aside>
  </>;
}
