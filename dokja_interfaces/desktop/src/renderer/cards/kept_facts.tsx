import { useCallback, useState } from "react";

import type { KeptFact, KeptFacts } from "../../shared/replies.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { CardFrame, Problem } from "./card_frame.js";
import { Button } from "./ui.js";
import { useCard, type LoadResult } from "./use_card.js";

// What known people asked Ningo to keep. It stands apart from the experience memory: it lives with the
// conversations, so it stays visible and deletable while that service is off.
async function loadFacts(transport: Transport): Promise<LoadResult<KeptFact[]>> {
  const answer = await transport.request("conversation.facts", {});
  if (!answer.ok) return answer;
  const facts = isRecord(answer.result) && Array.isArray(answer.result.facts) ? (answer.result as KeptFacts).facts : undefined;
  if (!facts) return { ok: false, error: { code: "unexpected", message: "unexpected facts reply" } };
  return { ok: true, data: facts };
}

function byPerson(facts: KeptFact[]): [string, KeptFact[]][] {
  const people = new Map<string, KeptFact[]>();
  for (const fact of facts) {
    const key = fact.owner ? "" : fact.person;
    people.set(key, [...(people.get(key) ?? []), fact]);
  }
  return [...people.entries()].sort(([left], [right]) => left.localeCompare(right));
}

export function KeptFactsPanel({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const times = useTimes();
  const load = useCallback(() => loadFacts(transport), [transport]);
  const { state, reload } = useCard(load);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<TransportError>();

  const remove = async (fact: KeptFact) => {
    if (working) return;
    setWorking(true); setProblem(undefined);
    const answer = await transport.request("conversation.fact_delete", { fact_id: fact.id });
    setWorking(false);
    if (answer.ok) reload();
    else if (answer.error.code !== "cancelled") setProblem(answer.error);
  };

  const placeOf = (fact: KeptFact) => fact.place === "private" ? t("facts_place_private") : t("facts_place_shared", { place: fact.place });

  return (
    <CardFrame title={t("facts_title")} state={state} onReload={reload}>
      {(facts) => <>
        <p className="detail">{t("facts_about")}</p>
        {problem && <Problem error={problem} />}
        {facts.length === 0 && <p className="detail">{t("facts_empty")}</p>}
        {byPerson(facts).map(([person, held]) => (
          <section key={person || "owner"} className="kept-person" aria-label={person || t("facts_owner")}>
            <h3>{person || t("facts_owner")}</h3>
            <ul className="kept-facts">
              {held.map((fact) => (
                <li key={fact.id}>
                  <p className="kept-fact">{fact.content}</p>
                  <p className="detail">{placeOf(fact)} · {times.day(fact.createdAt)}</p>
                  <Button variant="quiet" size="compact" disabled={working} aria-label={t("facts_delete_named", { fact: fact.content })}
                    title={t("facts_delete_named", { fact: fact.content })} onClick={() => void remove(fact)}>{t("facts_delete")}</Button>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </>}
    </CardFrame>
  );
}
