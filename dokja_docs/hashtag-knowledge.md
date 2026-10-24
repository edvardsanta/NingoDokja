# Hashtag examples belong to Knowledge

Step 09.4-f moves labelled-example lifecycle rules out of the Meme workflow and
into the [Knowledge domain](../dokja_domain/dokja_knowledge/hashtag.go), following
the accepted [ADR 0007](decisions/0007-faculties-not-features.md) direction.
The broader four-concept proposal in ADR 0010 remains proposed.

The domain prepares a validated `HashtagExample` before the orchestrator asks a
media adapter for text. Blank sources and invalid labels therefore produce no
OCR or storage call. Labels keep the existing optional leading `#`, Unicode
letter/digit and underscore rules. The orchestrator supplies explicit text or
sequences OCR, then asks Knowledge to store the prepared example.

Knowledge owns the legacy `meme_hashtag` kind, `meme-hashtag` tag and source ID:
`meme-hashtag:` followed by the first 32 hexadecimal characters of the SHA-256
of the trimmed source reference. It does not canonicalize the URL or change
existing document IDs. Tagging, deletion and task evidence use this same domain
function, so stored labels and their references remain compatible.

Search is restricted to that example kind with `k=1`. It returns the nearest
candidate even when the provider reports `relevant: false`; empty results keep
the existing absence reason and degraded-provider reasons are retained. The
domain preserves the existing result fields and embedding receipt behavior.
Listing requests only this kind with text and the existing paging arguments,
then projects the same example fields. Deleting addresses the same stored ID.
Service errors are returned without inventing a successful receipt.

The workflow retains the Knowledge enabled-state check, OCR sequencing, search
time budget and existing threshold recipe parameters. Candidate relevance and
delivery selection are not changed by this ownership extraction. Search/list
do not write examples or record feedback. No new public event or domain action
is introduced; `meme.hashtag.*` events and the generic Knowledge service events
continue through the current routes and adapters.

Compatibility review: existing source identities, event envelopes, command
arguments, errors, response shapes, service payloads and schema are unchanged.
There is no configuration, Compose, port or volume change and no backfill.
Rolling back the orchestrator retains the same labelled examples. Domain and
workflow tests cover label validity, fixed legacy IDs, unchanged/pending
embedding receipts, retrieval scope, below-threshold candidates, empty/degraded
results, paging, deletion, and validation before effects. All fixtures are
synthetic; no live OCR, model or owner database is used.
