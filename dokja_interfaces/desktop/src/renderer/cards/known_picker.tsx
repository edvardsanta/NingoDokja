import { useId, useState } from "react";

import { useTranslate } from "../i18n/context.js";

export type KnownItem = { key: string; title: string; detail?: readonly string[] };

// A long list is narrowed by typing, so it never grows past what fits on a screen.
const SHOWN = 8;

// What the owner may choose from, by name: each item with its Add button. Adding only fills the
// draft of the setting; nothing is saved until the owner reviews and saves it.
export function KnownPicker({ label, items, chosen, disabled, onAdd }: {
  label: string;
  items: readonly KnownItem[];
  chosen: readonly string[];
  disabled: boolean;
  onAdd: (key: string) => void;
}) {
  const t = useTranslate();
  const id = useId();
  const [filter, setFilter] = useState("");
  const needle = filter.trim().toLowerCase();
  const matching = needle ? items.filter((item) => [item.title, ...(item.detail ?? [])].join(" ").toLowerCase().includes(needle)) : items;
  const shown = matching.slice(0, SHOWN);
  return <div className="cfg-known">
    {items.length > SHOWN && <div className="cfg-filter">
      <label htmlFor={id}>{t("known_filter")}</label>
      <input id={id} value={filter} autoComplete="off" onChange={(event) => setFilter(event.target.value)} />
    </div>}
    {shown.length > 0
      ? <ul className="cfg-known-list" aria-label={label}>{shown.map((item) => {
        const added = chosen.includes(item.key);
        return <li key={item.key}>
          <span className="cfg-entry-text">
            <strong className="cfg-entry-title">{item.title}</strong>
            {(item.detail ?? []).map((part, at) => <span key={at} className="detail cfg-entry-detail">{part}</span>)}
          </span>
          <button type="button" className="compact" disabled={disabled || added} aria-label={t("known_add_named", { name: item.title })} onClick={() => onAdd(item.key)}>{added ? t("known_added") : t("list_add")}</button>
        </li>;
      })}</ul>
      : <p className="detail">{t("known_none")}</p>}
    {matching.length > shown.length && <p className="detail">{t("known_more", { count: SHOWN })}</p>}
  </div>;
}
