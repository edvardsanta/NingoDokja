# Media admissibility ownership

Step 09.4-e implements part of the accepted
[ADR 0007](decisions/0007-faculties-not-features.md) migration: interpreting a
media screening observation belongs to Moderation. The broader four-concept
reclassification in ADR 0010 remains proposed.

The [Moderation media policy](../dokja_domain/dokja_moderation/media.go)
returns a typed `MediaDecision` with `Safe` and `Reason`. Its `MediaScreener`
port is implemented by the existing Meme/Feeds adapter. The orchestrator
supplies the provider and the current enabled-state snapshot, sequences the
workflow, and passes the assessment to Communications destination policy.
There are no direct calls between domains.

| Input | Decision |
| --- | --- |
| Live screen returns `safe: true` without an error | Safe, with an empty refusal reason |
| Missing screener or disabled Meme service | Unsafe, without invoking the provider |
| Provider error, including an error accompanying `safe: true` | Unsafe, retaining the existing error prefix |
| Live screen returns false, a missing value or a non-boolean `safe` | Unsafe, with the provider reason or the existing fallback |
| Cached media label has boolean `safe` | Use that value and retain its reason verbatim |
| Cached label is missing or has a non-boolean `safe` | Unsafe, with `no nsfw label` |

Live and cached refusal reasons preserve their existing differences. The policy
does not infer safety from a missing result or convert a provider failure into
permission. Communications still owns configured destinations, disabled routing,
safe-only requirements and explicit operator bypass authorization. Scheduled
delivery does not request a bypass.

The System handler's public `MemeScreener` port remains an alias, so existing
clients and wiring continue to work. No new route or workflow action is needed
for this internal ownership change. Event envelopes, response fields, refusal
strings, provider HTTP/plugin contracts, commands, schema, environment, Compose
services and volumes are unchanged. Model execution, thresholds and OCR remain
in the current perception adapter. The Meme facade, intake cache and delivery
receipt persistence have not moved in this batch.

Synthetic tests cover missing and malformed labels, provider errors, disabled
providers, context/caption forwarding, safe-only destination refusal, unrestricted
delivery, manual bypass authorization and scheduled delivery. No live account,
model or owner database is exercised.
