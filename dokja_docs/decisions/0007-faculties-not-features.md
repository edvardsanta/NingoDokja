# ADR 0007: macro-components are faculties; features are workflows

Date: 2026-10-18. Status: accepted as the target direction. Migration: pending.
[ADR 0010](./0010-four-concepts.md) proposes replacing its faculty list with four concepts.
Generalizes [ADR 0006](./0006-reading-macro-component.md).
Related: [decision index](./README.md), [workspace](./0001-task-workspace.md),
[capability map](../capability-map.md),
[research](../cognitive-macrocomponents-research.md).

## Context

The current code is cut by feature: `dokja_book`, `dokja_meme`, `dokja_digest`
and the former language integration each bundle reading, storage, judgment and output for one
product. ADR 0006 showed that Book is a reading plus stored knowledge plus an
experience, not a capability of its own. The owner confirmed the same holds for
Meme: it is not a macro-component.

The [Meme service](../../dokja_services/dokja_meme/service.py) shows why. It
mixes:

- scrapers and a refreshed pool of collected items: scheduled reading with a
  cache, the same shape as Feeds;
- OCR and attachment fetching: reading;
- NSFW screening: an evaluation signal that moderation policy decides on;
- `mark_sent` and unsent filtering: receipts of a delivery action;
- hashtag labels and acceptance feedback: already moved to Knowledge and
  Experience by the hashtag refactor.

Digest is a selection policy (freshness, ordering, duplicates, source balance)
over what Feeds read. Chat is language generation inside the loop, as the chat
plan already proposes.

## Decision

A macro-component is a **faculty**: one broad, stable capability with its own
contract, extended by plugins for specific sources, formats, models or
platforms. A product feature is **not** a macro-component. It is a workflow that
runs the cycle across faculties, plus the policy parameters for that case.

| Faculty | Owns | Starting point |
| --- | --- | --- |
| Reading | Acquiring and extracting content from any source, scheduled or on demand | Feeds host (ADR 0006); meme scrapers, OCR and attachments, Knowledge extraction, voice transcription migrate in |
| Knowledge | The document contract, chunking, embedding, storage, retrieval, general methods | Knowledge domain and service |
| Experience | Episodes: what was done, in which context, with what outcome, including delivery receipts | Memory domain and service |
| Prediction | Estimating a named outcome before acting and scoring it afterwards | Memory domain prediction (ADR 0004) |
| Moderation | Is this admissible? Allow, reject, review, or allow with constraints, under safety, content and platform rules | Moderation domain; meme NSFW screening |
| Attention | What deserves priority? Score, rank and select by relevance, freshness, diversity and novelty | Digest ordering; meme hashtag relevance threshold |
| Language | Generating text | Chat domain and generation providers |
| Action | Sending, posting, replying on a platform | Communications domain and service |
| Coordination | The task workspace and the cycle; sequencing only | Orchestrator core (ADR 0001) |

Under this model:

- **Meme** = scheduled reading of meme sources (the pool is a reading cache) →
  moderation (screening) → knowledge (hashtag labels) → attention (relevance) → prediction (shadow) → action (delivery) →
  experience (sent receipts, feedback).
- **Digest** = reading (feeds) → attention (ordering) → language or direct
  rendering → action.
- **Book** = on-demand reading → knowledge → general method + experience →
  language → action → experience (ADR 0006).

## Where feature policy lives

The mandatory flow stays `Interface → Event → Orchestrator → Domain → Service`.
What changes is what a domain is: domains become faculties.

- A **workflow recipe** names the steps and binds parameters. It is owned and
  run by the orchestrator, and it contains no decisions of its own.
- A **decision inside a step** (is this meme admissible, which digest items to
  show, is this hashtag relevant) belongs to the faculty domain that makes that
  kind of decision, parameterized by the recipe. For example, the hashtag
  `min_score` becomes a selection parameter of the meme recipe, not a constant
  in a meme handler.
- Moderation and attention are separate faculties even while they share
  infrastructure (classifiers, scoring helpers): one answers whether something
  may be used, the other how much priority it gets. An item moderation rejects
  never reaches attention; attention never overrides a moderation verdict.
- Feature-specific code that is genuinely unique (a source format, a platform
  quirk) is a plugin of the relevant faculty, not a feature domain.

This keeps the rule that the orchestrator holds no business rules: recipes
sequence and configure; faculties decide.

## Workflow recipes

Declarative recipes are the target, but replacing Go handlers is an outcome of
the migration, not its prerequisite. Three stages:

1. **Current:** feature-specific Go handlers. Each knows its own sequence and
   calls the existing domains.
2. **Transitional:** typed workflows defined in Go. Book, Meme and Digest are
   composed from reusable faculty steps with typed inputs, outputs and policy
   parameters, and the orchestrator executes them.
3. **Target:** versioned recipes interpreted by the orchestrator. The same steps
   and parameters are data; Go implements the faculty operations, adapters and
   workflow engine, not each product's business sequence.

A feature moves to stage 3 only after it has run on stage 2 behind its baseline
tests.

## Migration and compatibility

No code moves in this record. Existing feature domains and services stay as
compatibility facades until each migration is proven:

1. Book first, through ADR 0006 (steps `07` and `09`).
2. Meme: its scrapers become reading-host plugins and its pool a reading cache,
   like the Feeds cache; screening moves behind moderation; sent receipts become experience. Existing meme events,
   CLI and Discord commands and the scheduler triggers keep working through a
   facade.
3. Digest: its ordering policy moves under attention; `digest.*` stays.
4. Chat: follows the chat plan, which already treats it as a loop faculty.

Each step reviews the event envelope, routes and actions, service contracts,
interface commands, SQLite schemas, environment variables and compose files
before it starts. Removing a facade needs the owner's approval. `AGENTS.md`
keeps describing the current layout until code actually moves.

## Resolved questions

Implementation progress: step 09.4-e moves live/cached media safety observation
interpretation into the Moderation domain. The
[media admissibility record](../media-admissibility.md) describes the preserved
delivery behavior and remaining provider, receipt and selection scope.

Step 09.4-f moves explicit hashtag-example validation, stable identity,
storage/retrieval scoping and result projection into Knowledge. The
[hashtag Knowledge record](../hashtag-knowledge.md) describes its preserved
facade and storage contracts; orchestration still sequences media text and
the existing recipe parameters.

- Moderation and attention are two faculties (decided above).
- The meme pool is a reading cache, like the Feeds cache, not stored knowledge.
  Items enter Knowledge only through an explicit workflow step such as tagging.
- Workflow recipes become data in stage 3, after typed Go workflows prove the
  steps (see Workflow recipes).
