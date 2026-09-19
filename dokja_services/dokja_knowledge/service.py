"""Ingest documents and retrieve them by meaning and by keyword.

Retrieval is hybrid: cosine similarity over embeddings and an FTS5 keyword match are
merged by reciprocal rank fusion. Every hit also carries `relevant`, decided by an
absolute signal (the cosine score against a threshold, or how many of the query's
words the chunk contains), never by rank alone. Rank always returns *something*; a
caller that injects context into a prompt must only use hits marked relevant.
"""

from __future__ import annotations

import hashlib
import json
import logging
import re
import unicodedata

import numpy as np

from chunker import chunk_text
from embedder import EmbedUnavailable
from store import Store

logger = logging.getLogger("dokja_knowledge.service")

SOURCE_ID = re.compile(r"^[A-Za-z0-9._:/#@-]{1,200}$")
KIND = re.compile(r"^[a-z][a-z0-9_-]{0,31}$")
MAX_BODY_CHARS = 2_000_000
MAX_TITLE_CHARS = 300
MAX_QUERY_CHARS = 2_000
CANDIDATES = 30
RRF_K = 60
# Calibrated with bge-m3 (see README): paraphrases score 0.46-0.61, unrelated 0.27-0.41.
DEFAULT_MIN_SCORE = 0.44
DEFAULT_LEXICAL_COVERAGE = 0.75

STOPWORDS = frozenset(
    "a o as os um uma uns umas de do da dos das em no na nos nas por para com sem sobre entre "
    "e ou mas que se como qual quais quem onde quando porque pois ao aos ha ser sao foi era "
    "the of and to in is are was for on with as by at an be this that it from or not what how "
    "who why when where qual quais isso isto essa esse esta este seu sua meu minha".split()
)


def fold(text: str) -> str:
    """Lowercase and strip accents, matching how the keyword index tokenizes."""
    decomposed = unicodedata.normalize("NFKD", text.casefold())
    return "".join(ch for ch in decomposed if not unicodedata.combining(ch))


def terms(text: str) -> list[str]:
    seen: dict[str, None] = {}
    for token in re.findall(r"\w+", fold(text)):
        if len(token) >= 3 and token not in STOPWORDS:
            seen.setdefault(token, None)
    return list(seen)


def slug(title: str) -> str:
    text = re.sub(r"[^a-z0-9]+", "-", fold(title)).strip("-")
    return text[:120] or "untitled"


class KnowledgeService:
    def __init__(self, store: Store, embedder=None, model: str = "bge-m3",
                 min_score: float = DEFAULT_MIN_SCORE,
                 lexical_coverage: float = DEFAULT_LEXICAL_COVERAGE):
        self.store = store
        self.embedder = embedder
        self.model = model
        self.min_score = min_score
        self.lexical_coverage = lexical_coverage
        self._matrix_version = -1
        self._ids = np.zeros(0, dtype=np.int64)
        self._matrix = np.zeros((0, 0), dtype=np.float32)

    def dispatch(self, event: dict) -> dict:
        event_type = (event.get("type") or event.get("event_type") or "").strip()
        payload = event.get("payload") or {}
        handlers = {
            "knowledge.ingest": self.ingest,
            "knowledge.search": self.search,
            "knowledge.list": self.list_documents,
            "knowledge.delete": self.delete,
            "knowledge.status": lambda _payload: self.status(),
            "knowledge.reindex": self.reindex,
        }
        handler = handlers.get(event_type)
        if handler is None:
            raise ValueError(f"unsupported knowledge event type: {event_type}")
        return handler(payload)

    # ---- ingest ---------------------------------------------------------------------

    def ingest(self, payload: dict) -> dict:
        title = str(payload.get("title") or "").strip()
        body = str(payload.get("body") or "")
        if not title or len(title) > MAX_TITLE_CHARS:
            raise ValueError(f"title is required (up to {MAX_TITLE_CHARS} characters)")
        if not body.strip():
            raise ValueError("body is required")
        if len(body) > MAX_BODY_CHARS:
            raise ValueError(f"body is larger than {MAX_BODY_CHARS} characters")

        kind = str(payload.get("kind") or "note").strip().lower()
        if not KIND.match(kind):
            raise ValueError("kind must be a short lowercase word such as note, article or book")
        source_ref = str(payload.get("source_ref") or "").strip()[:2000]
        source_id = str(payload.get("source_id") or "").strip() or f"{kind}:{slug(title)}"
        if not SOURCE_ID.match(source_id):
            raise ValueError("source_id may only use letters, digits and . _ : / # @ -")
        tags = self._tags(payload.get("tags"))

        digest = hashlib.sha256(
            json.dumps([title, kind, source_ref, tags, body], ensure_ascii=False).encode()
        ).hexdigest()
        existing = self.store.get_document(source_id)
        if existing is not None and existing["content_hash"] == digest:
            return {"source_id": source_id, "created": False, "changed": False,
                    "pending_embeddings": len(self.store.chunks_needing_embedding(self.model))}

        chunks = chunk_text(body)
        if not chunks:
            raise ValueError("the document has no text to store")
        document_id, created = self.store.replace_document(
            source_id, title, kind, source_ref, ",".join(tags), digest, chunks
        )
        embedded, degraded, reason = self._embed_pending()
        result = {"source_id": source_id, "document_id": document_id, "created": created,
                  "changed": True, "chunks": len(chunks), "embedded": embedded, "degraded": degraded}
        if reason:
            result["reason"] = reason
        return result

    @staticmethod
    def _tags(raw) -> list[str]:
        if raw is None:
            return []
        items = raw.split(",") if isinstance(raw, str) else list(raw)
        tags = []
        for item in items:
            tag = str(item).strip()
            if tag and len(tag) <= 40 and "," not in tag and tag not in tags:
                tags.append(tag)
        if len(tags) > 20:
            raise ValueError("a document takes at most 20 tags")
        return tags

    def _embed_input(self, row) -> str:
        return "\n".join(part for part in (row["title"], row["heading"], row["text"]) if part)

    def _embed_pending(self, limit: int | None = None) -> tuple[int, bool, str]:
        """Embed chunks that have no vector from the current model. Never raises."""
        pending = self.store.chunks_needing_embedding(self.model, limit)
        if not pending:
            return 0, False, ""
        if self.embedder is None:
            return 0, True, "no embedding server is configured"
        try:
            matrix = self.embedder.embed([self._embed_input(row) for row in pending])
        except EmbedUnavailable as err:
            logger.warning("embedding skipped for %d chunks: %s", len(pending), err)
            return 0, True, str(err)
        self.store.set_embeddings([row["id"] for row in pending], matrix, self.model)
        return len(pending), False, ""

    def reindex(self, payload: dict) -> dict:
        """Embed everything still missing a vector, for example after Ollama was down."""
        limit = payload.get("limit")
        embedded, degraded, reason = self._embed_pending(int(limit) if limit else None)
        result = {"embedded": embedded, "remaining": len(self.store.chunks_needing_embedding(self.model)),
                  "degraded": degraded}
        if reason:
            result["reason"] = reason
        return result

    # ---- search ---------------------------------------------------------------------

    def search(self, payload: dict) -> dict:
        query = str(payload.get("query") or "").strip()
        if not query or len(query) > MAX_QUERY_CHARS:
            raise ValueError(f"query is required (up to {MAX_QUERY_CHARS} characters)")
        k = min(max(int(payload.get("k") or 5), 1), 20)
        min_score = float(payload.get("min_score") if payload.get("min_score") is not None else self.min_score)

        semantic, degraded, reason = self._semantic_candidates(query)
        query_terms = terms(query)
        fts_query = " OR ".join(f'"{term}"' for term in query_terms)
        keyword = dict(self.store.keyword_search(fts_query, CANDIDATES))

        # Reciprocal rank fusion over the two rankings.
        fused: dict[int, float] = {}
        for rank, chunk_id in enumerate(sorted(semantic, key=semantic.get, reverse=True)):
            fused[chunk_id] = fused.get(chunk_id, 0.0) + 1.0 / (RRF_K + rank + 1)
        for rank, chunk_id in enumerate(sorted(keyword, key=keyword.get)):  # lower bm25 is better
            fused[chunk_id] = fused.get(chunk_id, 0.0) + 1.0 / (RRF_K + rank + 1)
        ordered = sorted(fused, key=fused.get, reverse=True)[:k]

        rows = self.store.chunks_by_ids(ordered)
        wanted = set(query_terms)
        hits = []
        for position, chunk_id in enumerate(ordered):
            row = rows[chunk_id]
            present = wanted & set(terms(f"{row['title']} {row['heading']} {row['text']}"))
            coverage = len(present) / len(wanted) if wanted else 0.0
            score = semantic.get(chunk_id)
            relevant = (score is not None and score >= min_score) or (
                len(present) >= 2 and coverage >= self.lexical_coverage
            )
            hits.append({
                "rank": position + 1,
                "source_id": row["source_id"], "title": row["title"], "kind": row["kind"],
                "source_ref": row["source_ref"], "tags": [t for t in row["tags"].split(",") if t],
                "position": row["position"], "heading": row["heading"], "text": row["text"],
                "score": round(score, 4) if score is not None else None,
                "coverage": round(coverage, 3), "relevant": bool(relevant),
            })
        result = {"query": query, "hits": hits, "degraded": degraded, "threshold": min_score,
                  "relevant_count": sum(1 for hit in hits if hit["relevant"])}
        if reason:
            result["reason"] = reason
        return result

    def _semantic_candidates(self, query: str) -> tuple[dict[int, float], bool, str]:
        """chunk id -> cosine for the closest chunks. Empty and degraded if embeddings are out."""
        if self.embedder is None:
            return {}, True, "no embedding server is configured; keyword search only"
        try:
            vector = self.embedder.embed([query])
        except EmbedUnavailable as err:
            logger.warning("semantic search unavailable: %s", err)
            return {}, True, f"{err}; keyword search only"
        ids, matrix = self._embeddings()
        if len(ids) == 0 or matrix.shape[1] != vector.shape[1]:
            return {}, False, ""
        scores = matrix @ vector[0]
        top = np.argsort(-scores)[:CANDIDATES]
        return {int(ids[i]): float(scores[i]) for i in top}, False, ""

    def _embeddings(self) -> tuple[np.ndarray, np.ndarray]:
        if self._matrix_version != self.store.version:
            self._ids, self._matrix = self.store.embedding_matrix(self.model)
            self._matrix_version = self.store.version
        return self._ids, self._matrix

    # ---- housekeeping ---------------------------------------------------------------

    def list_documents(self, payload: dict) -> dict:
        limit = min(max(int(payload.get("limit") or 20), 1), 100)
        offset = max(int(payload.get("offset") or 0), 0)
        documents = [
            {"source_id": row["source_id"], "title": row["title"], "kind": row["kind"],
             "source_ref": row["source_ref"], "tags": [t for t in row["tags"].split(",") if t],
             "chunks": row["chunks"], "embedded": row["embedded"] or 0, "updated_at": row["updated_at"]}
            for row in self.store.list_documents(limit, offset)
        ]
        return {"documents": documents, "total": self.store.stats(self.model)["documents"],
                "offset": offset}

    def delete(self, payload: dict) -> dict:
        source_id = str(payload.get("source_id") or "").strip()
        if not source_id:
            raise ValueError("source_id is required")
        return {"source_id": source_id, "deleted": self.store.delete_document(source_id)}

    def status(self) -> dict:
        stats = self.store.stats(self.model)
        reachable = None
        if self.embedder is not None and hasattr(self.embedder, "reachable"):
            reachable = self.embedder.reachable()
        return {**stats, "pending_embeddings": stats["chunks"] - stats["embedded"],
                "embed_model": self.model, "embedder_reachable": reachable,
                "threshold": self.min_score}
