import { useCallback, useId, useState } from "react";
import type { ProvidersView } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame } from "./card_frame.js";
import { useCard, type LoadResult } from "./use_card.js";

export function ProvidersCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const id = useId();
  const [inspecting, setInspecting] = useState(false);
  if (inspecting) return <ProviderInventory transport={transport} />;
  return <section className="card" aria-labelledby={id}>
    <span className="card-kind">{t("nav_system")}</span>
    <h2 id={id}>{t("providers_title")}</h2>
    <p className="detail">{t("providers_discovery_only")}</p>
    <button type="button" onClick={() => setInspecting(true)}>{t("providers_inspect")}</button>
  </section>;
}

function ProviderInventory({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<ProvidersView>> => {
    const answer = await transport.request("runtime.providers", {}, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.providers)) return { ok: false, error: { code: "unexpected", message: "unexpected provider inventory" } };
    return { ok: true, data: answer.result as ProvidersView };
  }, [transport]);
  const { state, reload } = useCard(load);
  return <CardFrame kindLabel={t("nav_system")} title={t("providers_title")} state={state} onReload={reload}>
    {(data) => <>
      <p className="detail">{t("providers_discovery_only")}</p>
      <ul className="services">{data.providers.map((row) => <li key={row.id}>
        <span className="service-name">{row.name}</span>
        <span className="service-status">{t(row.reason === "service_stopped" ? "provider_service_stopped" : row.state === "installed" ? "provider_installed" : row.state === "reachable" ? "provider_reachable" : "provider_unavailable")}</span>
        <span className="detail">{row.modelDiscovery === "listed"
          ? row.models.length ? row.models.join(", ") : t("provider_no_models")
          : t("provider_models_unavailable")}</span>
        {row.truncated && <span className="detail">{t("provider_models_truncated")}</span>}
      </li>)}</ul>
    </>}
  </CardFrame>;
}
