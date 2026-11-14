# Task contract: logical identity, dispatch admission and settlement

Date: 2026-11-14. Status: proposed, for the owner to review before the
implementation batches. Continues [session-task-architecture-review](session-task-architecture-review.md)
and [task-lifecycle-compatibility](task-lifecycle-compatibility.md); it changes
no runtime contract by itself.

## What exists

Archived chat already holds the full contract for one consumer: durable
conversation/send identity, an atomic attempt fence, an ordered per-conversation
lane with accepted/queued/dispatched observation, owner stop, a capability
report, startup recovery that never replays, and a verified settlement matrix.
`RunTask` gives every typed workflow bounded steps, evidence, receipts and a
fresh TaskID/AttemptID per invocation, with terminal guards and late-result
handling.

What does not exist is the same contract for any other consumer: no logical
task identity that outlives one `RunTask` call, no input admission beyond the
archive lane, and no reviewed settlement matrix outside archived chat.

## Contract

### 1. Logical task identity

A logical task is a piece of work a domain can name, not one execution of it.
`RunTask` IDs stay per-invocation, always fresh, as today.

```go
// TaskRef names one logical task. The consumer's own durable record owns the
// key; the reference grants nothing and is not manufactured from caller scope.
type TaskRef struct {
    Domain   Domain // the domain that owns eligibility and acceptance
    Workflow string // the typed workflow that executes it, with its version
    Key      string // the consumer's stable key, e.g. "conversation/<id>/send/<id>"
}
```

Rules:

- The Key belongs to a record the domain already keeps (archive turn, journal
  episode, watch row). A logical task without a durable record does not get an
  identity; the invocation stands alone, as today.
- Two invocations of the same TaskRef are attempts of one logical task. The
  attempt number comes from the owning record (archive `attempts`, journal
  revision), never from counting invocations.
- The public event ID remains transport correlation only. The workspace
  TaskID/AttemptID remain execution-local. Neither becomes the logical key.

### 2. Lifecycle and authority

Domains own eligibility, applicability and result acceptance; the orchestrator
sequences transitions and enforces bounds. A logical task has no central state
machine: its lifecycle is derived from the owning record.

| State | Meaning | Owner |
| --- | --- | --- |
| accepted | The admission recorded the work; nothing executed | orchestrator (lane) |
| queued | Earlier work of the same lane has not settled | orchestrator (lane) |
| dispatched | Execution has begun | orchestrator (lane observation) |
| settled | The owning record accepted a result for the current attempt | domain |
| interrupted | Execution may have produced an unknown effect; never replayed | domain (recovery) |

`accepted/queued/dispatched` are progress observations, not outcomes: none of
them proves a result or an external effect. Settled and interrupted are
terminal until the domain's own retry rule reopens the work.

### 3. Input dispatch admission

`core.OrderedQueue` generalizes to keyed lanes:

```go
// TaskLanes orders work per TaskRef key. Admission is one atomic step with the
// consumer's own recording, so lane order is record order. Independent lanes
// run concurrently; each lane is bounded.
type TaskLanes struct { ... }

func (l *TaskLanes) Admit(ref TaskRef, run func(ctx context.Context) error) (admitted bool)
```

- Admission and recording happen together (the archive enrolls a pending turn
  in one admission today; a consumer without a durable record does not use a
  lane and does not need one).
- A lane runs one task at a time; a bounded number may wait. Past the bound,
  admission refuses before recording anything.
- Waiting counts against the caller's own deadline. Cancellation fails the
  admitted work with a plain reason; the model/executor may never be asked.
- Receipts: `accepted`, `queued`, `dispatched`. An ingress that can deliver
  them asks; sources that cannot, report nothing, as WhatsApp does today.

Domains decide what may be admitted; the lane enforces order and bounds and
grants no capability. A caller cannot invent a lane key to jump a queue: the
key is the consumer's record identity.

### 4. Settlement

Settlement binds a result to the current attempt of the owning record, in one
conditional write, and is accepted by the domain:

- Duplicate results of the same attempt: first accepted, the rest conflict.
- Late results of an older attempt: conflict; the current attempt is untouched.
- Foreign scope or record: refused before any change.
- Cancellation or deadline: execution end is recorded as unknown-effect, not
  failure; an effect that lands later is a conflict, never an update.
- Recovery (restart): marks in-flight work interrupted; an interrupted effect
  is never replayed. Only an explicit, eligible retry sends it again.

These are the archived-chat matrix rows, restated for any consumer. Each
consumer proves them with its own regressions before claiming the contract.

### 5. State revision, authority and budgets

- State revision continues to bind a proposal to the state it examined
  (inference/application contracts); it is not replaced by attempt counters.
- Authority is the domain's: the orchestrator's lane admits and orders, the
  domain accepts or refuses results, and neither grants the other new rights.
- Aggregate budgets belong to the consumer that needs them. Archived chat has
  none (owner-initiated retries only). A cumulative attempt/cost budget becomes
  necessary with automatic retries or delegated work (`10.5`) and is defined
  there, per TaskRef.

### 6. Provider centralization (owner boundary, 2026-11-14)

Chat providers are **centralized and must not exist in services**. A provider
is an external adapter that helps create Ningo — it plugs into the central
Runtime binding ([ADR 0011](decisions/0011-inference-provider-boundaries.md)),
and no domain or service holds a provider client of its own. Consequences for
this contract:

- Execution always crosses one central Runtime boundary. Execution end,
  cancellation and unknown-effect settlement are measured at that boundary; a
  provider's native session, thread or run id never becomes task, attempt or
  transcript identity (already required by the identity map).
- Provider selection, availability and reported provenance are Runtime facts
  projected into the task scope; domains consume them, they do not discover or
  bind providers.
- A task contract regression must never require a provider inside a service:
  deterministic adapters at the Runtime boundary supply the settlement cases.

## Implementation batches

**`10.6-d` — core lanes.** `TaskLanes` as the ordered-queue generalization,
with admission/receipts/bounds/cancellation; archived chat becomes one consumer
on it (behavior preserved: same ordering, same refusal, same observation
vocabulary). Core regressions mirror the chat queue tests: position order,
independent lanes, bound refusal before recording, cancellation, lane cleanup.

**`10.6-e` — journal consumer matrix.** The knowledge source-read journal is
the first non-chat consumer to prove settlement: duplicate and late attempt
settlement, foreign-scope refusal, cancellation-during-read as interrupted,
recovery without replay — the existing journal admission and CAS cover most
rows; this batch adds the missing regressions where a gap is found and records
the matrix in [task-lifecycle-compatibility](task-lifecycle-compatibility.md).
No schema change is expected (revisions already supply the fence).

A consumer that needs ordering beyond one-at-a-time adopts a lane when it has a
record to bind to; consumers that are single-shot per record (intake, reading)
keep invoking `RunTask` directly.

## Compatibility

Additive. No event envelope, route, action, schema, provider contract or
configuration change. `TaskRef` and lanes are orchestrator-internal; the
desktop `queued/dispatched` observation vocabulary is unchanged, and archived
chat's behavior is preserved by construction (its tests must pass unmodified
in intent). Rollback is removing the new code; the archive and journal keep
their current guarantees either way.
