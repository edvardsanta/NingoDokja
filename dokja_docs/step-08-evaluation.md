# Step 08: architecture evaluation

Date: 2026-10-19. Tracker: [steps.json](../steps.json) step `08`.
Inputs: [step 03 baseline](./step-03-baseline.md), steps `04`–`07`,
[step 07 record](./step-07-reading.md), ADRs [0001](./decisions/0001-task-workspace.md),
[0002](./decisions/0002-plugin-lifecycle.md), [0004](./decisions/0004-shadow-prediction.md),
[0006](./decisions/0006-reading-macro-component.md).

The recommendations at the end are proposals. The owner decides whether each
prototype is adopted, revised or stopped.

## 1. Three modes on fixed cases

[step08_comparison_test.go](../dokja_orch/internal/handlers/step08_comparison_test.go)
composes the scheduled hashtag delivery three ways, as `cmd/va/main.go` would:

- **existing:** `WithHashtagSuggestions` plus `WithHashtagExperience`;
- **workspace:** the hashtag task with Memory prediction off;
- **shadow:** the task with prediction on.

It runs all three over 8 fixed cases with metered fakes. The invariants run in CI.
The timings run with
`DOKJA_MEASURE=1 go test ./internal/handlers -run TestStep08 -v`.

### Behavior

The delivered text is identical in all three modes for all 8 cases.

| Case | Delivered | Calls (existing / workspace / shadow) | Trace reason (task modes) | Existing path |
| --- | --- | --- | --- | --- |
| Relevant example | `meme\n#Humor` | OCR, search, recall, record / OCR, search, record / OCR, search, recall, record | (completed) | log line only |
| Below threshold | `meme` | OCR, search in all | `abstained` | no trace |
| No examples | `meme` | OCR, search in all | `abstained` | no trace |
| Unreadable media | `meme` | OCR in all | `media_text_unavailable` | log line only |
| Knowledge down | `meme` | OCR, search in all | `knowledge_unavailable` | log line only |
| Memory down | `meme\n#Humor` | as relevant | shadow: `prediction_unavailable` (degraded) | swallowed in background |
| Memory slow | `meme\n#Humor` | shadow skips the late recall | shadow: `prediction_budget_missed` (degraded) | swallowed in background |
| Delivery fails | (none, error) | existing records; task modes record nothing | (completed) | records an attempt that reached no channel |

There are two behavioral differences, both intended by ADR 0004:

- **Recording.** The existing path records a suggestion even when delivery
  fails. The task modes record only after delivery succeeds.
- **Prediction timing.** Only the shadow mode predicts before delivery. The
  existing path predicts in background work, so its order relative to delivery
  is not guaranteed.

In the shadow mode, a failed delivery costs one wasted `memory.recall`.

### Size

- Response bytes from the services are the same in all modes, plus the recall
  answer (55 bytes in the fakes) in shadow mode.
- The task trace (`TaskResult` as JSON) is 0.5–2.0 KB per attempt:
  - 515 B for unreadable media;
  - 931 B when Knowledge is down;
  - 1.2–1.5 KB for an abstention;
  - 1.7 KB without prediction and 2.0 KB with it, for a suggestion.

### Latency

Instant fakes, 50 runs per case and mode, time until the delivery returns.

| Case | existing p50 / p95 | workspace p50 / p95 | shadow p50 / p95 |
| --- | --- | --- | --- |
| Relevant example | 5.9 / 13.4 µs | 36.8 / 141.8 µs | 76.2 / 100.2 µs |
| Below threshold | 3.7 / 6.9 µs | 45.5 / 64.1 µs | 34.8 / 50.3 µs |
| No examples | 3.0 / 9.5 µs | 43.0 / 55.3 µs | 43.5 / 50.4 µs |
| Unreadable media | 1.1 / 1.4 µs | 6.5 / 7.4 µs | 6.5 / 7.5 µs |
| Knowledge down | 2.2 / 3.0 µs | 14.1 / 19.6 µs | 13.3 / 21.2 µs |
| Memory down | 5.5 / 7.1 µs | 33.1 / 41.1 µs | 43.2 / 72.2 µs |
| Delivery fails | 5.6 / 13.1 µs | 48.4 / 55.5 µs | 55.2 / 78.1 µs |

The workspace adds about 5–140 µs per delivery.

The service calls are identical across modes except the shadow recall, which is
the only latency cost that matters. With Memory answering after a fixed delay,
under the production 1 s budget (5 runs each):

| Memory delay | existing | workspace | shadow |
| --- | --- | --- | --- |
| 100 ms | 23 µs | 97 µs | 100 ms |
| 400 ms | 22 µs | 117 µs | 400 ms |
| 2 s | 23 µs | 91 µs | 1.000 s, then unscored |

**Live recall.** `memory.recall` against the owner's running Memory service
(read-only, a generic query, 31 requests):

- cold: 1305 ms;
- warm: p50 26 ms, p95 32 ms, max 33 ms;
- no degraded answers.

So a warm prediction holds a delivery about 30 ms. The first one after Ollama
unloads its model misses the 1 s budget and its attempt is recorded unscored.

The existing path's live latency (OCR, Knowledge search, Discord) was not
measured: the stack also runs the scheduler and the Discord bot, so it was not
driven.

## 2. Coupling

Lines added per experiment commit (code / tests), and the modules touched:

| Commit | Modules | Code / tests |
| --- | --- | --- |
| `d1f5079` typed task workspace | `dokja_orch` (core only) | 498 / 399 |
| `340de3a` versioned plugin lifecycle | Feeds, Knowledge | 388 / 244 |
| `5c981d0` hashtag task | `dokja_orch` (handlers, `main.go`) | 339 / 278 |
| `04e37d6` on-demand reading host | Feeds, images, 3 compose files | 451 / 452 |
| `b266691` `knowledge.read` | Knowledge domain, `dokja_orch` | 412 / 339 |

- None of the experiments changed the event envelope, an existing route or
  response, a schema, or an existing interface command.
- The router gained one route (`knowledge.read`).
- Rollback is one composition line in `main.go` for each orchestrator
  experiment.
- After the host exists, replacing a reader costs two manifest edits and no code
  (step 07).

## 3. Fault containment

Covered by tests, not re-run here beyond the suites:

- **Hashtag task (step 06):** optional Memory failure or a missed budget
  degrades the attempt and keeps the delivery. An unreadable meme or Knowledge
  outage ends the task with its reason, and the delivery goes ahead without a
  hashtag.
- **Reading host (step 07):** crash, hang, malformed or oversized output,
  incompatible versions and ambiguity each give a defined reason, and nothing is
  invoked or stored when it shouldn't be. A hung reading no longer delays
  `feeds.status`.
- **Workspace (step 04):**
  - `TestConcurrentTasksShareNothing` and `TestEvidenceFromAnotherScopeIsDropped`
    cover scope isolation;
  - the deadline and cancellation tests cover late results;
  - payloads over 64 KiB stop before any step, which forced the reading task to
    keep file content outside the workspace, as ADR 0001 intends.

The existing path contains these failures too: none of them changes the
delivered text. The difference is diagnosability, not containment.

## 4. Trace usefulness

- On the task paths, every terminal or degraded outcome names a stable reason,
  its step, the evidence it rested on (hashed refs) and the attempt id.
- The existing path leaves a log line for OCR and Knowledge failures and nothing
  for an abstention. Background Memory failures are swallowed.
- The trace is only returned to the caller and summarized in a log line. It is
  not stored anywhere, so it helps a test or a debugger but cannot be inspected
  after the fact. Storing it is a step `10` question (durable execution and state
  ownership).

## 5. Prediction skill, support and feedback

Read from a copy of the owner's Memory store (`.dokja/dokja_memory.db` with its
WAL), aggregate columns only. The copy was deleted afterwards.

| Quantity | Value |
| --- | --- |
| `hashtag.suggest` attempts | 7 (2026-10-14 to 2026-10-19) |
| Scored (frozen predicted and baseline probability) | 7 |
| From the legacy path (recorded at suggestion time) | 5 |
| From the task path (recorded after delivery) | 2, both on 2026-10-19 after the stack started on `feat/hashtag-workspace` |
| Resolved (accepted or replaced) | 0 |
| Feedback coverage | 0 / 7 |
| Brier score, baseline skill | Not computable: no eligible outcomes |

No claim about prediction skill can be made.

- **Volume.** ADR 0004's provisional 30-scored flag is not reached even for
  attempts, let alone resolved ones.
- **Feedback is the bottleneck, not the predictor.** In five days the explicit
  tag never resolved a suggestion. Unknown feedback is not negative, so these
  attempts stay unresolved.

## 6. Recommendations (for the owner)

1. **Task workspace (ADR 0001): adopt** for workflows that span several
   faculties and have more than one way to fail.
   - It changes no delivered text.
   - It adds microseconds.
   - It gives every outcome a stable reason, evidence and attempt id.

   Revise one point: decide in step `10` where traces are kept, since today they
   are lost after the reply.
2. **Plugin lifecycle and reading host (ADR 0002, 0006): adopt** for reading.
   - Replacement is configuration, and failures are defined.
   - Costs:
     - one subprocess per reading (about 43 ms with Python);
     - Python in the Feeds image;
     - the ROUTER server.

   Still open: explicit binding configuration, and a sandbox if untrusted plugins
   are ever allowed.
3. **Shadow prediction (ADR 0004): keep in shadow, but stop building on it**
   until feedback exists. It can only gather data if:
   - the task path runs live (merge or deploy);
   - suggestions get tagged.

   Review the feedback channel (how acceptance or replacement is observed)
   before any further prediction work. Optionally warm or keep the embedding
   model loaded, so the first prediction does not miss the budget.
4. **Latency budget (step 03 asked for one):**
   - workspace overhead: at most 1 ms p95 per task;
   - shadow prediction on a delivery path: the existing 1 s cap, with warm p95
     expected near 35 ms;
   - an on-demand reading: the plugin's manifest timeout, at most the host's
     2 minutes.

   These are measured-from-fakes plus one live service, not a live end-to-end
   baseline.

## 7. Proposed roadmap revision

- **Step 09:**
  - Start with the `book.*` facade over reading → Knowledge → generation, which
    step 07 unblocked.
  - Then express Meme and Digest over faculties, reusing the hashtag task's
    pattern.
  - Leave chat domain ownership last, since it touches the most commands.
  - The ADR 0005 loop transitions stay with the first workflow that iterates.
- **Step 10:** add the concrete findings:
  - traces and attempts are not persisted;
  - experience recording is background work with no durable job;
  - the Knowledge client's 5 s timeout can report a stored book as failed;
  - feedback for predictions has no channel that produced data.
- **Step 11:** Feeds is now the reading host with Python and pypdf. Review its
  image, permissions and the pypdf version split with Knowledge.

## 8. Owner decisions (2026-10-19)

| Prototype | Decision |
| --- | --- |
| Task workspace (ADR 0001) | Adopted for multi-faculty workflows; trace storage is decided in step `10` |
| Plugin lifecycle and reading host (ADR 0002, 0006) | Adopted as the reading path |
| Shadow prediction (ADR 0004) | Kept. Direction: call a local agent CLI (Claude Code or Codex) before Ollama, since it is easier to set up |
| Latency budgets | Accepted as proposed in section 6 |

The local agent CLI direction is recorded, not designed. Before building it,
check two constraints:

- **Embeddings.** Neither CLI produces embeddings, which `memory.recall` needs
  to find similar experiences. Ollama, or another embedder, stays for recall. A
  CLI fits judgment work: estimating acceptance from the recalled cases, or
  reading feedback.
- **Latency.** A CLI call takes seconds. On a delivery path that is beyond the
  accepted 1 s cap, so a CLI-backed prediction would run off the delivery path
  or be recorded unscored. Using a CLI also sends meme text to that tool's
  provider, so it needs the same review as any external call.
