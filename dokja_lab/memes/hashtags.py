"""Suggests a hashtag for a meme by nearest neighbour over its OCR text.

The operator tags examples (an image's text -> a hashtag such as "#GuiConversinhas");
a new meme's text is compared against every tagged example and, if the closest one is
similar enough, its hashtag is suggested. Below the threshold nothing is suggested: a
meme unlike anything tagged so far gets no guess rather than a wrong one, the same
absolute-relevance posture dokja_knowledge uses for its search results.

This is a single-nearest-neighbour classifier, not a general retrieval index: `suggest`
returns one hashtag (or none), not a ranked list of candidates.
"""

from __future__ import annotations

import os
import sqlite3
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

import numpy as np

from logging_config import get_logger

logger = get_logger(__name__)

# Provisional: not yet calibrated against real hashtag examples (unlike
# dokja_knowledge's threshold, which was measured on real bge-m3 output). Retune once
# there is a real set of tagged memes; see the README.
DEFAULT_MIN_SCORE = 0.6
LEARNING_BATCH_SIZE = 32

MIGRATIONS = [
    """
    CREATE TABLE IF NOT EXISTS hashtag_examples (
        id          INTEGER PRIMARY KEY,
        source_url  TEXT NOT NULL UNIQUE,
        text        TEXT NOT NULL,
        hashtag     TEXT NOT NULL,
        embedding   BLOB,
        embed_model TEXT,
        created_at  TEXT NOT NULL,
        updated_at  TEXT NOT NULL
    );
    """,
]


def normalize_hashtag(raw: str) -> str:
    """ "#Name" or "Name" -> "#Name". Letters, digits and underscore only after the "#"."""
    tag = str(raw or "").strip()
    if not tag:
        raise ValueError("hashtag is required")
    if not tag.startswith("#"):
        tag = "#" + tag
    body = tag[1:]
    if not body or not all(ch.isalnum() or ch == "_" for ch in body):
        raise ValueError(
            f"invalid hashtag {tag!r}: only letters, digits and _ are allowed after the #"
        )
    return tag


@dataclass(frozen=True)
class Example:
    source_url: str
    text: str
    hashtag: str
    vector: np.ndarray


class HashtagClassifier:
    """Stores tagged examples in its own SQLite file and suggests a hashtag for new text.

    `embedder` is optional, matching dokja_knowledge's degraded posture: without one (or
    while it is unreachable) tagging still stores the example so nothing is lost, just
    without a vector, and suggestion reports `degraded` instead of guessing.
    """

    def __init__(
        self,
        db_file: str,
        embedder: Any = None,
        model: str = "bge-m3",
        min_score: float = DEFAULT_MIN_SCORE,
    ) -> None:
        if db_file != ":memory:":
            directory = os.path.dirname(os.path.abspath(db_file))
            os.makedirs(directory, exist_ok=True)
        self.conn = sqlite3.connect(db_file, check_same_thread=False)
        self.conn.row_factory = sqlite3.Row
        self.conn.execute("PRAGMA busy_timeout = 5000")
        if db_file != ":memory:":
            self.conn.execute("PRAGMA journal_mode = WAL")
        self._migrate()
        self.embedder = embedder
        self.model = model
        self.min_score = min_score

    def close(self) -> None:
        self.conn.close()

    def _migrate(self) -> None:
        current = self.conn.execute("PRAGMA user_version").fetchone()[0]
        for index in range(current, len(MIGRATIONS)):
            with self.conn:
                self.conn.executescript(MIGRATIONS[index])
                self.conn.execute(f"PRAGMA user_version = {index + 1}")

    # ---- training ---------------------------------------------------------------

    def tag(self, source_url: str, hashtag: str, text: str) -> dict:
        source_url = str(source_url or "").strip()
        if not source_url:
            raise ValueError("source_url is required")
        hashtag = normalize_hashtag(hashtag)
        text = str(text or "").strip()
        if not text:
            raise ValueError("text is required: this meme has no text to learn from")

        vector, embedded, reason = self._embed_one(text)
        now = datetime.now(timezone.utc).isoformat(timespec="seconds")
        blob = vector.tobytes() if vector is not None else None
        with self.conn:
            self.conn.execute(
                """
                INSERT INTO hashtag_examples
                    (source_url, text, hashtag, embedding, embed_model, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(source_url) DO UPDATE SET
                    text=excluded.text, hashtag=excluded.hashtag, embedding=excluded.embedding,
                    embed_model=excluded.embed_model, updated_at=excluded.updated_at
                """,
                (
                    source_url,
                    text,
                    hashtag,
                    blob,
                    self.model if embedded else None,
                    now,
                    now,
                ),
            )
        logger.info(
            "hashtag tagged url=%s hashtag=%s embedded=%s",
            source_url,
            hashtag,
            embedded,
        )
        result = {
            "source_url": source_url,
            "hashtag": hashtag,
            "text": text,
            "embedded": embedded,
        }
        if reason:
            result["degraded"] = True
            result["reason"] = reason
        return result

    def delete(self, source_url: str) -> bool:
        source_url = str(source_url or "").strip()
        if not source_url:
            raise ValueError("source_url is required")
        with self.conn:
            cursor = self.conn.execute(
                "DELETE FROM hashtag_examples WHERE source_url = ?", (source_url,)
            )
        return cursor.rowcount > 0

    def list_examples(self, limit: int = 50, offset: int = 0) -> dict:
        limit = min(max(int(limit or 50), 1), 200)
        offset = max(int(offset or 0), 0)
        total = self.conn.execute("SELECT COUNT(*) FROM hashtag_examples").fetchone()[0]
        rows = self.conn.execute(
            "SELECT source_url, text, hashtag, embed_model, updated_at"
            " FROM hashtag_examples ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?",
            (limit, offset),
        ).fetchall()
        return {
            "total": total,
            "offset": offset,
            "examples": [
                {
                    "source_url": row["source_url"],
                    "text": row["text"],
                    "hashtag": row["hashtag"],
                    "embedded": row["embed_model"] == self.model,
                    "updated_at": row["updated_at"],
                }
                for row in rows
            ],
        }

    # ---- suggestion ---------------------------------------------------------------

    def suggest(self, text: str, min_score: float | None = None) -> dict:
        text = str(text or "").strip()
        if not text:
            raise ValueError("text is required")
        threshold = self.min_score if min_score is None else float(min_score)
        example_count = self.conn.execute(
            "SELECT COUNT(*) FROM hashtag_examples"
        ).fetchone()[0]

        if self.embedder is None:
            return self._degraded(
                text, threshold, example_count, "no embedding server is configured"
            )

        # Recover operator-labelled examples in bounded batches alongside the query.
        # One embedding call keeps an unavailable server from multiplying timeouts.
        pending = self.conn.execute(
            "SELECT source_url, text FROM hashtag_examples"
            " WHERE embedding IS NULL OR embed_model IS NULL OR embed_model != ?"
            " ORDER BY id LIMIT ?",
            (self.model, LEARNING_BATCH_SIZE),
        ).fetchall()
        try:
            vectors = np.asarray(
                self.embedder.embed([text] + [row["text"] for row in pending]),
                dtype=np.float32,
            )
            if (
                vectors.ndim != 2
                or len(vectors) != len(pending) + 1
                or vectors.shape[1] == 0
                or not np.isfinite(vectors).all()
                or np.any(np.linalg.norm(vectors, axis=1) == 0)
            ):
                raise ValueError("embedding server returned invalid learning vectors")
            vectors /= np.linalg.norm(vectors, axis=1, keepdims=True)
        except Exception as err:
            logger.warning("hashtag learning unavailable: %s", err)
            return self._degraded(text, threshold, example_count, str(err))

        with self.conn:
            for row, learned in zip(pending, vectors[1:]):
                self.conn.execute(
                    "UPDATE hashtag_examples SET embedding = ?, embed_model = ?"
                    " WHERE source_url = ? AND text = ?",
                    (learned.tobytes(), self.model, row["source_url"], row["text"]),
                )
        vector = vectors[0]
        if pending:
            logger.info("hashtag learning recovered examples=%d", len(pending))

        examples = self._embedded_examples()
        if not examples:
            return self._degraded(
                text,
                threshold,
                example_count,
                "no tagged examples with an embedding from this model yet",
                degraded=False,
            )

        matrix = np.stack([example.vector for example in examples])
        scores = matrix @ vector
        best = int(np.argmax(scores))
        score = float(scores[best])
        winner = examples[best]
        return {
            "query_text": text,
            "hashtag": winner.hashtag,
            "score": round(score, 4),
            "relevant": score >= threshold,
            "threshold": threshold,
            "degraded": False,
            "examples": len(examples),
            "matched_source_url": winner.source_url,
            "matched_text": winner.text,
        }

    @staticmethod
    def _degraded(
        text: str,
        threshold: float,
        example_count: int,
        reason: str,
        degraded: bool = True,
    ) -> dict:
        return {
            "query_text": text,
            "hashtag": None,
            "score": None,
            "relevant": False,
            "threshold": threshold,
            "degraded": degraded,
            "reason": reason,
            "examples": example_count,
        }

    def _embed_one(self, text: str) -> tuple[np.ndarray | None, bool, str]:
        if self.embedder is None:
            return None, False, "no embedding server is configured"
        try:
            vector = self.embedder.embed([text])[0]
        except (
            Exception
        ) as err:  # the embedder's own EmbedUnavailable, or anything else
            logger.warning("hashtag embedding failed: %s", err)
            return None, False, str(err)
        return vector, True, ""

    def _embedded_examples(self) -> list[Example]:
        rows = self.conn.execute(
            "SELECT source_url, text, hashtag, embedding FROM hashtag_examples"
            " WHERE embedding IS NOT NULL AND embed_model = ?",
            (self.model,),
        ).fetchall()
        return [
            Example(
                source_url=row["source_url"],
                text=row["text"],
                hashtag=row["hashtag"],
                vector=np.frombuffer(row["embedding"], dtype=np.float32),
            )
            for row in rows
        ]
