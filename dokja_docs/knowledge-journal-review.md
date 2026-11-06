# Application journal: design re-evaluation

Date: 2026-11-06. Owner request: the journal feels foreign to the flow, so this
reviews what it would take to simplify it or fold it into a simpler one. Nothing
is changed by this note; it proposes options and asks for a decision.

## What the journal is, in plain words

The [journal](knowledge-journal.md) records one try at something Ningo does (an
*attempt* inside an *episode*) and what was observed afterwards. It was meant as
the record a later step would learn from: which method worked, under which
conditions. Every change is a separate write with its own operation key, expected
revision and admission checks, stored through a transaction port whose only
adapter is [PostgreSQL](knowledge-journal-postgres.md).

## What exists today (measured)

| Part | Size | Used by |
| --- | --- | --- |
| Domain `dokja_memory/application` | 782 lines of code (`journal.go`, `records.go`, `eligibility.go`), 761 of tests | `knowledge_source_receipts.go` only |
| Eligibility, selection and prediction scoring (`Evaluate`, `Select`, `ScorePrediction`) | 203 lines | Nothing outside the package |
| PostgreSQL adapter, schema `dokja_application_journal` (4 tables) | 409 lines | The optional desktop source supervision, only when given its own PostgreSQL connection and token |
| Orchestrator source-read receipts and supervisors | 184 + 761 lines | The desktop's source supervision (opt-in) |
| Design docs around it | about 1,235 lines across 7 documents | — |

One flow writes to it: reading a selected source during a supervised knowledge
application. Each read makes three journal writes: `Begin` (prepared),
`MarkDispatched` (possibly sent) and `Observe` (the receipt), or `MarkInterrupted`.
Every write builds a fresh operation key, an expected revision and admission
closures. The supervisor's screen does not read the journal back; it gets the
settlement through an in-memory callback (`SourceReadSettlement`). No live
operator rollout has been recorded for this path.

## Why it feels foreign

1. **A second lifecycle next to the one that works.** Archived chat solves the same
   problems (duplicates, late results, retry, restart) with one turn row: a state,
   an attempt counter and one conditional update ([settlement matrix](task-lifecycle-compatibility.md)).
   The journal solves them again with episodes, attempts, observations, operation
   keys, revisions and admission, in its own vocabulary.
2. **General before a second consumer.** It is built for any registered capability,
   predictions and learning, but has one caller doing one kind of read.
3. **Ceremony per action.** Three transactions for one bounded read, each with its
   own idempotency key, is more machinery than the read itself.
4. **A separate store.** Everything else the owner runs keeps state in SQLite; this
   path alone needs a PostgreSQL connection and its own token.
5. **Written for a reader that does not exist.** Eligibility, selection and scoring
   are the reason for most of the record shape, and nothing calls them.

## What is worth keeping, whatever the shape

- No success without a receipt: a prepared or dispatched attempt is never an outcome.
- A late receipt settles only its own attempt; it cannot be relabelled as a newer one.
- "Possibly dispatched" and "done" stay different, and an interrupted attempt stays
  unknown: it is never replayed by itself.
- Stored evidence (digests, source version) instead of a provider's claim.

These are the same rules archived chat already holds, which is the argument for
folding rather than keeping a parallel model.

## Options

### A. Freeze it as it is

Change nothing; extend nothing until a second real consumer appears.

- Cost: none now. The maintenance and reading cost of the abstraction stays.
- Keeps the open gap from `08.2`: a crash between dispatch and receipt leaves the
  attempt dispatched with no owner to mark it interrupted (it is still never
  counted as success).

### B. Shrink it to what its consumer uses

Keep the journal and its PostgreSQL adapter, but remove what nothing uses:

- Move eligibility, selection and scoring out (delete, or keep in `dokja_lab` as a
  sketch) until a reader exists.
- Merge `Begin` and `MarkDispatched` into one "started" write, since the read is
  dispatched immediately after preparation and nothing happens in between.
- Derive the operation key from the attempt and the step instead of a fresh UUID
  per call, so a retry of the same step is naturally the same operation.

Cost: a focused refactor of the domain and `knowledge_source_receipts.go`, with
their tests. The PostgreSQL schema can stay at version 1 if the snapshot encoding is
kept, or move to version 2 if the merged write changes it. It still leaves two
lifecycle models.

### C. Fold the source read into the turn-style flow (recommended)

Treat a supervised source read like an archived chat send: one row per attempt.

| Chat turn today | Source-read attempt |
| --- | --- |
| `pending` → `completed` / `failed`; restart → `interrupted` | `started` → `succeeded` / `failed` / `unknown`; restart → `interrupted` |
| `attempts` column plus one conditional update to settle | Same: settle only the pending attempt with the expected counter |
| Same `send_id` is answered from the archive | Same step key returns the recorded attempt |
| Startup recovery owns pending turns, never replays | Same owner rule for started attempts |
| Reply and provenance | Receipt digest, source version and outcome |

- Store it beside the rest of the owner's state (SQLite, as the archive does), so
  the path no longer needs a separate PostgreSQL connection and token.
- The supervisor keeps its controls and its in-memory settlement view; only the
  record underneath changes.
- When learning from outcomes is actually built, it reads these rows. Eligibility
  rules can then be written against real data instead of ahead of it.

What changes: `knowledge_source_receipts.go` writes one attempt row and settles it
once; the `dokja_memory/application` journal and the PostgreSQL adapter are retired
for this path; `cmd/va/source_supervision.go` stops opening a PostgreSQL journal;
the docs around the journal are reduced to one note. A new SQLite table is a schema
addition and goes through review first.

What is lost: the general multi-capability, multi-observation model and its
PostgreSQL storage. Both can be rebuilt from real needs if a second consumer
appears, and the safety rules above carry over unchanged.

Compatibility: the path is opt-in and has no recorded live rollout, so no existing
operator data needs migrating. An operator who already initialised the PostgreSQL
schema would simply stop using it. Interfaces and the event envelope do not change.

It also closes the `08.2` gap the same way chat does: startup recovery marks
started attempts interrupted, with one owner and no replay.

### D. Remove the supervised source-read path

If supervised application reading is not something the owner wants to keep, remove
the path, the journal and the adapter together. This is the smallest result, but it
gives up the supervision feature and the receipts work in `08.2`.

## Recommendation (before the owner's decision)

Option C. It keeps every safety rule that matters, removes a second lifecycle model,
drops the separate PostgreSQL requirement for this path and makes the source read
look like the rest of the system. If a smaller first step is preferred, B is a safe
stage on the way to C: removing the unused scoring first loses nothing.

## Owner decision (2026-11-06)

- Keep supervised source reading as a feature, integrated in the existing workflow:
  it already runs inside the task runner, and no new flow or record type is created.
  Option C is rejected.
- Option B, shrinking the journal in place.
- Keep PostgreSQL: the journal is the macro-learning store (episodes consolidated for
  later learning, apart from the conversation archive), so it stays separate from
  SQLite. Eligibility, selection and scoring stay, because they are that learning.

### Revised B

Checking B against the code changed one item. The journal refuses a prediction that
is not strictly earlier than dispatch (`MarkDispatched` and `ValidatePrediction`):
prediction before action is the signal later scoring compares against. Merging
`Begin` and `MarkDispatched` would remove the moment a prediction is recorded before
acting, so that merge is dropped. B is now:

1. Keep prepared, dispatched and receipt as separate steps, and keep scoring.
2. Derive each operation key from the attempt and the step instead of a fresh UUID
   per call, so repeating a step replays it instead of becoming a new operation.
3. Close the `08.2` gap in the existing workflow: at orchestrator start, mark
   attempts left dispatched without a receipt as interrupted, as chat recovery does,
   never replaying them.
4. Route the three writes in `knowledge_source_receipts.go` through one small helper
   instead of repeating keys, revisions and admission at each call.

## Decisions asked of the owner (answered above)

1. Keep supervised source reading as a feature (A, B or C), or drop it (D)?
2. If kept: fold into a turn-style record (C), or shrink the journal (B)?
3. Is PostgreSQL for this path something you want to keep, or should it live in the
   same SQLite store as conversations?
4. Should eligibility and scoring be deleted, or parked in `dokja_lab` as a sketch?

The revised B above is implemented: the write helper, operation keys named by attempt and
step, and startup recovery (see [restart recovery](knowledge-journal.md#restart-recovery)).
