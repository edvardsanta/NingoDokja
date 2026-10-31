import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";

import { createPortal } from "react-dom";

import { useTranslate } from "../i18n/context.js";

export type MenuItem = { label: string; run: () => void; disabled?: boolean };

type Place = { x: number; y: number };

// The things to do with one row, in one menu: opened with a right click on the row (at the pointer),
// with the Menu key or Shift+F10 on the row, or with the row's ⋯ button. Arrows move, Enter runs,
// Esc closes, and the focus goes back where it was. The row itself stays a plain list item.
export function useRowMenu(items: readonly MenuItem[], name: string) {
  const [place, setPlace] = useState<Place>();
  const back = useRef<HTMLElement | null>(null);
  const openAt = useCallback((at: Place) => {
    back.current = document.activeElement as HTMLElement | null;
    setPlace(at);
  }, []);
  const close = useCallback(() => {
    setPlace(undefined);
    back.current?.focus?.();
  }, []);

  const rowProps = {
    onContextMenu: (event: MouseEvent<HTMLElement>) => {
      event.preventDefault();
      openAt({ x: event.clientX, y: event.clientY });
    },
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (event.key !== "ContextMenu" && !(event.key === "F10" && event.shiftKey)) return;
      event.preventDefault();
      const box = event.currentTarget.getBoundingClientRect();
      openAt({ x: box.left + 16, y: box.top + 16 });
    },
  };
  const button = <MenuButton name={name} open={place !== undefined} onOpen={(at) => openAt(at)} />;
  const menu = place ? <RowMenu items={items} place={place} name={name} onClose={close} /> : null;
  return { rowProps, button, menu };
}

function MenuButton({ name, open, onOpen }: { name: string; open: boolean; onOpen: (at: Place) => void }) {
  const t = useTranslate();
  // reached with the Menu key on the row, or with a click: the list has one tab stop, not one per button
  return <button type="button" className="row-menu-button" aria-haspopup="menu" aria-expanded={open} tabIndex={-1}
    aria-label={t("row_menu_named", { name })}
    onClick={(event) => {
      const box = event.currentTarget.getBoundingClientRect();
      onOpen({ x: box.right, y: box.bottom });
    }}>⋯</button>;
}

function RowMenu({ items, place, name, onClose }: { items: readonly MenuItem[]; place: Place; name: string; onClose: () => void }) {
  const t = useTranslate();
  const id = useId();
  const box = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState(place);

  // Kept inside the window: a menu opened near an edge moves back in.
  useLayoutEffect(() => {
    const menu = box.current;
    if (!menu) return;
    const { width, height } = menu.getBoundingClientRect();
    setAt({ x: Math.max(4, Math.min(place.x, window.innerWidth - width - 4)), y: Math.max(4, Math.min(place.y, window.innerHeight - height - 4)) });
    menu.querySelector<HTMLButtonElement>("[role=menuitem]:not(:disabled)")?.focus();
  }, [place]);

  // A press anywhere else closes it, as does the window losing its place under it.
  useEffect(() => {
    const away = (event: Event) => {
      if (!box.current?.contains(event.target as Node)) onClose();
    };
    document.addEventListener("pointerdown", away, true);
    window.addEventListener("resize", onClose);
    return () => {
      document.removeEventListener("pointerdown", away, true);
      window.removeEventListener("resize", onClose);
    };
  }, [onClose]);

  const move = (event: KeyboardEvent<HTMLDivElement>) => {
    const entries = [...(box.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]:not(:disabled)") ?? [])];
    const now = entries.indexOf(document.activeElement as HTMLButtonElement);
    const go = (index: number) => entries[(index + entries.length) % entries.length]?.focus();
    if (event.key === "Escape" || event.key === "Tab") { event.preventDefault(); onClose(); }
    else if (event.key === "ArrowDown") { event.preventDefault(); go(now + 1); }
    else if (event.key === "ArrowUp") { event.preventDefault(); go(now - 1); }
    else if (event.key === "Home") { event.preventDefault(); go(0); }
    else if (event.key === "End") { event.preventDefault(); go(entries.length - 1); }
  };

  // On the page itself, so it sits at the pointer whatever box the row is in.
  return createPortal(<div ref={box} id={id} role="menu" aria-label={t("row_menu_named", { name })} className="row-menu"
    style={{ left: at.x, top: at.y }} onKeyDown={move}>
    {items.map((item) => <button key={item.label} type="button" role="menuitem" disabled={item.disabled}
      onClick={() => { onClose(); item.run(); }}>{item.label}</button>)}
  </div>, document.body);
}

// A list row that carries a menu: focusable, so the keyboard can reach the Menu key on it. A list of
// rows hands the tab stop to one of them (the others are -1, still reached by the arrows or a click).
export function MenuRow({ menu, children, tabIndex = 0, onFocus, onActivate }: {
  menu: ReturnType<typeof useRowMenu>; children: ReactNode; tabIndex?: number; onFocus?: () => void;
  // Enter on the row itself: what the row's title does when it is clicked.
  onActivate?: () => void;
}) {
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    menu.rowProps.onKeyDown(event);
    if (event.defaultPrevented || event.key !== "Enter" || event.target !== event.currentTarget || !onActivate) return;
    event.preventDefault();
    onActivate();
  };
  return <li tabIndex={tabIndex} className="menu-row" onFocus={onFocus} {...menu.rowProps} onKeyDown={onKeyDown}>
    {children}
    {menu.button}
    {menu.menu}
  </li>;
}
