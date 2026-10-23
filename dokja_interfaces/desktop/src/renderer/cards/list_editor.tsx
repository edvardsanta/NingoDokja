import { useId, useState } from "react";

import { useTranslate } from "../i18n/context.js";

type Props = {
  label: string;
  values: string[];
  onChange: (values: string[]) => void;
  disabled?: boolean;
  maxItems?: number;
};

export function ListEditor({ label, values, onChange, disabled = false, maxItems }: Props) {
  const t = useTranslate();
  const id = useId();
  const [entry, setEntry] = useState("");
  const candidate = entry.trim();
  const canAdd = candidate !== "" && !values.includes(candidate) && (maxItems === undefined || values.length < maxItems);
  const add = () => {
    if (!canAdd || disabled) return;
    onChange([...values, candidate]);
    setEntry("");
  };

  return <div className="list-editor">
    <label htmlFor={id}>{label}</label>
    {values.length > 0 ? <ul className="list-editor-items">
      {values.map((value) => <li key={value}>
        <span>{value}</span>
        <button type="button" disabled={disabled} aria-label={t("list_remove_named", { name: value })} onClick={() => onChange(values.filter((item) => item !== value))}>{t("list_remove")}</button>
      </li>)}
    </ul> : <p className="detail list-editor-empty">{t("setting_none")}</p>}
    <div className="list-editor-add">
      <input id={id} value={entry} maxLength={128} autoComplete="off" disabled={disabled} onChange={(event) => setEntry(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); add(); } }} />
      <button type="button" disabled={disabled || !canAdd} onClick={add}>{t("list_add")}</button>
    </div>
  </div>;
}
