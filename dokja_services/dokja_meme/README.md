# Meme Service Python

This service is the extraction boundary for the legacy Dokja Lab meme system.

It does not rewrite the meme stack. It wraps the existing legacy components so the orchestrator can talk to memes through ZeroMQ request/reply instead of direct Flask or in-process calls.

## Legacy Components Reused

- `dokja_lab/handlers/meme_handler.py`
- `dokja_lab/workers/meme_worker.py`
- `dokja_lab/memes/*`
- `dokja_lab/infra/sqlite/storage.py`
- `dokja_lab/models/Meme.py`

## Responsibility

This service owns:

- meme pool refresh
- unsent meme retrieval
- sent-state updates
- meme service health/status

This service does not own interface rendering. The legacy Flask UI can continue to exist during migration, but the orchestrator should integrate through ZeroMQ only.

## Event Contract

The service accepts JSON requests over ZeroMQ `REP`.

Preferred orchestrator envelope:

```json
{
  "event_id": "uuid",
  "timestamp": "2026-03-10T12:00:00Z",
  "source": "orchestrator",
  "type": "meme.fetch",
  "user": {
    "id": "discord-user-id",
    "name": "Alice"
  },
  "channel": {
    "id": "discord-channel-id"
  },
  "payload": {
    "limit": 5
  },
  "context": {}
}
```

Supported event types:

- `meme.fetch`
  Returns unsent memes and marks them as sent.
- `meme.pool.refresh`
  Scrapes sources and stores new memes.
- `meme.status`
  Returns service health and current unsent count.
- `meme.screen`
  Runs the NSFW filter on one image URL and returns the verdict, the OCR text and the
  detections. See `dokja_lab/memes/safety.py`.
- `meme.list`
  Browses the pool (`unsent` or `sent`) without consuming it.
- `meme.mark_sent`
  Flags one meme as delivered without going through `meme.fetch`.
- `meme.hashtag.tag`, `meme.hashtag.suggest`, `meme.hashtag.list`, `meme.hashtag.untag`
  See "Hashtag suggestion" below.
- `meme.attachment.fetch`
  Downloads a pooled meme's raw bytes (base64-encoded) for a caller that cannot reach
  the origin host directly. See "Attachment fetch" below.

Response format:

```json
{
  "status": "ok",
  "result": {}
}
```

Error format:

```json
{
  "status": "error",
  "message": "description"
}
```

## Local Run

```bash
cd dokja_services/dokja_meme
python3 main.py
```

Environment variables:

- `MEME_SERVICE_ENDPOINT`
  Default: `tcp://*:5557`
- `MEME_SERVICE_DB_FILE`
  Default: `<repo>/ningo_memory.db`
- `MEME_SERVICE_SCRAPERS`
  Optional comma-separated list of scraper keys.
  When omitted, the service uses the scraper set defined in `dokja_lab/config.py`.
  When provided, the keys are resolved against that same legacy scraper registry.
- `MEME_HASHTAG_DB_FILE`
  Default: `<repo>/ningo_hashtags.db`. Its own file, separate from the meme pool.
- `DOKJA_EMBED_ENDPOINT` (default `http://127.0.0.1:11434`), `DOKJA_EMBED_MODEL`
  (default `bge-m3`), `DOKJA_EMBED_TIMEOUT`, `DOKJA_EMBED=off` (no embedder; tagging
  still stores text, suggestion is `degraded`). Same names `dokja_knowledge` uses, both
  services talk to the same Ollama server; see its README for why the client is not
  shared code.
- `MEME_HASHTAG_MIN_SCORE`
  Default `0.6`, provisional. See "Hashtag suggestion" below.
- `MEME_ATTACHMENT_FETCH=off`
  Disables `meme.attachment.fetch` (default `on`). See "Attachment fetch" below.
- `MEME_ATTACHMENT_MAX_BYTES` (default `20000000`), `MEME_ATTACHMENT_TIMEOUT`
  (default `30` seconds).

## Hashtag suggestion

The operator tags a meme's OCR text with a hashtag (`meme.hashtag.tag`); a new meme's
OCR text is compared to every tagged example by cosine similarity over `bge-m3`
embeddings, and the closest one's hashtag is suggested (`meme.hashtag.suggest`) only if
the score is at or above `MEME_HASHTAG_MIN_SCORE` (`relevant: true`). Below that, the
closest hashtag is still reported for visibility, but `relevant` is `false` and callers
must not present it as a match — the same absolute-relevance posture `dokja_knowledge`
uses for its search results.

The signal is the image's own printed text (read by the same OCR reader the NSFW filter
uses), not the scraper's title or tags: those are often meaningless (random keymashes,
or the scraper's own boilerplate), while the caption baked into the meme is what a
hashtag is actually about. `meme.hashtag.tag` and `.suggest` read it automatically from
`url`, or accept an explicit `text` to skip that (useful for testing, or when the
caller already has it from a prior `meme.screen` call). A meme with no legible text, or
a video without legible text, needs explicit `text` to be tagged or matched.
Videos use OCR on up to 12 frames spread across the clip, deduplicating repeated
lines. The same download size limit (8 MB) and timeout as image OCR apply. Audio
is not transcribed, and sampled OCR does not change the NSFW screening policy.
OCR downloads use the same browser-compatible `curl_cffi` client as attachment
delivery, including for images. OCR keeps its own 8 MB / 15 second download limits
and remains available when attachment fetching is disabled. Restart the meme
service after updating its code (rebuild its image when code is not bind-mounted).

Without an embedder (`DOKJA_EMBED=off`, or Ollama unreachable), tagging still stores
the text so nothing is lost, and `suggest` reports `degraded` with a `reason` instead
of guessing.

`MEME_HASHTAG_MIN_SCORE` (default `0.6`) is provisional. The small evaluation corpus
in `dokja_lab/tests/test_hashtags_live.py` is skipped without a reachable embedding
server; validate the threshold against representative tagged memes before relying on it.

Each suggestion embeds up to 32 pending examples (missing vectors or a different
model) alongside the query in one request. Subsequent suggestions recover the rest
after the configured model becomes available. Failed requests leave examples pending.
Tagging the same URL again corrects its label instead of adding a duplicate. Automatic
suggestions are never saved as training examples.

Manual and scheduled delivery append only relevant suggestions. Suggestion failures
leave the original message unchanged. Automatic OCR requires the text reader used
by the screening component; explicit `--text` input works without that reader.

## Attachment fetch

Some hosts put media downloads behind a client-fingerprint check (Cloudflare and
similar) that a plain HTTP client fails even with a correct `Referer`/`User-Agent`,
because it inspects the TLS/HTTP client signature itself, not just the headers. The
scraper's own session usually gets through, since it already has to; `meme.attachment.fetch`
reuses that same approach (`curl_cffi`, impersonating a browser) so a caller that only
has a URL — the Discord delivery interface, downloading a meme to attach it — can get
the bytes without hitting that check itself.

Scoped to URLs already in this service's own pool (`storage.exists`), so this is not a
generic unauthenticated fetch-any-URL endpoint. The orchestrator calls it before
delivering an attachment and falls back to sending the plain URL (the delivery
interface's own fetch) on any failure, so this being off or unreachable never blocks
delivery for hosts that do not need it.
