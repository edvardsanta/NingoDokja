"""SQLite storage for documents, their chunks, embeddings and a keyword index.

This service owns its database file. It is deliberately not part of the shared
dokja.db, whose schema version is managed by the Go side.
"""

from __future__ import annotations

import os
import hashlib
import json
import sqlite3
from datetime import datetime, timezone

import numpy as np

from chunker import Chunk
from metadata import encode_metadata

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
    """
    CREATE TABLE document_metadata (
        document_id INTEGER PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
        metadata TEXT NOT NULL
    );
    """,
]


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


class Store:
    backend = "sqlite"

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
            try:
                self.conn.executescript(f"""
                    BEGIN IMMEDIATE;
                    {MIGRATIONS[index]}
                    PRAGMA user_version = {index + 1};
                    COMMIT;
                """)
            except Exception:
                self.conn.rollback()
                raise

    # ---- documents ------------------------------------------------------------------

    def get_document(self, source_id: str) -> sqlite3.Row | None:
        return self.conn.execute("SELECT * FROM documents WHERE source_id = ?", (source_id,)).fetchone()

    def replace_document(self, source_id: str, title: str, kind: str, source_ref: str, tags: str,
                         content_hash: str, chunks: list[Chunk], metadata: str = "{}") -> tuple[int, bool]:
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
            self._write_metadata(document_id, metadata)
        self.version += 1
        return document_id, created

    def get_metadata(self, document_id: int) -> str:
        row = self.conn.execute(
            "SELECT metadata FROM document_metadata WHERE document_id = ?", (document_id,)
        ).fetchone()
        return row["metadata"] if row is not None else "{}"

    def _write_metadata(self, document_id: int, metadata: str) -> None:
        self.conn.execute(
            """INSERT INTO document_metadata(document_id, metadata) VALUES (?, ?)
               ON CONFLICT(document_id) DO UPDATE SET metadata = excluded.metadata""",
            (document_id, metadata),
        )

    def update_metadata(self, document_id: int, metadata: str) -> bool:
        """Update defining metadata without replacing chunks or invalidating embeddings."""
        if self.get_metadata(document_id) == metadata:
            return False
        with self.conn:
            self._write_metadata(document_id, metadata)
            self.conn.execute("UPDATE documents SET updated_at = ? WHERE id = ?", (_now(), document_id))
        return True

    def metadata_snapshot(self, source_id: str) -> dict:
        """One read binds metadata to the content identity the user is answering for."""
        row = self.conn.execute(
            """SELECT d.source_id, d.title, d.content_hash,
                      COALESCE(m.metadata, '{}') AS metadata
               FROM documents d LEFT JOIN document_metadata m ON m.document_id = d.id
               WHERE d.source_id = ?""", (source_id,),
        ).fetchone()
        if row is None:
            raise ValueError("knowledge document not found")
        metadata = json.loads(row["metadata"])
        identity = json.dumps([row["source_id"], row["content_hash"], metadata],
                              ensure_ascii=False, sort_keys=True, separators=(",", ":"))
        return {"source_id": row["source_id"], "title": row["title"], "metadata": metadata,
                "revision": hashlib.sha256(identity.encode()).hexdigest()}

    def classification_sample(self, source_id: str) -> dict:
        """Bind a bounded chunk reconstruction to metadata in one read transaction."""
        self.conn.execute("BEGIN")
        try:
            snapshot = self.metadata_snapshot(source_id)
            document = self.get_document(source_id)
            parts = []
            remaining = 16000
            rows = self.conn.execute(
                "SELECT substr(text, 1, 16000) AS text FROM chunks "
                "WHERE document_id = ? ORDER BY position", (document["id"],),
            )
            for row in rows:
                part = row["text"][:remaining]
                parts.append(part)
                remaining -= len(part) + 2
                if remaining <= 0:
                    break
            result = {**snapshot, "text": "\n\n".join(parts)[:16000],
                      "representation": "chunk_reconstruction"}
            self.conn.commit()
            return result
        except Exception:
            self.conn.rollback()
            raise

    def patch_metadata(self, source_id: str, patch: dict, revision: str) -> dict:
        """Compare and patch under a write lock, leaving content and vectors intact."""
        self.conn.execute("BEGIN IMMEDIATE")
        try:
            snapshot = self.metadata_snapshot(source_id)
            if snapshot["revision"] != revision:
                raise ValueError("classification_conflict: document changed; reload the questions")
            merged = {**snapshot["metadata"], **patch}
            encoded = encode_metadata(merged)
            changed = merged != snapshot["metadata"]
            if changed:
                document = self.get_document(source_id)
                self._write_metadata(document["id"], encoded)
                self.conn.execute("UPDATE documents SET updated_at = ? WHERE id = ?",
                                  (_now(), document["id"]))
            result = self.metadata_snapshot(source_id)
            self.conn.commit()
        except Exception:
            self.conn.rollback()
            raise
        return {**result, "metadata_changed": changed, "changed": False}

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

    def list_documents(
        self,
        limit: int,
        offset: int,
        kind: str = "",
        exclude: tuple[str, ...] = (),
        source_id: str = "",
    ) -> list[sqlite3.Row]:
        # The kinds to leave out travel as one JSON array, so the query stays a fixed text with
        # every value bound.
        return self.conn.execute(
            """
            SELECT d.*, COALESCE(m.metadata, '{}') AS metadata, COUNT(c.id) AS chunks,
                   SUM(CASE WHEN c.embedding IS NOT NULL THEN 1 ELSE 0 END) AS embedded
            FROM documents d
            LEFT JOIN chunks c ON c.document_id = d.id
            LEFT JOIN document_metadata m ON m.document_id = d.id
            WHERE (? = '' OR d.kind = ?)
              AND d.kind NOT IN (SELECT value FROM json_each(?))
              AND (? = '' OR d.source_id = ?)
            GROUP BY d.id
            ORDER BY d.updated_at DESC, d.id DESC
            LIMIT ? OFFSET ?
            """,
            (kind, kind, json.dumps(list(exclude)), source_id, source_id, limit, offset),
        ).fetchall()

    def document_count(
        self, kind: str = "", exclude: tuple[str, ...] = (), source_id: str = ""
    ) -> int:
        return self.conn.execute(
            """
            SELECT COUNT(*) FROM documents
            WHERE (? = '' OR kind = ?) AND kind NOT IN (SELECT value FROM json_each(?))
              AND (? = '' OR source_id = ?)
            """,
            (kind, kind, json.dumps(list(exclude)), source_id, source_id),
        ).fetchone()[0]

    def document_text(self, document_id: int) -> str:
        rows = self.conn.execute(
            "SELECT heading, text FROM chunks WHERE document_id = ? ORDER BY position",
            (document_id,),
        ).fetchall()
        return "\n\n".join(
            "\n".join(part for part in (row["heading"], row["text"]) if part)
            for row in rows
        )

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

    def embedding_matrix(self, model: str, kind: str = "") -> tuple[np.ndarray, np.ndarray]:
        """(chunk ids, matrix) of every chunk embedded by `model`. Rows are unit vectors."""
        rows = self.conn.execute(
            """
            SELECT c.id, c.embedding
            FROM chunks c
            JOIN documents d ON d.id = c.document_id
            WHERE c.embedding IS NOT NULL
              AND c.embed_model = ?
              AND (? = '' OR d.kind = ?)
            ORDER BY c.id
            """,
            (model, kind, kind),
        ).fetchall()
        if not rows:
            return np.zeros(0, dtype=np.int64), np.zeros((0, 0), dtype=np.float32)
        ids = np.array([row["id"] for row in rows], dtype=np.int64)
        matrix = np.stack([np.frombuffer(row["embedding"], dtype=np.float32) for row in rows])
        return ids, matrix

    # ---- keyword search -------------------------------------------------------------

    def keyword_search(self, fts_query: str, limit: int, kind: str = "") -> list[tuple[int, float]]:
        """(chunk id, bm25) best first. bm25 is lower for better matches in SQLite."""
        if not fts_query:
            return []
        rows = self.conn.execute(
            """
            SELECT chunks_fts.rowid, bm25(chunks_fts) AS score
            FROM chunks_fts
            JOIN chunks c ON c.id = chunks_fts.rowid
            JOIN documents d ON d.id = c.document_id
            WHERE chunks_fts MATCH ?
              AND (? = '' OR d.kind = ?)
            ORDER BY score
            LIMIT ?
            """,
            (fts_query, kind, kind, limit),
        ).fetchall()
        return [(row["rowid"], row["score"]) for row in rows]

    def chunks_by_ids(self, ids: list[int]) -> dict[int, sqlite3.Row]:
        if not ids:
            return {}
        marks = ",".join("?" for _ in ids)
        rows = self.conn.execute(
            f"""SELECT c.id, c.position, c.heading, c.text,
                       d.source_id, d.title, d.kind, d.source_ref, d.tags,
                       COALESCE(m.metadata, '{{}}') AS metadata
                FROM chunks c JOIN documents d ON d.id = c.document_id
                LEFT JOIN document_metadata m ON m.document_id = d.id
                WHERE c.id IN ({marks})""",
            ids,
        ).fetchall()
        return {row["id"]: row for row in rows}
