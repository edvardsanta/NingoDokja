# Answer feedback

Status: implemented for Desktop Talk (2026-11-08), CLI and TUI (2026-11-14).
Desktop buttons and the TUI `u`/`w` keys are offered only when
`conversation.capabilities` reports `feedback: supported`; the CLI asks the same report
before sending a verdict.

The owner can judge an answer in Desktop Talk, the CLI or the TUI as **useful** or
**wrong**. That verdict is the
outcome step `08.3` learns from: until it existed, Ningo recorded what it did but never
whether it helped. It reuses the experience memory loop that music and hashtag suggestions
already use, rather than a new store. The order was the owner's decision, and the
[Apply review](knowledge-apply-review.md) plan puts this step before preparation in Talk.

## Flow

1. **Recording.** When an answer in an archived conversation (Desktop, CLI or TUI) settles
   as completed, the orchestrator records a `chat.answer` memory experience in the
   background:
   - the context is the owner's question, bounded like any experience context;
   - the detail is `cited` or `uncited` (whether the answer cited the owner's documents);
   - before any verdict can exist, it asks memory how likely a similar question's answer
     was found useful, and freezes `predicted_p` and `baseline_p` with the record.

   The experience ref is derived from the turn (`ChatAnswerRef`), so a replayed or
   retried send lands on the same experience; memory's record ignores a ref it already
   holds. Failed, stopped, voice and channel answers record nothing: groups have no verdict
   to give, and their questions are other people's. Memory being slow or down never delays
   or fails the reply; the record is skipped and logged without the question.
2. **Verdict.** `conversation.feedback`, an owner-initiated change: a real Desktop or TUI
   press, or the explicit CLI command `conversations feedback <id> <useful|wrong>`:

   ```json
   { "conversation_id": "<id>", "turn_id": "<id>", "seq": 3, "verdict": "useful" }
   ```

   Desktop and TUI offer it only for a completed answer they are showing; the CLI reads the
   conversation, chooses the newest completed answer, and asks the capability report first.
   The orchestrator reads the turn back from the caller's conversation and accepts a
   verdict only for a completed answer. It derives the ref from the turn, never from the
   request, and resolves it: `useful` is `accepted` and `wrong` is `replaced`. The reply is
   `{turn_id, found, resolved, verdict}`. The first verdict stands; a second one reports
   it with `resolved: false`. `found: false` means an answer older than this feature.
3. **No verdict.** An answer nobody judges expires after `MEMORY_EXPIRE_AFTER` (720 h by
   default) and is **unscored**, never counted as wrong.
4. **Reading it.** The Notebook lists `chat.answer` experiences ("Answered in Talk, citing
   your documents") and scores them on their own card ("Answers you judged in Talk"), apart
   from hashtag suggestions. `memory.stats` accepts `chat.answer` or the default
   `hashtag.suggest`, and nothing else.

`conversation.capabilities` names `feedback` when memory is wired. Desktop and TUI offer
verdict controls only on an explicit `supported`; the CLI asks the same report before it
sends, since an older orchestrator would refuse the request.

## Privacy

The owner's questions in Desktop, CLI and TUI conversations are now also stored in the
memory service's database, which already holds owner messages for hashtag experiences. Its
file is created readable by its owner only. The questions are never logged.

## Verification

- Memory domain: ref stability, preparation bounds, frozen prediction, record accepted by
  memory's own validator, expired answers unscored.
- Orchestrator: a completed archived answer records exactly one experience with a frozen
  prediction, including across a replay; failed and channel answers record nothing;
  memory down never fails the answer; verdict mapping and the first verdict standing;
  refusals for a failed turn, another turn, an unknown verdict and a channel source,
  none reaching memory; capability report. Removing the recording call or swapping the
  mapping fails these tests.
- Desktop: shell payload and press gating (the screen cannot name an outcome or a ref),
  Talk buttons only with `feedback: supported`, the first verdict shown, an older answer,
  and the separate Notebook score.
- CLI: `conversations feedback` chooses the newest completed answer from the archive read,
  refuses unknown verdicts and unsupported orchestrators, sends the archive turn identity,
  and prints resolved, conflicting and older-answer replies.
- TUI: `u`/`w` are offered only inside an open conversation with a completed answer, ask
  the capability report first, hold duplicate presses while one verdict is in flight, and
  localize saved, conflicting, older-answer and unsupported results.
- Live, against a real `dokja-memory` process with a scratch database (embeddings off): an
  archived handler with a stubbed model answered 3 sends and 3 replays, and memory held 3
  experiences. Useful and wrong were kept, a second verdict reported the first, and the
  `chat.answer` score counted 2 scored answers with their frozen predictions. No real chat
  provider or Electron session was exercised in that run.

## Not yet

- Showing an earlier verdict when a conversation is reopened; Talk and the TUI learn it
  only when a verdict is submitted, and the CLI prints the standing verdict after a
  conflicting request.
- Using the score: `08.3` decides what an answer prediction that beats its baseline should
  change.
