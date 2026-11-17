# ADR 0018: come back when a watched search's first prices arrive

Date: 2026-11-17. Status: implemented in `feat/price-notification`.
Related: [ADR 0015](./0015-price-history-domain.md),
[ADR 0017](./0017-deferred-reading-followup.md).

## Context

`/preco` (`/price`) answers only from what the collectors already recorded. When the
owner's search adds a new watched search, the command collects it at once and the
reply says either what was found or "as lojas não trouxeram nada" — and the
conversation ends there. The owner asked for the ADR 0017 behavior on the price
flow: when Ningo says "não tem agora", it should come back by itself when there is
something.

The pieces exist. Watch-on-demand is how the watched list grows (ADR 0015), the
intake task collects one new watch immediately, proactive delivery exists since
ADR 0017 (`DeliverReply` with `reply_to_message_id`), and the resilient registry
pattern (persisted snapshot, boot re-arm) exists since the ADR 0017 review.

## Decision

1. **The promise is registered orchestrator-side, at one deterministic moment:**
   a Discord search whose watch was added, collected at once and returned zero
   products. The orchestrator sees all three facts in the search result; the
   interface needs no new request shape. Permanent cases (watch full, not owner,
   store refusal) never promise.
2. **A durable registry holds one pending notification per channel and watched
   search**, the same resilient pattern as ADR 0017: a snapshot beside the runtime
   databases, restored and re-armed on boot, metadata only (channel, correlation,
   watch, registered-at), a small cap and a seven-day expiry — a price answer that
   arrives next month is not a follow-up anymore.
3. **The intake fires the notification.** After a run records new observations for
   the watched searches, the notifier re-runs the pending searches whose watch just
   collected data; a search with products composes a fixed follow-up — cheapest
   offers per matching product, prices in reais, a pointer back to the `/preco`
   command — and delivers it as a reply to the original command message. A search
   that still returns nothing keeps its notification for the next run.
4. **The follow-up is a fixed template, not a generation.** Price replies in the
   interfaces are already templates (the slash command composes them without a
   model); the follow-up keeps that voice and stays honest by construction. Only
   the application composes and sends it.
5. **Where it exists is stated in the capability catalogs**, like ADR 0017's
   deferred reading: Discord only, owner watches only, one per channel and search,
   seven days of life.

## Consequences

- The watched list can grow into a promise, and the promise settles without anyone
  re-asking. The settlement is a new message, never an edit of the command reply.
- The notification's content is bounded by the same search limits as the command
  itself; it can only report recorded history, never invented offers.
- No event envelope, route or command contract changes: the registration is
  orchestrator-side, the delivery reuses the ADR 0017 endpoint, and the intake
  gains one internal call. One interface sentence changes ("eu aviso aqui quando
  chegar").
