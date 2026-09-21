"""SQLite storage for documents, their chunks, embeddings and a keyword index.

This service owns its database file. It is deliberately not part of the shared
dokja.db, whose schema version is managed by the Go side.
"""

from __future__ import annotations

import os
import sqlite3
from datetime import datetime, timezone

import numpy as np

from chunker import Chunk

MIGRATIONS = [
    """
    CREATE TABLE IF NOT EXISTS documents (
        id           INTEGER PRIMARY KEY,
        source_id    TEXT NOT NULL UNIQUE,
        title        TEXT NOT NULL,
        kind         TEXT NOT NULL,
        source_ref   TEXT NOT NULL,
        tags         TEXT NOT NULL,
        content_hash TEXT NOT NULL,
        created_at   TEXT NOT NULL,
        updated_at   TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS chunks (
        id          INTEGER PRIMARY KEY,
        document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
        position    INTEGER NOT NULL,
        heading     TEXT NOT NULL,
        text        TEXT NOT NULL,
        embedding   BLOB,
        embed_model TEXT
    );
    CREATE INDEX IF NOT EXISTS chunks_by_document ON chunks(document_id, position);
    CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
        text, heading, content='chunks', content_rowid='id',
        tokenize='unicode61 remove_diacritics 2'
    );
    CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
        INSERT INTO chunks_fts(rowid, text, heading) VALUES (new.id, new.text, new.heading);
    END;
    CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
        INSERT INTO chunks_fts(chunks_fts, rowid, text, heading)
        VALUES ('delete', old.id, old.text, old.heading);
    END;
    CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE OF text, heading ON chunks BEGIN
        INSERT INTO chunks_fts(chunks_fts, rowid, text, heading)
        VALUES ('delete', old.id, old.text, old.heading);
        INSERT INTO chunks_fts(rowid, text, heading) VALUES (new.id, new.text, new.heading);
    END;
    """,
]


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


class Store:
    def __init__(self, path: str):
        if path != ":memory:":
            directory = os.path.dirname(os.path.abspath(path))
            os.makedirs(directory, exist_ok=True)
        self.path = path
        self.conn = sqlite3.connect(path, check_same_thread=False)
        self.conn.row_factory = sqlite3.Row
        self.conn.execute("PRAGMA foreign_keys = ON")
        self.conn.execute("PRAGMA busy_timeout = 5000")
        if path != ":memory:":
            self.conn.execute("PRAGMA journal_mode = WAL")
        self._migrate()
        # Bumped by every write, so the in-memory embedding matrix knows when to rebuild.
        self.version = 0

    def close(self) -> None:
        self.conn.close()

    def _migrate(self) -> None:
        current = self.conn.execute("PRAGMA user_version").fetchone()[0]
        if current > len(MIGRATIONS):
            raise RuntimeError(
                f"knowledge database is version {current}, newer than this build understands"
            )
        for index in range(current, len(MIGRATIONS)):
            with self.conn:
                self.conn.executescript(MIGRATIONS[index])
                self.conn.execute(f"PRAGMA user_version = {index + 1}")

    # ---- documents ------------------------------------------------------------------

    def get_document(self, source_id: str) -> sqlite3.Row | None:
        return self.conn.execute("SELECT * FROM documents WHERE source_id = ?", (source_id,)).fetchone()

    def replace_document(self, source_id: str, title: str, kind: str, source_ref: str, tags: str,
                         content_hash: str, chunks: list[Chunk]) -> tuple[int, bool]:
        """Create or fully replace a document and its chunks, atomically.

        Returns (document id, created). Old chunks are removed first, so an edited
        document never keeps stale text or embeddings.
        """
        stamp = _now()
        with self.conn:
            existing = self.get_document(source_id)
            if existing is None:
                cursor = self.conn.execute(
                    "INSERT INTO documents(source_id, title, kind, source_ref, tags, content_hash,"
                    " created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                    (source_id, title, kind, source_ref, tags, content_hash, stamp, stamp),
                )
                document_id, created = cursor.lastrowid, True
            else:
                document_id, created = existing["id"], False
                self.conn.execute("DELETE FROM chunks WHERE document_id = ?", (document_id,))
                self.conn.execute(
                    "UPDATE documents SET title=?, kind=?, source_ref=?, tags=?, content_hash=?,"
                    " updated_at=? WHERE id=?",
                    (title, kind, source_ref, tags, content_hash, stamp, document_id),
                )
            self.conn.executemany(
                "INSERT INTO chunks(document_id, position, heading, text) VALUES (?, ?, ?, ?)",
                [(document_id, c.position, c.heading, c.text) for c in chunks],
            )
        self.version += 1
        return document_id, created

    def delete_document(self, source_id: str) -> bool:
        with self.conn:
            existing = self.get_document(source_id)
            if existing is None:
                return False
            # Explicit, so the keyword-index triggers fire for every chunk.
            self.conn.execute("DELETE FROM chunks WHERE document_id = ?", (existing["id"],))
            self.conn.execute("DELETE FROM documents WHERE id = ?", (existing["id"],))
        self.version += 1
        return True

    def list_documents(self, limit: int, offset: int) -> list[sqlite3.Row]:
        return self.conn.execute(
            "SELECT d.*, COUNT(c.id) AS chunks,"
            " SUM(CASE WHEN c.embedding IS NOT NULL THEN 1 ELSE 0 END) AS embedded"
            " FROM documents d LEFT JOIN chunks c ON c.document_id = d.id"
            " GROUP BY d.id ORDER BY d.updated_at DESC, d.id DESC LIMIT ? OFFSET ?",
            (limit, offset),
        ).fetchall()

    def stats(self, model: str) -> dict:
        row = self.conn.execute(
            "SELECT (SELECT COUNT(*) FROM documents) AS documents,"
            " (SELECT COUNT(*) FROM chunks) AS chunks,"
            " (SELECT COUNT(*) FROM chunks WHERE embedding IS NOT NULL AND embed_model = ?) AS embedded",
            (model,),
        ).fetchone()
        return dict(row)

    # ---- embeddings -----------------------------------------------------------------

    def chunks_needing_embedding(self, model: str, limit: int | None = None) -> list[sqlite3.Row]:
        """Chunks with no embedding, or one made by a different model."""
        sql = (
            "SELECT c.id, c.text, c.heading, d.title FROM chunks c"
            " JOIN documents d ON d.id = c.document_id"
            " WHERE c.embedding IS NULL OR c.embed_model IS NOT ? ORDER BY c.id"
        )
        params: tuple = (model,)
        if limit is not None:
            sql += " LIMIT ?"
            params += (limit,)
        return self.conn.execute(sql, params).fetchall()

    def set_embeddings(self, chunk_ids: list[int], matrix: np.ndarray, model: str) -> None:
        with self.conn:
            self.conn.executemany(
                "UPDATE chunks SET embedding = ?, embed_model = ? WHERE id = ?",
                [(np.asarray(vector, dtype=np.float32).tobytes(), model, chunk_id)
                 for chunk_id, vector in zip(chunk_ids, matrix)],
            )
        self.version += 1

    def embedding_matrix(self, model: str) -> tuple[np.ndarray, np.ndarray]:
        """(chunk ids, matrix) of every chunk embedded by `model`. Rows are unit vectors."""
        rows = self.conn.execute(
            "SELECT id, embedding FROM chunks WHERE embedding IS NOT NULL AND embed_model = ? ORDER BY id",
            (model,),
        ).fetchall()
        if not rows:
            return np.zeros(0, dtype=np.int64), np.zeros((0, 0), dtype=np.float32)
        ids = np.array([row["id"] for row in rows], dtype=np.int64)
        matrix = np.stack([np.frombuffer(row["embedding"], dtype=np.float32) for row in rows])
        return ids, matrix

    # ---- keyword search -------------------------------------------------------------

    def keyword_search(self, fts_query: str, limit: int) -> list[tuple[int, float]]:
        """(chunk id, bm25) best first. bm25 is lower for better matches in SQLite."""
        if not fts_query:
            return []
        rows = self.conn.execute(
            "SELECT rowid, bm25(chunks_fts) AS score FROM chunks_fts WHERE chunks_fts MATCH ?"
            " ORDER BY score LIMIT ?",
            (fts_query, limit),
        ).fetchall()
        return [(row["rowid"], row["score"]) for row in rows]

    def chunks_by_ids(self, ids: list[int]) -> dict[int, sqlite3.Row]:
        if not ids:
            return {}
        marks = ",".join("?" for _ in ids)
        rows = self.conn.execute(
            f"SELECT c.id, c.position, c.heading, c.text, d.source_id, d.title, d.kind, d.source_ref, d.tags"
            f" FROM chunks c JOIN documents d ON d.id = c.document_id WHERE c.id IN ({marks})",
            ids,
        ).fetchall()
        return {row["id"]: row for row in rows}
