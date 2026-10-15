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
- `meme.text.read`
  Internal orchestrator capability that returns OCR text without running hashtag storage
  or retrieval. Hashtag labels belong to the knowledge service.
- `meme.list`
  Browses the pool (`unsent` or `sent`) without consuming it.
- `meme.mark_sent`
  Flags one meme as delivered without going through `meme.fetch`.
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
- `DOKJA_OCR_MODEL_DIR`
  Folder with the OCR models (`ch_PP-OCRv3_det_infer.onnx`, `ch_PP-OCRv3_rec_infer.onnx`,
  `ch_ppocr_mobile_v2.0_cls_infer.onnx`). Unset, the models bundled with the OCR package are used.
- `DOKJA_OCR_REC_MODEL`
  Optional file name, inside `DOKJA_OCR_MODEL_DIR`, of another text recognizer. See "OCR
  recognizer" below.
- `MEME_ATTACHMENT_FETCH=off`
  Disables `meme.attachment.fetch` (default `on`). See "Attachment fetch" below.
- `MEME_ATTACHMENT_MAX_BYTES` (default `20000000`), `MEME_ATTACHMENT_TIMEOUT`
  (default `30` seconds).

## Hashtag suggestion

The public `meme.hashtag.*` events are orchestrator workflows, not meme-service events.
The meme service supplies only OCR through `meme.text.read`. The orchestrator stores an
operator's label as `kind=meme_hashtag` in the shared knowledge base, retrieves similar
labelled examples through `knowledge.search`, and records a suggestion that was actually
used through the Prediction/Experience memory loop. This keeps media integration,
knowledge and learned outcomes in their respective layers.

`MEME_HASHTAG_MIN_SCORE` is now an orchestrator setting (default `0.6`). The knowledge
service owns embeddings, keyword fallback and reindexing. If embeddings are unavailable,
the explicit label remains stored and retrieval degrades to Knowledge's keyword search.

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

### OCR recognizer

The recognizer that ships in the OCR models folder only knows Chinese and English letters,
so it reads Portuguese without its accents ("coração" becomes "coracao"). That is enough
for the blacklist, which folds accents, and for similarity, but the text shown as "read"
is not what is printed. Set `DOKJA_OCR_REC_MODEL` to the file name of a recognizer trained
on the Latin alphabet, placed in `DOKJA_OCR_MODEL_DIR`, to keep them. The file must carry
its character list (the ONNX `character` metadata, as RapidOCR's converted models do);
the detector and the orientation classifier stay as they are because they do not depend on
the language. A Latin recognizer cannot read Chinese or Japanese, which is why this is
opt-in. A missing file fails the screen closed, like a missing model folder.

Changing the recognizer does not rewrite what was already learned. A tagged example keeps
the text it was tagged with: run `meme.hashtag.tag` again on its `url` without `text` to
read it again and replace the example. Contexts in the experience memory are immutable and
stay as they were recorded.

Tagging the same URL again corrects its label instead of adding a duplicate. Automatic
suggestions are never saved as training examples.

Manual and scheduled delivery append only relevant suggestions. Suggestion failures
leave the original message unchanged. Automatic OCR requires the text reader used
by the screening component; explicit `--text` input works without that reader.

Legacy `ningo_hashtags.db` files are no longer read at runtime. With the orchestrator
running, migrate one through the interface contract:

```bash
python3 scripts/migrate_meme_hashtags.py ningo_hashtags.db --apply
```

The source file is opened read-only and remains available for rollback.

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
