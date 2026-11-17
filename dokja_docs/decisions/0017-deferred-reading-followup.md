# ADR 0017: deferred reading with a proactive follow-up reply

Date: 2026-11-17. Status: proposed; no code exists. Related:
[ADR 0001](./0001-task-workspace.md),
[ADR 0006](./0006-reading-macro-component.md),
[capability map](../capability-map.md) (reading links, messages delivery).

## Context

A chat turn that carries links reads them synchronously, inside one bounded
budget (`webReadBudget`, 15 seconds). When a page is slow, cold or protected,
the turn reports the failure and the conversation moves on; nothing comes back
later. The owner asked for the opposite behavior: Ningo should be able to say
"Não tenho isso agora, vou verificar e volto", finish the turn, and post a
follow-up message in the channel when the content finally arrives — referencing
the original command or link.

Two incidents motivate this:

- A re-sent article link that had been read an hour earlier failed its fresh
  read and the reply degraded to "I could not open the link", losing the
  conversation thread (fixed separately by keeping earlier readings; this ADR
  covers the case where there is no kept copy).
- Instagram reels and similar pages frequently sit behind login walls or slow
  readers; a synchronous turn can only apologize.

The architecture constrains the design: the only flow is
`Interface → Event → Orchestrator → Domain → Service`. A follow-up message is
not a reply to a request; it is the orchestrator driving an interface
outbound. Proactive delivery already exists for that direction: the
communications deliverer posts to Discord's delivery endpoint, and the task
runner already knows how to admit, order, block and settle background work.

## Decision

1. **Eligibility is the chat domain's, and it is narrow.** A deferred reading
   is offered only when a fresh read failed with a *transient* reason
   (`timeout`, `blocked` or `unavailable`) — never for permanent refusals,
   `unreadable` decodes, `too_large`, disabled readers, or failures where a
   kept copy from an earlier read was served. The model cannot promise a
   follow-up on its own: the application decides, closes the turn with the
   honest partial answer plus one fixed sentence ("vou tentar de novo e volto
   aqui"), and records the deferred task. When the domain declines, the turn
   behaves exactly as today.
2. **The orchestrator owns a volatile deferred-reading registry, keyed by
   channel and URL.** An entry holds the channel, the original message and
   event ids, the question text, the URL and an expiry (bounded, minutes —
   stale follow-ups are worse than none). At most one pending entry per
   (channel, URL); at most a small number per channel. A restart forgets the
   registry: a lost follow-up is a lost nicety, not state, so nothing is
   persisted and no recovery is attempted (ADR 0003's no-new-durable-state
   boundary is preserved). The loss is logged.
3. **A background task re-reads with a longer budget.** The task runs outside
   the turn's context and its 15-second budget, through the same
   `readChatLink` path and the same untrusted-evidence framing — no new reader,
   no new trust level. Social readers stay first, plain web reader second.
   Feeds remains the only fetcher; media is still not downloaded by a chat
   turn (a delivered post stays caption + vision passages, as today).
4. **The follow-up reply is generated fresh, with citations, and delivered
   proactively.** On success the task asks the chat domain for a short reply
   over the new evidence (bounded prompt, same quotation verification), then
   sends it through the communications deliverer to the original channel. On
   failure the task posts one fixed honest notice ("não consegui ler mesmo;
   o link continua indisponível") instead of going silent — the channel must
   never be left with a promise and no settlement. Moderation blocks settle
   the task without any delivery, as the task runner already does.
5. **Correlation is best-effort and additive.** The delivery contract gains an
   optional `reply_to_message_id`. Discord's bot-token path posts it as a
   `message_reference`; the webhook path ignores it (webhooks cannot reply).
   Interfaces without proactive delivery (WhatsApp, Desktop, CLI) do not offer
   deferred readings at all — eligibility is per-interface, and the capability
   catalog states where it exists.
6. **The follow-up never rewrites history.** It is a new message, not an edit
   of the original reply; the session transcript records the promise and the
   settlement as ordinary turns. The generated follow-up passes the same
   citation and quotation checks as any reading reply, and its evidence is
   merged into the session's kept reading set under ADR-merge semantics
   (a fresh read updates its own URL).

## Consequences

- A turn can end before all its evidence arrived; the conversation gains a
  second, asynchronous voice. The fixed promise and settlement sentences keep
  that voice honest and bounded — the model improvises neither.
- Failure surface grows: the registry can be lost, the delivery can fail, the
  follow-up generation can fail. Every path settles with either a delivery or
  a logged drop; none blocks the originating turn, which has already ended.
- The public envelope, routes and event contracts are unchanged; the delivery
  contract only gains an optional field. No new durable stores, no new env
  vars beyond the existing delivery configuration.

## Open questions

- The exact cap and expiry (entries per channel, minutes until a promise
  expires) need a trial value; start conservative (one per channel, 10
  minutes) and adjust from use.
- Whether the follow-up should also be offered when a kept copy exists but the
  speaker explicitly asks for a fresh read (today the kept copy is served with
  an outdated-content notice). Leaning no: the notice already says what is
  missing.
- WhatsApp proactive delivery would need its own outbound path decision and is
  out of scope until asked.
