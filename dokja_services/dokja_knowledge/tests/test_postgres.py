"""Application integration tests. Each test owns a new disposable database."""

import hashlib
import json
import os
import uuid
from concurrent.futures import ThreadPoolExecutor

import numpy as np
import pytest
from fakes import FakeEmbedder
from service import KnowledgeService
from store import Store

# Optional imports must stay below the skip guards for SQLite-only test runs.
# isort: off
psycopg = pytest.importorskip("psycopg")
pytest.importorskip("pgvector")
from psycopg import sql
from psycopg.conninfo import make_conninfo

from migrate_postgres import export_sqlite, import_sqlite
from postgres_store import PostgresStore

# isort: on


@pytest.fixture
def postgres_dsn():
    base = os.getenv("KNOWLEDGE_TEST_POSTGRES_DSN")
    if not base:
        pytest.skip("KNOWLEDGE_TEST_POSTGRES_DSN is not configured")
    name = "knowledge_test_" + uuid.uuid4().hex
    with psycopg.connect(base, autocommit=True) as admin:
        admin.execute(sql.SQL("CREATE DATABASE {}").format(sql.Identifier(name)))
        try:
            yield make_conninfo(base, dbname=name)
        finally:
            admin.execute(
                sql.SQL("DROP DATABASE {} WITH (FORCE)").format(sql.Identifier(name))
            )


@pytest.fixture
def pg_store(postgres_dsn):
    store = PostgresStore(postgres_dsn)
    yield store
    store.close()


def ingest(
    service,
    source_id="study",
    body="Physics research evidence and observations.",
    **extra
):
    return service.ingest(
        {"source_id": source_id, "title": "Study", "body": body, **extra}
    )


def test_application_ingest_search_replace_delete_and_restart(pg_store, postgres_dsn):
    service = KnowledgeService(pg_store, FakeEmbedder(), model="fake")
    result = ingest(service, tags=["physics"])
    assert result["created"] and result["embedded"] > 0
    assert not ingest(service, tags=["physics"])["changed"]
    # The application must not load PostgreSQL vectors into the SQLite matrix path.
    service._embeddings = lambda *_: pytest.fail("application-side vector scan used")
    result = service.search({"query": "physics research evidence"})
    assert result["hits"][0]["source_id"] == "study" and result["hits"][0]["relevant"]
    assert result["hits"][0]["score"] > 0.5
    other = PostgresStore(postgres_dsn)
    try:
        reopened = KnowledgeService(other, FakeEmbedder(), model="fake")
        assert reopened.list_documents({"include_text": True})["documents"][0][
            "tags"
        ] == ["physics"]
        ingest(reopened, body="Cooking carrots and chocolate cake.")
        hits = service.search({"query": "carrots chocolate cake"})["hits"]
        assert hits[0]["relevant"] and "carrots" in hits[0]["text"]
        assert service.delete({"source_id": "study"})["deleted"]
        assert reopened.search({"query": "carrots chocolate cake"})["hits"] == []
    finally:
        other.close()


def test_keyword_fallback_handles_accents_and_kind_filter(pg_store):
    service = KnowledgeService(pg_store, None, model="fake")
    ingest(
        service, body="Investigação científica sobre pressão atmosférica.", kind="note"
    )
    ingest(
        service,
        source_id="label",
        body="Investigação científica pressão atmosférica.",
        kind="meme_hashtag",
    )
    found = service.search(
        {"query": "investigacao cientifica pressao atmosferica", "kind": "note"}
    )
    assert found["degraded"] and len(found["hits"]) == 1
    assert found["hits"][0]["source_id"] == "study" and found["hits"][0]["relevant"]
    assert service.search({"query": "unseenword"})["hits"] == []


def test_model_dimensions_and_zero_vectors_do_not_leak_candidates(pg_store):
    service = KnowledgeService(pg_store, FakeEmbedder(8), model="old")
    ingest(service, kind="note")
    other = KnowledgeService(pg_store, FakeEmbedder(16), model="new")
    ingest(
        other,
        source_id="other",
        body="Different physics evidence.",
        kind="meme_hashtag",
    )
    # Ingest reindexes pending chunks for its selected model. Keep an explicit
    # mixed-model fixture to exercise SQL model and dimension exclusion.
    chunk_id = pg_store.conn.execute(
        "SELECT id FROM chunks WHERE document_id=1"
    ).fetchone()["id"]
    pg_store.set_embeddings(
        [chunk_id], np.ones((1, 8), dtype=np.float32) / np.sqrt(8), "old"
    )
    assert pg_store.vector_search(np.zeros(8, dtype=np.float32), "old", "note", 10)
    assert (
        pg_store.vector_search(np.ones(16, dtype=np.float32), "old", "note", 10) == {}
    )
    assert (
        pg_store.vector_search(np.ones(8, dtype=np.float32), "old", "meme_hashtag", 10)
        == {}
    )
    result = service.search({"query": "physics evidence", "kind": "note"})
    assert {hit["source_id"] for hit in result["hits"]} == {"study"}
    service.model = "new"
    service.embedder = FakeEmbedder(16)
    assert service.search({"query": "physics evidence", "kind": "note"})["degraded"]
    assert service.reindex({})["remaining"] == 0


def test_metadata_compare_and_patch_has_only_one_winner(pg_store, postgres_dsn):
    service = KnowledgeService(pg_store, FakeEmbedder(), model="fake")
    ingest(service)
    before = pg_store.conn.execute(
        "SELECT id, embedding::text AS embedding FROM chunks"
    ).fetchall()
    revision = pg_store.metadata_snapshot("study")["revision"]
    other = PostgresStore(postgres_dsn)
    try:

        def apply(store, topic):
            try:
                return store.patch_metadata("study", {"topics": [topic]}, revision)
            except ValueError as error:
                assert "classification_conflict" in str(error)
                return "conflict"

        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(
                pool.map(
                    lambda args: apply(*args),
                    [(pg_store, "physics"), (other, "science")],
                )
            )
        assert results.count("conflict") == 1
        assert (
            pg_store.conn.execute(
                "SELECT id, embedding::text AS embedding FROM chunks"
            ).fetchall()
            == before
        )
        assert (
            pg_store.classification_sample("study")["representation"]
            == "chunk_reconstruction"
        )
    finally:
        other.close()


def test_verified_import_preserves_sqlite_ids_metadata_vectors_and_file(
    pg_store, tmp_path
):
    path = tmp_path / "knowledge.db"
    source = Store(str(path))
    service = KnowledgeService(source, FakeEmbedder(), model="fake")
    ingest(service, metadata={"topics": ["physics"]})
    ingest(service, source_id="pending", body="Unembedded evidence.")
    source.conn.execute(
        "UPDATE chunks SET embedding=NULL, embed_model=NULL WHERE document_id=2"
    )
    source.conn.commit()
    revision = source.metadata_snapshot("study")["revision"]
    original = service.search({"query": "physics research evidence"})["hits"][0]
    source.close()
    before = hashlib.sha256(path.read_bytes()).digest()
    result = import_sqlite(str(path), pg_store)
    assert result["verified"] and result["documents"] == 2
    assert hashlib.sha256(path.read_bytes()).digest() == before
    assert pg_store.metadata_snapshot("study")["revision"] == revision
    migrated = KnowledgeService(pg_store, FakeEmbedder(), model="fake")
    hit = migrated.search({"query": "physics research evidence"})["hits"][0]
    assert (
        hit["source_id"] == original["source_id"] and hit["score"] == original["score"]
    )
    assert len(pg_store.chunks_needing_embedding("fake")) == 1
    with pytest.raises(ValueError, match="must be empty"):
        import_sqlite(str(path), pg_store)
    ingest(migrated, source_id="later", body="Later physics evidence.")
    assert migrated.list_documents({})["total"] == 3


def test_newer_schema_is_refused_without_modifying_it(pg_store, postgres_dsn):
    pg_store.conn.execute("UPDATE schema_version SET version=999")
    with pytest.raises(RuntimeError, match="not supported"):
        PostgresStore(postgres_dsn)
    assert (
        pg_store.conn.execute("SELECT version FROM schema_version").fetchone()[
            "version"
        ]
        == 999
    )


def test_bootstrap_and_request_envelope_use_postgres_without_opening_sqlite(
    postgres_dsn, monkeypatch, tmp_path
):
    from bootstrap import build_service
    from server import KnowledgeServiceServer

    source = tmp_path / "untouched.db"
    monkeypatch.setenv("KNOWLEDGE_STORAGE_BACKEND", "postgres")
    monkeypatch.setenv("KNOWLEDGE_POSTGRES_DSN", postgres_dsn)
    monkeypatch.setenv("KNOWLEDGE_DB_FILE", str(source))
    monkeypatch.setenv("DOKJA_EMBED", "off")
    service = build_service()
    server = KnowledgeServiceServer(service=service)
    try:
        assert isinstance(service.store, PostgresStore)
        assert service.status()["storage_backend"] == "postgres"
        response = server.handle_request(
            json.dumps(
                {
                    "type": "knowledge.ingest",
                    "payload": {
                        "source_id": "wire",
                        "title": "Wire note",
                        "body": "Physics research evidence.",
                    },
                }
            )
        )
        assert response["status"] == "ok" and response["result"]["created"]
        response = server.handle_request(
            json.dumps(
                {
                    "type": "knowledge.search",
                    "payload": {
                        "query": "physics research evidence",
                    },
                }
            )
        )
        assert (
            response["status"] == "ok"
            and response["result"]["hits"][0]["source_id"] == "wire"
        )
        assert not source.exists()
    finally:
        server.socket.close(0)
        server.context.destroy(linger=0)
        service.store.close()


def test_import_verification_failure_rolls_back_every_row(
    pg_store, tmp_path, monkeypatch
):
    import migrate_postgres

    path = tmp_path / "source.db"
    source = Store(str(path))
    ingest(KnowledgeService(source, FakeEmbedder(), model="fake"))
    source.close()
    original = path.read_bytes()
    values = iter(["expected", "mismatch"])
    monkeypatch.setattr(migrate_postgres, "_digest", lambda *_: next(values))
    with pytest.raises(RuntimeError, match="verification failed"):
        import_sqlite(str(path), pg_store)
    assert pg_store.document_count() == 0 and pg_store.stats("fake")["chunks"] == 0
    assert path.read_bytes() == original


def test_verified_rollback_export_includes_post_cutover_writes(pg_store, tmp_path):
    service = KnowledgeService(pg_store, FakeEmbedder(), model="fake")
    ingest(service)
    ingest(
        service, source_id="post-cutover", body="New evidence accepted after cutover."
    )
    destination = tmp_path / "rollback.db"
    result = export_sqlite(str(destination), pg_store)
    assert result["verified"] and result["documents"] == 2
    assert destination.stat().st_mode & 0o777 == 0o600
    rollback = Store(str(destination))
    try:
        assert rollback.document_count() == 2
        restored = KnowledgeService(rollback, FakeEmbedder(), model="fake")
        assert (
            restored.search({"query": "new evidence accepted cutover"})["hits"][0][
                "source_id"
            ]
            == "post-cutover"
        )
        assert rollback.metadata_snapshot("study") == pg_store.metadata_snapshot(
            "study"
        )
    finally:
        rollback.close()
    original = destination.read_bytes()
    with pytest.raises(FileExistsError):
        export_sqlite(str(destination), pg_store)
    assert destination.read_bytes() == original
    assert pg_store.document_count() == 2


def test_version_one_source_import_does_not_upgrade_original(pg_store, tmp_path):
    path = tmp_path / "legacy.db"
    source = Store(str(path))
    ingest(KnowledgeService(source, FakeEmbedder(), model="fake"))
    source.conn.execute("DROP TABLE document_metadata")
    source.conn.execute("PRAGMA user_version=1")
    source.conn.commit()
    source.close()
    original = path.read_bytes()
    assert import_sqlite(str(path), pg_store)["verified"]
    assert pg_store.metadata_snapshot("study")["metadata"] == {}
    assert path.read_bytes() == original


def test_connection_and_storage_diagnostics_do_not_expose_private_values(
    monkeypatch, caplog
):
    from server import KnowledgeServiceServer

    def fail(*args, **kwargs):
        raise psycopg.OperationalError("private-connection-value")

    monkeypatch.setattr(psycopg, "connect", fail)
    with pytest.raises(RuntimeError, match="connection failed") as error:
        PostgresStore("host=localhost")
    assert "private-connection-value" not in str(error.value)

    class FailedService:
        def dispatch(self, event):
            raise psycopg.DataError("private-document-value")

    server = KnowledgeServiceServer(service=FailedService())
    try:
        reply = server.handle_request(
            '{"type":"knowledge.search","payload":{"query":"private-query-value"}}'
        )
        assert reply == {
            "status": "error",
            "message": "knowledge storage request failed",
        }
        assert "private-document-value" not in caplog.text
        assert "private-query-value" not in caplog.text
    finally:
        server.socket.close(0)
        server.context.destroy(linger=0)
