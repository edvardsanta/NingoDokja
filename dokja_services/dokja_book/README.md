# Dokja Book

`dokja_book` is the specialized book-analysis service for Ningo Dokja.

Its first responsibility is not raw file parsing. It classifies book resources, selects the right summarization route, and prepares context-compaction strategy so the orchestrator can route book workflows precisely.

The classifier is local-first. It uses a Hugging Face zero-shot classification pipeline and keeps the model cached inside the `dokja_book` folder so it is not reloaded on every request.

The classifier and overview generator are both provider-based. They can stay local or use an external HTTP backend without changing the service contract.

Classification and summary are separate responsibilities and should use different models.

## Responsibilities

- classify book resource format
- classify book type for routing
- select extraction strategy for PDF / EPUB / MOBI / text-like resources
- select context compaction strategy
- produce an initial structured summary when book text is already available
- extract real content from supported local files when `content` is not provided

## Endpoints

### `GET /health`

Returns service health.

### `POST /books/classify`

Classifies a book resource and returns:

- `format`
- `book_type`
- `resource_strategy`
- `context_strategy`
- `agent_route`

Example request:

```json
{
  "title": "Clean Architecture",
  "filename": "clean-architecture.pdf",
  "goal": "study"
}
```

### `POST /books/summarize`

Builds a structured initial summary when text content is already available.

Example request:

```json
{
  "title": "Clean Architecture",
  "filename": "clean-architecture.pdf",
  "goal": "study",
  "content": "Long extracted text here..."
}
```

## Important note

This first version assumes the text is already extracted when calling `/books/summarize`.

If `content` is not provided, the service now tries to extract from a local resource path using:

- `PyMuPDF (fitz)` for `pdf` and `epub`
- `mobi` for `mobi`
- `markdown-it-py` for `md`
- direct file read for `txt`

## Environment

- `DOKJA_BOOK_HOST`
  - default: `0.0.0.0`
- `DOKJA_BOOK_PORT`
  - default: `8083`
- `DOKJA_BOOK_DEFAULT_LANGUAGE`
  - default: `pt-BR`
- `DOKJA_BOOK_DEFAULT_GOAL`
  - default: `study`
- `DOKJA_BOOK_CLASSIFIER_MODEL`
  - default service fallback: `facebook/bart-large-mnli`
  - dev stack default: `typeform/distilbert-base-uncased-mnli`
- `DOKJA_BOOK_CLASSIFIER_PROVIDER`
  - `local` or `external`
  - default: `local`
- `DOKJA_BOOK_CLASSIFIER_ENDPOINT`
  - required when `DOKJA_BOOK_CLASSIFIER_PROVIDER=external`
- `DOKJA_BOOK_MODEL_CACHE_DIR`
  - default: `./models`
- `DOKJA_BOOK_OVERVIEW_PROVIDER`
  - `extractive`, `transformers`, or `external`
  - default service fallback: `extractive`
  - dev stack default: `transformers`
- `DOKJA_BOOK_OVERVIEW_MODEL`
  - used when `DOKJA_BOOK_OVERVIEW_PROVIDER=transformers`
  - default: `sshleifer/distilbart-cnn-12-6`
- `DOKJA_BOOK_OVERVIEW_ENDPOINT`
  - required when `DOKJA_BOOK_OVERVIEW_PROVIDER=external`

## Local model behavior

`classify` uses the zero-shot classifier to choose:

- `book_type` from:
  - `technical`
  - `academic`
  - `fiction`
  - `non-fiction`
- `format` from:
  - `pdf`
  - `epub`
  - `mobi`
  - `txt`
  - `md`

The model is loaded once during service startup and reused for the life of the process.

## Overview generation

`summary.overview` is now generated through a provider abstraction:

- `extractive`
  - local, lightweight, no extra model beyond classification
- `transformers`
  - local abstractive summary via a summarization pipeline
- `external`
  - reserved for future agent/service adapters

This keeps the service flexible for both local and external summarization agents.

Recommended split:

- classification:
  - `facebook/bart-large-mnli`
  - or a smaller NLI model such as `typeform/distilbert-base-uncased-mnli`
- overview / summary:
  - a summarization model such as `sshleifer/distilbart-cnn-12-6`

Do not reuse the zero-shot classification model as the summary model.

## External provider contracts

Classifier external backend:

- `POST /classify/format`
- `POST /classify/book-type`

Accepted payload:

```json
{
  "title": "...",
  "content": "...",
  "filename": "..."
}
```

Accepted response shapes:

```json
{"label":"technical"}
```

or

```json
{"result":{"label":"technical"}}
```

Overview external backend:

- `POST /overview`

Accepted payload:

```json
{
  "content": "...",
  "sentences": ["...", "..."]
}
```

Accepted response shapes:

```json
{"summary":"..."}
```

or

```json
{"result":{"summary":"..."}}
```

## Extraction support

The service expects a readable local file path in either:

- `resource_uri`
- `filename`

It can also receive file bytes directly in:

- `resource_bytes_b64`

Supported extraction formats:

- `pdf`
- `epub`
- `mobi`
- `txt`
- `md`
