# ADR 0010: four concepts instead of nine faculties

Date: 2026-10-21. Status: proposed, for the owner to accept when reviewing it.
Would supersede the faculty list of [ADR 0007](./0007-faculties-not-features.md)
and amend [ADR 0006](./0006-reading-macro-component.md). Migration: pending.

## Context

ADR 0007 split Ningo into nine faculties: Reading, Knowledge, Experience,
Prediction, Moderation, Attention, Language, Action and Coordination. The owner
found that list harder to reason about than the product it describes, and the
split leaked into the interfaces: the desktop showed "readings", "knowledge" and
"memory" as three separate places although, for the person using Ningo, reading
is simply how knowledge comes in.

What ADR 0007 got right stays: Book, Meme and Digest are not capabilities of
their own but workflows across shared ones, and they keep their public events as
facades while they migrate.

## Decision

Ningo has four concepts. Each is what the owner thinks about, not how the code
is deployed.

| Concept | What it covers | Where it lives today |
| --- | --- | --- |
| **Knowledge** | What Ningo knows and how it comes in: following sources and reading files (feeds plugins, extraction, OCR, transcription), classifying, storing, searching and consolidating | Knowledge domain and service; Feeds service as its intake |
| **Memory** | What Ningo did and how it went: episodes, delivery receipts, feedback, and predictions scored against outcomes | Memory domain and service |
| **Action** | What Ningo says and does outside: replying, posting and sending on a platform, including generating the text it says | Communications domain and service, the Discord and WhatsApp interfaces, chat generation |
| **Rules** | What decides whether and how much: admissibility (NSFW, safe-only channels, platform rules), relevance and selection (thresholds, digest ordering, freshness), and the owner's switches | Moderation domain; selection parameters that workflows pass to it |

The orchestrator sequences workflows across these concepts and holds no rules of
its own; that part of ADR 0007 is unchanged. Prediction is part of Memory,
because a prediction is only useful when it is scored against what happened.
Generating text is part of Action, because it only matters when Ningo says it.
Reading belongs to Knowledge and to Memory at once: what was read becomes
knowledge, and the act of reading it (what, when, why, with what result) is an
episode Memory keeps, as ADR 0006 already says.
Coordination is the orchestrator, not a concept.

Under this model:

- **Meme** = Knowledge intake (scrapers become Feeds plugins and the pool is an
  intake cache) → Rules (NSFW screening, hashtag relevance) → Action (delivery)
  → Memory (sent receipts, feedback).
- **Digest** = Knowledge intake (Feeds) → Rules (ordering, freshness) → Action
  or direct rendering.
- **Book** = Knowledge (on-demand reading and storage) → Action → Memory.

Services and containers stay separate where deployment needs it, for example a
heavy NSFW classifier keeps its own container and is called by Rules. A
container is a deployment choice, not a concept.

## Consequences

- Interfaces present these concepts. The desktop's sources are a view of
  Knowledge rather than a workspace of their own; the CLI and TUI follow the
  same grouping as they gain those screens.
- The migration order of ADR 0007 stays (Meme, then Digest, then Chat), with
  "faculty" read as one of the four concepts. Existing `meme.*`, `digest.*` and
  `book.*` events, commands and scheduler triggers keep working through their
  facades and baseline tests until the owner approves removing one.
- No code moves in this record. `AGENTS.md` keeps describing the current layout
  until code actually moves.
