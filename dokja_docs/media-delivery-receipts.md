# Confirmed media delivery receipts

Step 09.4-h separates retrieving cached media from acknowledging its delivery.
The owner approved the breaking behavior change: `meme.fetch` returns unsent
items without marking them sent. The bundled Feeds media library and optional
legacy Meme service both implement this behavior. Response shapes and existing
public event, route and action names stay unchanged.

The [Memory receipt policy](../dokja_domain/dokja_memory/media_receipt.go)
requires a requested receipt, a nonempty media reference, an enabled recorder
and at least one confirmed transport success. The orchestrator supplies the
observed success count and sequences the existing receipt adapter. It never
counts a transport error, timeout, blocked destination or disabled interface as
confirmation. The existing public `MemeMarker` name remains a port alias.

| Outcome | Receipt behavior |
| --- | --- |
| Fetch, browse or screening only | No write |
| Scheduled send succeeds for at least one destination | Record once for that media item |
| All sends fail, time out, are blocked or are disabled | No receipt |
| One destination succeeds and another fails or is unknown | Record the known success; keep reporting the aggregate delivery error |
| Manual send with `mark_sent: true` and a confirmed destination | Record, including partial success |
| Manual send without `mark_sent`, or text without media | No receipt |
| Receipt adapter fails | No successful receipt is claimed; log the failure and do not resend within the workflow |

A successful scheduled item is marked after its sends and before reporting
other destination errors. This prevents a failure elsewhere from erasing known
success. Existing safe-only, destination authorization, hashtag selection and
feedback rules remain unchanged.

## State and limits

The existing `meme.sent_count` and `date_sent` fields remain the transitional
receipt store. Duplicate ingestion preserves them and host restarts retain
them. This batch moves the decision into Memory, not the physical table into
the Memory service. Existing flags are retained without backfill or reset;
older versions may have set them during retrieval, so they are not proof of
historical delivery. The flag is media-level, not a per-destination ledger.

Successful delivery and persisting its receipt are separate effects. A crash
between them or a receipt-write failure can leave delivered media unsent, and
a later dispatch may send it again. An unknown transport outcome also remains
unsent. There is no durable reservation, distributed exclusion or exactly-once
guarantee. Reading alone no longer reserves media: operate one dispatcher for
this queue. A durable attempt/outcome ledger and receipt storage migration
remain separate work; this change does not claim those guarantees.

## Rollout and rollback

Update the orchestrator and selected reader together, including any custom
`media.library.v1` provider. An older orchestrator paired with the new reader
does not acknowledge scheduled items; a new orchestrator with an older reader
still consumes items before delivery. Stop competing dispatchers during the
switch. Roll back the pair together; old readers restore consume-on-fetch.
There is no schema, environment, port, volume or Compose change. No live account,
owner database or production deployment is touched by this implementation.

Regression fixtures cover read-only fetch across real plugin host restarts,
receipt persistence, legacy backend reads, policy refusals, safe-only blocking,
failed/unknown outcomes, partial confirmation, explicit manual recording and
receipt failure without an immediate resend.
