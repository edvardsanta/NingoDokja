import { useState } from "react";
import type { Transport } from "../../shared/transport.js";
import type { LinkedGroup } from "../../shared/replies.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";

// The alias a named conversation goes by, made from the group's name: lowercase letters, digits, "-"
// and "_", up to 32, and different from the aliases already taken.
export function conversationAlias(name: string, taken: readonly string[]): string {
  const base = name.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().replace(/[^a-z0-9_-]+/g, "-").replace(/^[-_]+|-+$/g, "").slice(0, 28).replace(/-+$/, "") || "grupo";
  let alias = base;
  for (let n = 2; taken.includes(alias); n++) alias = `${base}-${n}`;
  return alias;
}

// Lists the groups the linked WhatsApp account is in, so the owner chooses one by its name instead of
// hunting for its id. Adding only fills the setting's draft; nothing is saved until the owner reviews
// and saves the setting as usual.
export function GroupDiscovery({ transport, chosen, disabled, onAdd }: {
  transport: Transport;
  chosen: readonly string[];
  disabled: boolean;
  onAdd: (group: LinkedGroup) => void;
}) {
  const t = useTranslate();
  const [groups, setGroups] = useState<LinkedGroup[]>();
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);

  const discover = async () => {
    setLoading(true); setFailed(false);
    const answer = await transport.request("communications.link.groups", { interface: "whatsapp" }).catch(() => undefined);
    const listed = answer?.ok && isRecord(answer.result) && Array.isArray(answer.result.groups) ? (answer.result.groups as LinkedGroup[]) : undefined;
    if (listed) setGroups(listed); else setFailed(true);
    setLoading(false);
  };

  return <div className="group-discovery">
    <button type="button" disabled={disabled || loading} onClick={() => void discover()}>{t("groups_discover")}</button>
    {loading && <p role="status" className="detail">{t("groups_discovering")}</p>}
    {failed && <p role="alert">{t("groups_not_connected")}</p>}
    {groups && groups.length === 0 && <p className="detail">{t("groups_none")}</p>}
    {groups && groups.length > 0 && <ul className="group-discovery-list" aria-label={t("groups_found")}>{groups.map((group) => {
      const name = group.name || t("groups_unnamed");
      const added = chosen.includes(group.id);
      return <li key={group.id}>
        <span><strong>{name}</strong> <span className="detail">{t("groups_members", { count: String(group.members) })}</span></span>
        <button type="button" disabled={disabled || added} aria-label={t("groups_add_named", { name })} onClick={() => onAdd(group)}>{added ? t("groups_added") : t("groups_add")}</button>
      </li>;
    })}</ul>}
  </div>;
}
