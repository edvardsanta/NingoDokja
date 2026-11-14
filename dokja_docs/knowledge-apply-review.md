# Apply review: a flow step, not a screen

Status: proposal, 2026-11-08. No code changes yet. Implementation follows the
owner's approval, in the order under [Plan](#plan).

## Why this review

A live run of the Desktop **Apply** view (Library → Apply) on an isolated stack
was meant to close 08.2's rollout check. It found two defects, and a bigger
question that should have been asked first: **what uses this?**

- `knowledge.application.prepare`, the step that pins document versions, reads
  them within limits and records each read in the journal, is called only by
  the Desktop Apply view and its source supervisor.
- `knowledge.application.propose`, `assess` and `review` are called only by the
  offline comparison harness (`dokja_orch/cmd/knowledge-comparison`).
- No conversation, workflow or scheduled task consumes any of them.

The Apply view validates source versions and stops. It shows "3 of 3 documents
validated" and states that this "does not prove that a method was understood or
applied". A person gets no answer from it. The supervisor around it (pause,
correction, resume, cancel, an operator credential, a separate request ingress
and four routes) exists to steer a task that produces nothing they use.

From now on, every new surface or capability starts by naming its consumer. A
capability with none is evaluation code, not a feature.

## What the live run found

Isolated loopback orchestrator, scratch Knowledge store with the six authored
fixtures from `corpus_v1.json`, a disposable PostgreSQL journal, and the built
Electron app. No owner data, env file or database was used.

| Check | Result |
| --- | --- |
| Authenticated start over the real request ingress | Passed; 3 real Knowledge reads, 3 episodes with `begin`, `dispatch` and `observe` steps in PostgreSQL |
| Cancel | Passed |
| Real orchestrator restart | Passed: startup recovery logged `interrupted=2 unreadable=0`; the view reported the restart and offered only a new preparation |
| Pause during a slow read | **Crashed the orchestrator** (libzmq assertion, SIGABRT). Fixed in PR #302 for every orchestrator service client and the CLI/TUI requester |
| Pause during a read, after the fix | **Blocks the run for good**: every control cancels the plan context, the real Knowledge client reports the cancelled read as unknown, `Uncertain` latches and Resume stays disabled. The synthetic tests missed it because their fake source ignores its context |
| Host loss while polling | Detected only after a manual Refresh; the background poll left stale controls visible (refused by the orchestrator, so safe) |

The second defect is not fixed here: removing the human controls removes it.

## What Talk already does

Every chat turn already uses documents for a goal, in `internal/handlers`:

- `lookupForTurn` searches the focused document first (`lookupFocused`, the last
  document a reply cited), then the text-specific and general searches.
- Excerpts enter the prompt numbered, as reference material and never as
  instructions, and every quote is checked against the document.
- Focus follows the conversation (`chatFocus.remember`) and ends after three
  turns without the document.

So the person-visible half of "apply these documents to my goal" exists in
Talk. What Talk lacks is what `prepare` adds:

1. **Version pins.** A cited excerpt does not record which version of the
   document it came from. `prepare` pins `text_version` (a SHA-256 of the
   text) per source.
2. **Bounded whole-source reads** with read, byte and time limits owned by the
   orchestrator.
3. **Durable receipts.** Each read becomes an episode in the PostgreSQL journal
   (`begin`, `dispatch`, `observe`, or `interrupt` on recovery). This is the
   macro-learning record chosen in the journal review.

What Talk also lacks, and `prepare` does not provide, is an **outcome**. No chat
handler records whether an answer helped. Without an outcome, the journal holds
reads and nothing can learn from them. 08.3 needs that signal.

## Decision

Apply becomes a **step of the Talk workflow**, chosen by the flow, never by a
click:

```
Interface → message.created → Orchestrator
  → [knowledge] prepare focused sources (pin, bounded read, journal receipt)
  → [chat] answer from the prepared context, citing pinned versions
```

- **Trigger.** Only turns with a focused document (or documents the person
  names). A turn without focus behaves exactly as today. One preparation per
  turn; a focus that does not change reuses the pins of the last turn when
  the text version still matches, so it reads nothing again. The owner can
  supply those documents or links from Desktop, Discord or WhatsApp; the
  preparation keeps that origin and reading identity as provenance. Content
  sent by another person in a shared place is not owner knowledge merely
  because Ningo saw it.
- **Budget.** Preparation runs inside the chat turn's deadline. Its 15 s
  workflow deadline becomes the smaller of that and what remains of the turn.
  A source that cannot be read in time is left out and the answer says so;
  the turn does not fail.
- **Stop.** A Stop (10.6-b) or turn timeout cancels the read. That read settles
  as `interrupted` for **that turn's episode only**. Each turn is its own
  application, so nothing latches across turns, and the next turn starts
  clean. This designs out the pause defect.
- **Outcome.** The episode's outcome stays unknown until a later step adds
  answer feedback in Talk (a person marking an answer useful or wrong). The
  Notebook already lets the owner resolve memory experiences through
  `memory.resolve`; step 4 should extend that existing path to answers rather
  than open a new channel. Until then the journal is provenance, and the docs
  say so; nothing claims learning.

## What goes away

| Area | Removed |
| --- | --- |
| Desktop | Library → Apply tab, `source_application.tsx`, its i18n keys, the main-process allowlist entries and payload builders, `source_application.e2e.ts`, the unit and UI tests, the fake transport entries |
| Orchestrator | Routes `knowledge.application.supervision.{status,start,preview,control}`, `SourceSupervisor`, `knowledge_desktop_supervision.go`, the Desktop credential admission and redaction |
| Configuration | `DOKJA_SOURCE_SUPERVISION_TOKEN` in compose files, `production-runbook.md` and docs |
| Docs | `desktop-source-supervision.md`, `knowledge-source-steering.md` and `knowledge-supervision.md` become history notes pointing here |

**Kept:** the preparation workflow and its limits, the `knowledge.application.prepare`
route (read-only; step 3's Talk workflow dispatches it as a Knowledge step, so it
is no longer offered to the Desktop shell), the journal and its PostgreSQL
adapter, and startup recovery (`RecoverSourceReads`).

The removed `knowledge.application.supervision.*` request types are refused with
an explicit error before reaching the Knowledge service, and the retired
credential is still dropped from every event an older Desktop build might send.

**Configuration after the change:** the journal is enabled by
`DOKJA_SOURCE_SUPERVISION_POSTGRES_DSN` alone. The name is kept on purpose: a
rename would be a second breaking change for no behavior. Startup recovery runs
whenever the DSN is set.

`propose`, `assess` and `review` stay as evaluation routes for the comparison
harness, unchanged. They are marked as having no production consumer.

## Compatibility

Breaking, for an opt-in surface that is off by default:

- four orchestrator request types removed;
- one environment variable removed (an orchestrator still started with it
  logs that it is ignored, instead of failing);
- the Desktop Apply tab removed.

No envelope, schema or journal namespace change. Existing journal rows remain
readable, and recovery keeps settling them.

## Plan

1. **PR #302** (open): the socket crash fix. Independent of this review.
2. **Remove the surface.** Desktop Apply, the supervisor, the operator routes and
   the token. The journal is configured by the DSN alone, and recovery keeps
   running at startup. Compose files, the runbook and the docs change in the
   same PR. The journal is reachable and recovered at every commit.
3. **Answer feedback as the outcome**, so 08.3 has a signal: the owner judges an
   answer in Talk as useful or wrong, through the existing experience memory. See
   [answer feedback](answer-feedback.md).
4. **Prepare in Talk.** The workflow step above, with version-pinned citations,
   Stop settling as `interrupted`, and tests for: no focus (unchanged turn),
   focus with an unchanged version (no re-read), a changed version, a source
   over budget, and a Stop during a read.

## Owner decision (2026-11-08)

Preparation in Talk alone would only name document versions and bound reads; the
learning the journal was built for needs an outcome. The owner chose answer
feedback first (step 3), since it is the only step that gives 08.3 something to
learn from. Preparation in Talk (step 4) follows.
