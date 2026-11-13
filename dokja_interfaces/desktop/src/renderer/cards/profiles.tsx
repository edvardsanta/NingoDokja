import { useCallback, useState } from "react";

import type { PersonProfile, PersonProfiles } from "../../shared/replies.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useNumbers, useTimes, useTranslate } from "../i18n/context.js";
import { CardFrame, Problem } from "./card_frame.js";
import { Button } from "./ui.js";
import { useCard, type LoadResult } from "./use_card.js";
import { WorkspacePanel } from "./workspace_panel.js";

// Inferring one profile asks a model to read dozens of exchanges; the orchestrator allows its
// analysis task up to ninety seconds, so wait a little longer than that.
const INFERENCE_TIMEOUT_MS = 100_000;

async function loadProfiles(transport: Transport): Promise<LoadResult<PersonProfiles>> {
  const answer = await transport.request("conversation.profiles", {});
  if (!answer.ok) return answer;
  const result = answer.result;
  if (!isRecord(result) || !Array.isArray(result.profiles)) return { ok: false, error: { code: "unexpected", message: "unexpected profiles reply" } };
  return { ok: true, data: result as PersonProfiles };
}

function List({ label, items }: { label: string; items: string[] }) {
  if (items.length === 0) return null;
  return (
    <div className="profile-list">
      <h4>{label}</h4>
      <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul>
    </div>
  );
}

function Person({ person, min, inference, working, onGenerate }: {
  person: PersonProfile; min: number; inference: boolean; working: boolean; onGenerate: (person: PersonProfile) => void;
}) {
  const t = useTranslate();
  const times = useTimes();
  const numbers = useNumbers();
  const name = person.name || (person.owner ? t("facts_owner") : person.person);
  const enough = person.answered >= min;
  const reading = person.profile;
  return (
    <section className="profile" aria-label={name}>
      <h3>{name}{person.realName && <span className="detail"> · {t("profiles_real_name", { name: person.realName })}</span>}</h3>
      <p className="detail profile-counts">
        {t("profiles_counts", { turns: person.turns, answered: person.answered, voice: person.voiceTurns, reply: numbers.fixed(person.medianReply, 0) })}
      </p>
      {reading ? (
        <div className="profile-reading">
          <p className="profile-summary">{reading.summary}</p>
          {reading.tone && <p><strong>{t("profiles_tone")}:</strong> {reading.tone}</p>}
          <List label={t("profiles_topics")} items={reading.topics} />
          <List label={t("profiles_patterns")} items={reading.patterns} />
          <p className="detail">
            {reading.model
              ? t("profiles_provenance", { when: times.day(reading.generatedAt), count: reading.exchanges, model: reading.model })
              : t("profiles_provenance_unnamed", { when: times.day(reading.generatedAt), count: reading.exchanges })}
          </p>
        </div>
      ) : (
        <p className="detail">{enough ? t("profiles_none_yet") : t("profiles_need", { min, answered: person.answered })}</p>
      )}
      <Button variant="quiet" size="compact" disabled={working || !enough || !inference} aria-label={t("profiles_generate_named", { name })}
        title={!inference ? t("profiles_no_inference") : !enough ? t("profiles_need", { min, answered: person.answered }) : undefined}
        onClick={() => onGenerate(person)}>
        {working ? t("profiles_working") : reading ? t("profiles_again") : t("profiles_generate")}
      </Button>
    </section>
  );
}

// How Ningo deals with each person the owner introduced, and the owner. The counts are read off the
// stored conversations. The text is a model's reading of those exchanges, so it is not deterministic:
// it is made on request, says when and by which model, and is replaced when asked again.
export function ProfilesPanel({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadProfiles(transport), [transport]);
  const { state, reload, poll } = useCard(load);
  const [working, setWorking] = useState<string>();
  const [problem, setProblem] = useState<TransportError>();

  const generate = async (person: PersonProfile) => {
    if (working) return;
    setWorking(person.person); setProblem(undefined);
    const answer = await transport.request("conversation.profile_generate", { person: person.person }, { timeoutMs: INFERENCE_TIMEOUT_MS });
    setWorking(undefined);
    if (answer.ok) poll();
    else if (answer.error.code !== "cancelled") setProblem(answer.error);
  };

  return (
    <CardFrame title={t("profiles_title")} state={state} onReload={reload} onRefresh={poll}>
      {(data) => <>
        <p className="detail kept-about">{t("profiles_about")}</p>
        {problem && <Problem error={problem} />}
        {data.profiles.length === 0 && <p className="detail">{t("profiles_empty")}</p>}
        {data.profiles.map((person) => (
          <Person key={person.person} person={person} min={data.minExchanges} inference={data.inference}
            working={working === person.person} onGenerate={(chosen) => void generate(chosen)} />
        ))}
      </>}
    </CardFrame>
  );
}

export function ProfilesCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  return <WorkspacePanel trail={[t("nav_profiles")]}><ProfilesPanel transport={transport} /></WorkspacePanel>;
}
