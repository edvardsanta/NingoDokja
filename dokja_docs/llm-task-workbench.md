# LLM task workbench

Internal task inference keeps the existing flow: interface event → Runtime
orchestrator → Runtime domain → inference service. Ordinary conversation keeps
its explicit chat binding. These controls apply to the shared task adapter.

## Capacity and cancellation

One Runtime admits two task generations simultaneously and one per provider.
At most 64 calls wait for capacity. Waiting does not hold a provider recovery
probe. Both queueing and generation use the request context, so cancellation or
expiry releases capacity and reaches the inference adapter. The whole shared
client task has a 90-second ceiling, including waiting and fallback; a shorter
caller deadline wins. The price workflow also enforces its 165-second deadline.
A single attempt gets at most 15 seconds for `lightweight` or 45 for `analysis`,
and a fair share of the remaining caller budget. Capacity and health are local
to one Runtime process, not a distributed quota across independent controllers.

## Profiles

System → Inference providers → Tasks and fallback exposes ordered priorities
for `lightweight` and `analysis`. Add registered models, move them up, remove
them and save. Each profile accepts at most 16 distinct provider/model pairs.
An empty list uses automatic selection in the existing provider catalog order.
Lightweight selection prefers the registered small-model heuristic; automatic
analysis selects the highest rank within each registration. Names are a
heuristic, not evidence of model capability: use explicit priorities when the
operator knows which models suit a workload.

Explicit lists define the entire fallback chain. Disabled or removed models
are skipped without silently adding another model. A list whose entries are all
unavailable reports an inventory failure. Legacy price model environment
aliases apply only to automatically selected lightweight bindings.

Settings use the existing host settings table under `runtime.tasks.v1`, with an
independent revision and compare-and-swap writes. No table migration or change
to provider/chat settings is required. The Runtime domain validates registration
membership; services persist configuration without choosing models.

The additive bootstrap actions are `runtime.tasks.profiles`,
`runtime.tasks.configure` and `runtime.tasks.diagnostics`. Configuration carries
`configuration_revision` and `profiles` with both profile arrays. Private task
inference uses `GET`/`POST /chat/tasks/{profile}`. Technical comparison reviews
use `analysis`; discovery, short purchase comments, music remarks and
questionnaire tasks retain `lightweight`.

## Diagnostics

Opening the desktop panel requests a snapshot; Refresh updates it. The view
shows active calls, queued calls, the concurrency limit, paused providers and
the latest 100 reported attempts, newest first. Each attempt includes its task
id, stage, profile, provider, model, start time, duration, outcome and safe reason.
A cooldown includes its categorized cause and remaining delay at snapshot time.

The shared client reports the same metadata it already logs to private
`POST /chat/task-attempts`, after JSON and semantic validation. This captures
invalid JSON and contract rejection as fallback causes. Reporting is best-effort
with a 200 ms budget independent of cancellation; failure to report does not
invalidate an otherwise successful task. A controller outage can therefore leave
an incomplete desktop history; orchestrator logs remain available. Snapshots
never retain prompts, completions, raw provider errors or validator messages.
The ring and provider cooldowns reset when the controller restarts. All new
screen labels have English and Portuguese locale entries.

## Price context and technical sources

Discord comparison accepts optional `intended_use` (`uso`) and
`current_equipment` (`equipamento_atual`), each at most 500 characters. Existing
product, link and budget calls remain valid. The bridge forwards budget cents
and the new fields. The Price domain validates context, then the orchestrator
includes it in fresh technical review and purchase-comment facts. Missing
components remain unknown; the reference product is not assumed to be installed.
Context and model opinions do not enter competitor-discovery caches.

Collectors may supply `specifications.technical_sources` in
`price.observation.v1`. Each product accepts at most five entries:

```json
{
  "title": "Example test lab",
  "url": "https://bench.example/review",
  "observed_at": "2026-11-11T10:00:00Z",
  "workload": "1080p gaming",
  "excerpt": "Collector-extracted results for this workload."
}
```

Title, URL and RFC3339 observation time are required. Optional workload and
excerpt are limited to 120 and 1200 characters. URLs must use HTTP(S) without
credentials. Sources remain factual collector observations in the existing
specification JSON, with product attribution. They are not model-generated
opinions. Old observations without sources remain valid.

The model may return `source_urls` only from collected references with excerpts;
unknown URLs and unread references reject that attempt and allow fallback.
The prompt treats excerpts and equipment text as untrusted data. A citation does
not independently certify a benchmark or establish equivalence: model reviews
remain labeled qualitative estimates. Discord links collected references and
shows their workload and observation date. Without collected sources it states
that the analysis is an estimate. The public repository does not include owner
collectors, and this change does not add an automatic web fetcher; collectors
must provide the technical excerpts before sourced analysis is available.

Rebuild the desktop, host controller, orchestrator and Discord adapter; refresh
Discord command registration for the optional fields. Deploy the controller
before callers using `analysis` and metadata reporting. Existing event envelopes,
SQLite tables, HTTP chat calls and existing command options are unchanged.
No port, environment variable, Compose service or volume is introduced.
