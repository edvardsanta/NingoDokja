"""Explicit, verified Knowledge import and rollback export; never run at startup."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sqlite3
import tempfile
from contextlib import closing
from pathlib import Path

import numpy as np
from postgres_store import PostgresStore
from store import Store

DOCUMENT_FIELDS = (
    "id",
    "source_id",
    "title",
    "kind",
    "source_ref",
    "tags",
    "content_hash",
    "created_at",
    "updated_at",
    "metadata",
)
CHUNK_FIELDS = (
    "id",
    "document_id",
    "position",
    "heading",
    "text",
    "embedding",
    "embed_model",
)


def _digest(documents, chunks):
    digest = hashlib.sha256()
    for row in documents:
        digest.update(
            json.dumps(list(row), ensure_ascii=False, separators=(",", ":")).encode()
        )
        digest.update(b"\n")
    for row in chunks:
        values = list(row)
        if values[5] is not None:
            values[5] = np.asarray(values[5], dtype=np.float32).tobytes().hex()
        digest.update(
            json.dumps(values, ensure_ascii=False, separators=(",", ":")).encode()
        )
        digest.update(b"\n")
    return digest.hexdigest()


def import_sqlite(source_path: str, target: PostgresStore) -> dict:
    # A missing path cannot silently create an empty source. A read transaction
    # sees a consistent WAL snapshot without modifying the original database.
    uri = Path(source_path).resolve().as_uri() + "?mode=ro"
    with closing(sqlite3.connect(uri, uri=True)) as source:
        source.execute("BEGIN")
        version = source.execute("PRAGMA user_version").fetchone()[0]
        if version not in {1, 2}:
            raise ValueError("SQLite Knowledge schema must be version 1 or 2")
        metadata = "'{}'" if version == 1 else "COALESCE(m.metadata, '{}')"
        join = (
            ""
            if version == 1
            else "LEFT JOIN document_metadata m ON m.document_id=d.id"
        )
        documents = source.execute(
            f"SELECT d.*, {metadata} AS metadata FROM documents d {join} ORDER BY d.id"
        ).fetchall()
        chunks = []
        for row in source.execute("SELECT * FROM chunks ORDER BY id"):
            values = list(row)
            if values[5] is not None:
                vector = np.frombuffer(values[5], dtype=np.float32).copy()
                if not len(vector) or not np.isfinite(vector).all():
                    raise ValueError(
                        "SQLite contains an invalid embedding; import refused"
                    )
                values[5] = vector
            chunks.append(tuple(values))
        expected = _digest(documents, chunks)
        with target.conn.transaction():
            # Exclude application writes for the entire import and verification.
            target.conn.execute(
                "LOCK TABLE documents, chunks, document_metadata IN EXCLUSIVE MODE"
            )
            if target.document_count() or target.stats("")["chunks"]:
                raise ValueError(
                    "PostgreSQL Knowledge target must be empty; import never overwrites data"
                )
            with target.conn.cursor() as cursor:
                cursor.executemany(
                    """
                    INSERT INTO documents(id, source_id, title, kind, source_ref, tags,
                                          content_hash, created_at, updated_at)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
                """,
                    [row[:9] for row in documents],
                )
                cursor.executemany(
                    "INSERT INTO document_metadata VALUES (%s, %s)",
                    [(row[0], row[9]) for row in documents],
                )
                cursor.executemany(
                    """
                    INSERT INTO chunks(id, document_id, position, heading, text, embedding, embed_model)
                    VALUES (%s, %s, %s, %s, %s, %s, %s)
                """,
                    chunks,
                )
            actual_documents = target.conn.execute("""
                SELECT d.*, COALESCE(m.metadata, '{}') AS metadata FROM documents d
                LEFT JOIN document_metadata m ON m.document_id=d.id ORDER BY d.id
            """).fetchall()
            actual_chunks = target.conn.execute("""
                SELECT id, document_id, position, heading, text, embedding, embed_model
                FROM chunks ORDER BY id
            """).fetchall()
            actual = _digest(
                [
                    tuple(row[field] for field in DOCUMENT_FIELDS)
                    for row in actual_documents
                ],
                [tuple(row[field] for field in CHUNK_FIELDS) for row in actual_chunks],
            )
            if actual != expected:
                raise RuntimeError(
                    "Knowledge import verification failed; transaction rolled back"
                )
            target.conn.execute("""
                SELECT setval(pg_get_serial_sequence('documents','id'),
                              COALESCE(MAX(id), 1), COUNT(*) > 0) FROM documents
            """)
            target.conn.execute("""
                SELECT setval(pg_get_serial_sequence('chunks','id'),
                              COALESCE(MAX(id), 1), COUNT(*) > 0) FROM chunks
            """)
    target.version += 1
    return {
        "verified": True,
        "documents": len(documents),
        "chunks": len(chunks),
        "digest": expected,
    }


def export_sqlite(destination_path: str, source: PostgresStore) -> dict:
    """Publish a new verified SQLite snapshot, never replacing an existing file."""
    destination = Path(destination_path).resolve()
    if destination.exists():
        raise FileExistsError("SQLite export destination already exists")
    descriptor, temporary = tempfile.mkstemp(
        prefix=".knowledge-export-", dir=destination.parent
    )
    os.close(descriptor)
    target = None
    try:
        target = Store(temporary)
        with source.conn.transaction():
            source.conn.execute(
                "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"
            )
            documents = [
                tuple(row[field] for field in DOCUMENT_FIELDS)
                for row in source.conn.execute("""
                SELECT d.*, COALESCE(m.metadata, '{}') AS metadata FROM documents d
                LEFT JOIN document_metadata m ON m.document_id=d.id ORDER BY d.id
            """).fetchall()
            ]
            chunks = [
                tuple(row[field] for field in CHUNK_FIELDS)
                for row in source.conn.execute("""
                SELECT id, document_id, position, heading, text, embedding, embed_model
                FROM chunks ORDER BY id
            """).fetchall()
            ]
            expected = _digest(documents, chunks)
            with target.conn:
                target.conn.executemany(
                    """
                    INSERT INTO documents VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                    [row[:9] for row in documents],
                )
                target.conn.executemany(
                    "INSERT INTO document_metadata VALUES (?, ?)",
                    [(row[0], row[9]) for row in documents],
                )
                target.conn.executemany(
                    "INSERT INTO chunks VALUES (?, ?, ?, ?, ?, ?, ?)",
                    [
                        (
                            *row[:5],
                            (
                                None
                                if row[5] is None
                                else np.asarray(row[5], dtype=np.float32).tobytes()
                            ),
                            row[6],
                        )
                        for row in chunks
                    ],
                )
                actual_documents = target.conn.execute("""
                    SELECT d.*, COALESCE(m.metadata, '{}') AS metadata FROM documents d
                    LEFT JOIN document_metadata m ON m.document_id=d.id ORDER BY d.id
                """).fetchall()
                actual_chunks = []
                for row in target.conn.execute("SELECT * FROM chunks ORDER BY id"):
                    values = list(row)
                    if values[5] is not None:
                        values[5] = np.frombuffer(values[5], dtype=np.float32)
                    actual_chunks.append(values)
                if _digest(actual_documents, actual_chunks) != expected:
                    raise RuntimeError("Knowledge export verification failed")
        target.close()
        target = None
        # Atomic no-replace publication on the same filesystem.
        os.link(temporary, destination)
        return {
            "verified": True,
            "documents": len(documents),
            "chunks": len(chunks),
            "digest": expected,
        }
    finally:
        if target is not None:
            target.close()
        for suffix in ("", "-wal", "-shm"):
            Path(temporary + suffix).unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    direction = parser.add_mutually_exclusive_group(required=True)
    direction.add_argument("--sqlite-source")
    direction.add_argument("--sqlite-export")
    args = parser.parse_args()
    target = PostgresStore(os.environ.get("KNOWLEDGE_POSTGRES_DSN", ""))
    try:
        result = (
            import_sqlite(args.sqlite_source, target)
            if args.sqlite_source
            else export_sqlite(args.sqlite_export, target)
        )
        print(json.dumps(result))
    finally:
        target.close()


if __name__ == "__main__":
    main()
