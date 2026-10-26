# Measured inference and token acceptance budgets

Step `08.2` adds an opt-in measured path alongside existing chat and
[Knowledge proposals](knowledge-provider-proposals.md). It reports provider token
counts and withholds results that exceed explicit acceptance limits. These checks
run **after generation**; they do not limit billed tokens, stop generation at a
token boundary, or prove total account usage. No live comparison has been run.

## Ownership and compatibility

The private Runtime inference socket now accepts `POST /chat/measured` with
`messages` and `budget` (`max_input_tokens`, `max_output_tokens`). The Runtime
domain validates the selected provider/model, message limits and positive budget;
then it asks the existing isolated adapter exactly once. No fallback or repair
call is added. The request parser rejects unknown fields and trailing data.
Existing `/chat`, `/chat/stream` and their response shapes remain unchanged.

The adapter advertises measured support for the two registered command-based
providers whose structured output contains usage: `codex` and `claude_code`.
The current `ollama run` transport supplies plain text without structured counts,
so measured calls using that provider are refused before launch. Existing
unmeasured calls still support it. A supported provider can still return missing
or malformed usage; that reply is withheld after the attempt rather than assigned
zero tokens or passed to another provider.

The Runtime response includes status, provider ID, requested model, configuration
revision, `budget_mode: post_generation`, budget, usage and a call count. It
contains a reply only when all acceptance checks succeed. Status values are:

| Status | Meaning |
| --- | --- |
| `ok` | Nonempty bounded reply and complete nonnegative counts within budget |
| `over_budget` | Input or output count exceeded its limit; retain usage, omit reply |
| `usage_unavailable` | Missing, invalid or inconsistent counts; omit reply |
| `provider_error` | Provider execution/protocol failed; omit reply and private diagnostic |
| `invalid_reply` | Empty or oversized reply; omit reply |

Invalid request/configuration, unsupported measured inference and cancellation
return errors before accepting any reply. Cancellation discards late results;
provider work may have an unknown outcome. There is no persistent attempt ledger
or billing reconciliation in this batch.

## Usage semantics

All usage fields are nullable integers. `input_tokens` and `output_tokens` are
required for acceptance. A reported zero is distinct from a missing field.
Negative/fractional/malformed counters, impossible cache subsets, overflow and
multiple completed-turn envelopes do not become accepted counts.

For `codex`, use the single `turn.completed.usage.input_tokens` total directly;
retain `cached_input_tokens` and optional `cache_write_input_tokens` as subsets,
without adding them to the total again. For `claude_code`, total input is the sum
of reported `input_tokens`, `cache_read_input_tokens` and
`cache_creation_input_tokens`; all three fields are required to construct that
total. Arithmetic is checked for overflow. Output uses the reported
`output_tokens`; it is not estimated from the visible answer. Cache subdivisions
remain separate metadata, and missing subdivisions stay null where the reported
total is independently available.

Counts describe the provider's reported inference envelope, not hidden account
activity or a trusted invoice. They include the CLI's own reported prompt
context, not just Knowledge document bytes. The requested model is retained;
the adapter does not claim that it independently observed the resolved model.

The parsing conventions follow the providers' structured usage definitions,
verified during implementation; protocol identifiers are recorded here rather
than embedding external endpoints as application defaults. No credentials or
new provider settings are required by this change.

## Knowledge application entry point

`knowledge.application.propose.measured` is a new orchestrator event/action. It
uses the exact existing preparation payload (`knowledge.application.context.v1`,
goal, constraints, selected sources and optional text pins). It is wired to the
measured Runtime port with **12000 input / 2000 output tokens** as result acceptance
ceilings. Ordinary `knowledge.application.propose` remains unmeasured and retains
its behavior; there is no automatic fallback between the two paths.

The normal source/task/quote/semantic rules still apply. On an accepted measured
reply, Knowledge checks task/revision binding, anchors unique quotations and runs
the argument assessor. `semantic_status` stays `unverified`, execution remains
disabled and experience remains unconsulted. Counting tokens establishes no
knowledge, applicability or observed outcome.

The returned inference metadata carries counts and `budget_mode`/`budget_status`.
A rejected result retains its reported usage without exposing a proposal or source
bodies. Stable task reasons include `token_budget_exceeded` and
`token_usage_unavailable`. A failed/unknown attempt may have null usage; do not
sum it as zero. The Knowledge client independently checks counts against its
ceilings even if Runtime incorrectly labels the response `ok`.

| Bound | Value |
| --- | --- |
| Runtime messages / content | Existing 128-message / 64 KiB content bounds |
| Private measured HTTP request | 128 KiB |
| Runtime acceptance budget range | Input 1..1000000; output 1..65536 tokens |
| Knowledge acceptance budget | Input 12000; output 2000 tokens |
| Runtime deadline | 90 seconds or earlier caller deadline |
| Knowledge provider step / whole task | Existing 60 / 90 seconds |
| Provider attempts | At most one; unsupported adapters launch none |
| Canonical knowledge/episode writes / external actions | None |

The Runtime controller and main orchestrator need the matching update for this
new action. Older Runtime versions refuse the measured endpoint; the caller does
not fall back. No port, environment variable, Compose service or database schema
changes. Running services and user data are untouched by implementation/tests.

## Verification and remaining gate

Domain tests cover exact-boundary acceptance, over-budget rejection with retained
usage, missing/negative/inconsistent counts, invalid budgets/messages, unsupported
providers, ordinary chat compatibility and cancellation. Adapter tests verify
cache accounting, overflow, missing fields, single-turn envelopes, no launch for
unsupported providers and unchanged isolation flags. HTTP/socket tests verify
request parsing, budget forwarding, withheld text and independent client checks.
Application tests verify the separate measured action, retained rejection metrics
and absence of an unmeasured fallback.

Runtime service and transport race suites pass. Runtime domain race suite passed
on rerun after one timing-sensitive lifecycle failure in the initial parallel run.
Knowledge domain and focused orchestrator client/handler race tests pass. Static
checks pass in all five touched Go modules. The full main-orchestrator suite still
fails `TestEveryBanterTurnKeepsToneIntentAndTrustBoundaries`; the same failure was
reproduced on an unchanged checkout of base `b4cfddd` (missing persona text).
It is unrelated to measured inference and is not reported as a passing suite.

The source/case fixtures and rubric frozen in `8b9f331` are unchanged. Its token
rule records overflow as a failure; this path implements that result acceptance
check for complete supported-provider usage. It does not promise pre-generation
resource or billing caps. Such caps are separate from that frozen rule.

Next, build/run the comparison harness with recorded provider/model/settings,
complete/unknown failure accounting, source versions and independently reviewed
semantic results. The adaptive retrieval strategy also remains unimplemented;
do not compare it as if available or tune prompts on held-out answers. No live
comparison or semantic-quality result is claimed in this batch. Actual outcomes,
retrieval/revision and durable eligible episode reuse remain open in `08.2`/`08.3`.

Sources in the repository: [Runtime policy](../dokja_domain/dokja_runtime/measured_chat.go),
[provider accounting](../dokja_services/dokja_runtime/measured_chat.go),
[private HTTP transport](../dokja_orch/runtime/chat.go),
[Knowledge client](../dokja_orch/internal/clients/measured_knowledge_proposal_client.go),
and [application workflow](../dokja_orch/internal/handlers/knowledge_application.go).
