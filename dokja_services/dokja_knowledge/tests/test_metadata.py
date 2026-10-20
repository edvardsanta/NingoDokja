"""Metadata storage contracts, migration compatibility and embedding preservation."""

import json
import sqlite3

import pytest

from fakes import FakeEmbedder
from service import KnowledgeService
from store import MIGRATIONS, Store


@pytest.fixture
def service():
    store = Store(":memory:")
    yield KnowledgeService(store, embedder=FakeEmbedder(), model="fake")
    store.close()


def metadata(**extra):
    return {"form": "book", "content_kind": "research", "form_choice": {"origin": "user"},
            "kind_choice": {"origin": "user"}, "topics": ["physics"], **extra}


def ingest(service, **extra):
    return service.ingest({"source_id": "study", "title": "Study", "body": "Physics research evidence.", **extra})


def chunks(service):
    return [tuple(row) for row in service.store.conn.execute("SELECT * FROM chunks ORDER BY id")]


def test_metadata_roundtrips_without_using_tags_or_legacy_kind(service):
    expected = metadata(entities=[{"name": "Example Lab", "type": "organization"}], date_anchor="2026-10-19")
    ingest(service, kind="meme_hashtag", tags=["free-form"], metadata=expected)
    document = service.list_documents({})["documents"][0]
    assert document["kind"] == "meme_hashtag"
    assert document["tags"] == ["free-form"]
    assert document["metadata"] == expected
    hit = service.search({"query": "physics research", "kind": "meme_hashtag"})["hits"][0]
    assert hit["metadata"] == expected
    assert service.search({"query": "physics research", "kind": "book"})["hits"] == []


def test_metadata_only_update_preserves_chunks_vectors_and_embedding_cache(service):
    ingest(service, metadata=metadata())
    before = chunks(service)
    calls = len(service.embedder.calls)
    revision = service.store.version
    updated = metadata(content_kind="reference")
    result = ingest(service, metadata=updated)
    assert result["changed"] is False and result["metadata_changed"] is True
    assert chunks(service) == before
    assert len(service.embedder.calls) == calls
    assert service.store.version == revision
    assert service.list_documents({})["documents"][0]["metadata"] == updated
    assert ingest(service, metadata=updated)["metadata_changed"] is False
    assert ingest(service)["metadata_changed"] is False
    assert service.list_documents({})["documents"][0]["metadata"] == updated


def test_changed_content_invalidates_unsupplied_metadata_and_explicit_empty_clears(service):
    ingest(service, metadata=metadata())
    assert ingest(service, metadata={})["metadata_changed"] is True
    assert service.list_documents({})["documents"][0]["metadata"] == {}
    ingest(service, metadata=metadata())
    ingest(service, body="A different subject about carrot cake.")
    assert service.list_documents({})["documents"][0]["metadata"] == {}


def test_reading_reread_preserves_user_choices_and_patches_only_supplied_axes(service):
    first = metadata(reading={"format": "pdf", "reader": "test", "reader_version": "1"})
    ingest(service, metadata=first)
    before = chunks(service)
    defaults = {"form": None, "content_kind": None, "form_choice": {"origin": "user_required"},
                "kind_choice": {"origin": "user_required"}, "reading": first["reading"]}
    result = ingest(service, metadata=defaults, metadata_if_unchanged={"reading": first["reading"]})
    assert result["metadata_changed"] is False
    assert service.list_documents({})["documents"][0]["metadata"] == first
    ingest(service, metadata=defaults, metadata_if_unchanged={
        "form": "article", "form_choice": {"origin": "user"}, "topics": []})
    actual = service.list_documents({})["documents"][0]["metadata"]
    assert actual["form"] == "article" and actual["content_kind"] == "research" and actual["topics"] == []
    assert chunks(service) == before
    # A legacy document with no metadata receives defaults on its first reread.
    ingest(service, metadata={})
    ingest(service, metadata=defaults, metadata_if_unchanged={"reading": first["reading"]})
    assert service.list_documents({})["documents"][0]["metadata"] == defaults


@pytest.mark.parametrize("bad", [None, [], "research", {"form":"general"}, {"content_kind":"general"}, {"form": "pdf"}, {"content_kind": []},
    {"form":"book", "form_choice":{"origin":"user_required"}},
    {"content_kind":None, "kind_choice":{"origin":"user"}},
    {"topics": [3]}, {"entities": [{"type": "person"}]}, {"date_anchor": "2026-02-30"},
    {"date_anchor": "20261019"}, {"form_choice": {"origin": "classifier"}},
    {"reading": {"format": "pdf"}}, {"unknown": True}, {"topics": ["x" * 65537]},
    {"classifier": {"id": "kinds"}}, {"classifier": {"id": " ", "version": "1"}},
    {"classifier": {"id": "kinds", "version": "1", "model": "x"}},
])
def test_invalid_metadata_does_not_replace_existing_document(service, bad):
    ingest(service, metadata=metadata())
    before = chunks(service)
    with pytest.raises(ValueError, match="metadata"):
        ingest(service, body="Replacement", metadata=bad)
    assert chunks(service) == before
    assert service.list_documents({})["documents"][0]["metadata"] == metadata()


def test_delete_cascades_metadata(service):
    ingest(service, metadata=metadata())
    assert service.store.conn.execute("SELECT COUNT(*) FROM document_metadata").fetchone()[0] == 1
    service.delete({"source_id": "study"})
    assert service.store.conn.execute("SELECT COUNT(*) FROM document_metadata").fetchone()[0] == 0


def test_schema_one_upgrade_and_metadata_downgrade_preserve_content(tmp_path):
    path = str(tmp_path / "knowledge.db")
    # Generate a genuine v1 database through the original migration, including an
    # existing digest and embedded chunk. No owner database is opened by this test.
    old = sqlite3.connect(path)
    old.executescript(MIGRATIONS[0])
    old.execute("PRAGMA user_version = 1")
    old.execute("INSERT INTO documents VALUES (1, 'legacy', 'Legacy', 'note', '', '', 'digest', 't', 't')")
    old.execute("INSERT INTO chunks VALUES (1, 1, 0, '', 'Physics evidence', ?, 'fake')", (b'vector',))
    old.commit()
    old.close()
    store = Store(path)
    assert store.conn.execute("PRAGMA user_version").fetchone()[0] == 2
    assert store.list_documents(20, 0)[0]["metadata"] == "{}"
    assert store.get_document("legacy")["content_hash"] == "digest"
    original = [tuple(row) for row in store.conn.execute("SELECT * FROM chunks")]
    store.update_metadata(1, json.dumps(metadata()))
    assert store.chunks_by_ids([1])[1]["metadata"] == json.dumps(metadata())
    assert [tuple(row) for row in store.conn.execute("SELECT * FROM chunks")] == original
    # Documented offline downgrade discards metadata only, preserving post-upgrade text.
    store.conn.executescript("BEGIN IMMEDIATE; DROP TABLE document_metadata; PRAGMA user_version = 1; COMMIT;")
    assert [tuple(row) for row in store.conn.execute("SELECT * FROM chunks")] == original
    assert store.conn.execute("SELECT content_hash FROM documents").fetchone()[0] == "digest"
    store.close()
    # Re-upgrade after downgrade is supported, with no invented metadata.
    reopened = Store(path)
    assert reopened.get_metadata(1) == "{}"
    assert reopened.keyword_search("Physics", 10)
    reopened.close()


def test_failed_migration_rolls_back_schema_and_version(tmp_path, monkeypatch):
    path = str(tmp_path / "failed.db")
    db = sqlite3.connect(path)
    db.executescript(MIGRATIONS[0])
    db.execute("PRAGMA user_version = 1")
    db.close()
    monkeypatch.setattr("store.MIGRATIONS", [MIGRATIONS[0],
        "CREATE TABLE partial (id INTEGER); INSERT INTO nonexistent VALUES (1);"])
    with pytest.raises(sqlite3.OperationalError):
        Store(path)
    db = sqlite3.connect(path)
    assert db.execute("PRAGMA user_version").fetchone()[0] == 1
    assert db.execute("SELECT name FROM sqlite_master WHERE name = 'partial'").fetchone() is None
    db.close()


def test_older_build_refuses_newer_schema(tmp_path, monkeypatch):
    path = str(tmp_path / "newer.db")
    Store(path).close()
    monkeypatch.setattr("store.MIGRATIONS", [MIGRATIONS[0]])
    with pytest.raises(RuntimeError, match="newer than this build understands"):
        Store(path)


def test_pending_axes_survive_rereads_until_user_supplies_them(service):
    reading = {"format": "pdf", "reader": "test", "reader_version": "1"}
    pending = {"form": "book", "content_kind": None, "form_choice": {"origin": "user"},
               "kind_choice": {"origin": "user_required"}, "reading": reading}
    first = ingest(service, metadata=pending)
    assert first["metadata"] == pending
    before = chunks(service)
    # Even a new source default must not answer a question pending for the user.
    defaults = metadata(reading=reading)
    defaults.pop("topics")
    again = ingest(service, metadata=defaults, metadata_if_unchanged={"reading": reading})
    assert again["metadata"] == pending
    answered = ingest(service, metadata=defaults, metadata_if_unchanged={
        "content_kind": "research", "kind_choice": {"origin": "user"}})
    assert answered["metadata"]["content_kind"] == "research"
    assert answered["metadata"]["kind_choice"]["origin"] == "user"
    assert answered["metadata_changed"] and not answered["changed"]
    assert chunks(service) == before
