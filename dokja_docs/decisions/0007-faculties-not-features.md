# ADR 0007: macro-components are faculties; features are workflows

Date: 2026-10-18. Status: accepted as the target direction. Migration: pending.
Generalizes [ADR 0006](./0006-reading-macro-component.md).
Related: [decision index](./README.md), [workspace](./0001-task-workspace.md),
[capability map](../capability-map.md),
[research](../cognitive-macrocomponents-research.md).

## Context

The current code is cut by feature: `dokja_book`, `dokja_meme`, `dokja_digest`
and `dokja_chat_ai` each bundle reading, storage, judgment and output for one
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
| Evaluation and selection | Judging admissibility and choosing what deserves attention | Moderation domain; Digest ranking; meme screening |
| Language | Generating text | Chat domain and generation providers |
| Action | Sending, posting, replying on a platform | Communications domain and service |
| Coordination | The task workspace and the cycle; sequencing only | Orchestrator core (ADR 0001) |

Under this model:

- **Meme** = scheduled reading of meme sources → evaluation (screening) →
  knowledge (hashtag labels) → prediction (shadow) → action (delivery) →
  experience (sent receipts, feedback).
- **Digest** = reading (feeds) → selection policy → language or direct
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
- Feature-specific code that is genuinely unique (a source format, a platform
  quirk) is a plugin of the relevant faculty, not a feature domain.

This keeps the rule that the orchestrator holds no business rules: recipes
sequence and configure; faculties decide.

## Migration and compatibility

No code moves in this record. Existing feature domains and services stay as
compatibility facades until each migration is proven:

1. Book first, through ADR 0006 (steps `07` and `09`).
2. Meme: its scrapers and pool become reading-host plugins; screening moves
   behind evaluation; sent receipts become experience. Existing meme events,
   CLI and Discord commands and the scheduler triggers keep working through a
   facade.
3. Digest: its policy moves under evaluation and selection; `digest.*` stays.
4. Chat: follows the chat plan, which already treats it as a loop faculty.

Each step reviews the event envelope, routes and actions, service contracts,
interface commands, SQLite schemas, environment variables and compose files
before it starts. Removing a facade needs the owner's approval. `AGENTS.md`
keeps describing the current layout until code actually moves.

## Open questions

- Is evaluation and selection one faculty, or are moderation (admissibility)
  and attention (ranking) separate?
- Where does the meme pool live: a reading cache like Feeds, or stored
  knowledge?
- Does a recipe format (data interpreted by the orchestrator) replace Go
  handlers per feature, and when?
