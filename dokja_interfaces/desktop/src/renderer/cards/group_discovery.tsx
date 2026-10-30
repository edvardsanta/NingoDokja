import { useState } from "react";

import type { CommunicationChannel } from "../../shared/replies.js";
import { useTranslate } from "../i18n/context.js";
import { KnownPicker } from "./known_picker.js";
import type { Groups, Lookup } from "./settings_lookups.js";

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
// and saves it.
export function GroupDiscovery({ groups, chosen, disabled, onAdd }: {
  groups: Groups;
  chosen: readonly string[];
  disabled: boolean;
  onAdd: (group: { id: string; name: string }) => void;
}) {
  const t = useTranslate();
  const [shown, setShown] = useState(false);
  const loading = groups.phase === "loading";
  const failed = groups.asked && groups.phase === "unavailable";
  return <div className="cfg-discovery">
    <button type="button" className="compact" disabled={disabled || loading} onClick={() => { setShown(true); void groups.discover(); }}>{t("groups_discover")}</button>
    {shown && loading && <p role="status" className="detail">{t("groups_discovering")}</p>}
    {shown && failed && <p role="alert">{t("groups_not_connected")}</p>}
    {shown && groups.phase === "ready" && groups.items.length === 0 && <p className="detail">{t("groups_none")}</p>}
    {shown && groups.phase === "ready" && groups.items.length > 0 && <KnownPicker label={t("groups_found")} chosen={chosen} disabled={disabled}
      items={groups.items.map((group) => ({ key: group.id, title: group.name || t("groups_unnamed"), detail: [t("groups_members", { count: String(group.members) })] }))}
      onAdd={(key) => { const group = groups.items.find((item) => item.id === key); if (group) onAdd(group); }} />}
  </div>;
}

// The Discord channels Ningo can see, by name and server, to add one without copying its id.
export function ChannelPicker({ channels, chosen, disabled, onAdd }: {
  channels: Lookup<CommunicationChannel>;
  chosen: readonly string[];
  disabled: boolean;
  onAdd: (id: string) => void;
}) {
  const t = useTranslate();
  const [shown, setShown] = useState(false);
  const named = channels.items.filter((channel) => channel.name && channel.name !== channel.id);
  if (channels.phase !== "ready" || named.length === 0) return null;
  return <div className="cfg-discovery">
    <button type="button" className="compact" aria-expanded={shown} disabled={disabled} onClick={() => setShown(!shown)}>{t("channels_choose")}</button>
    {shown && <KnownPicker label={t("channels_found")} chosen={chosen} disabled={disabled}
      items={named.map((channel) => ({ key: channel.id, title: `#${channel.name}`, detail: [...(channel.guildName ? [channel.guildName] : []), channel.id] }))}
      onAdd={onAdd} />}
  </div>;
}
