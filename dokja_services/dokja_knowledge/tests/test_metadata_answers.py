"""Resuming user decisions must never re-read content or overwrite stale state."""

from concurrent.futures import ThreadPoolExecutor

import pytest

from fakes import FakeEmbedder
from service import KnowledgeService
from store import Store


@pytest.fixture
def service():
    store = Store(":memory:")
    yield KnowledgeService(store, embedder=FakeEmbedder(), model="fake")
    store.close()


def add(service):
    return service.ingest({"source_id": "study", "title": "Study", "body": "Physics research evidence.",
        "metadata": {"form": "book", "content_kind": None, "form_choice": {"origin": "user"},
                     "kind_choice": {"origin": "user_required"}, "topics": ["physics"]}})


def snapshot(service):
    return service.dispatch({"type": "knowledge.metadata.get", "payload": {"source_id": "study"}})


def answer(service, revision, kind="research"):
    return service.dispatch({"type": "knowledge.metadata.patch", "payload": {
        "source_id": "study", "revision": revision,
        "patch": {"content_kind": kind, "kind_choice": {"origin": "user"}},
    }})


def chunks(service):
    return [tuple(row) for row in service.store.conn.execute("SELECT * FROM chunks")]


def test_answer_only_changes_metadata_and_preserves_other_axes(service):
    add(service)
    before = chunks(service)
    count = len(service.embedder.calls)
    version = service.store.version
    old = snapshot(service)
    result = answer(service, old["revision"])
    assert result["metadata_changed"] and not result["changed"]
    assert result["metadata"] == {**old["metadata"], "content_kind": "research", "kind_choice": {"origin": "user"}}
    assert result["revision"] != old["revision"]
    assert chunks(service) == before and len(service.embedder.calls) == count
    assert service.store.version == version
    assert snapshot(service) == {k: result[k] for k in ("source_id", "title", "metadata", "revision")}
    assert not answer(service, result["revision"])["metadata_changed"]


@pytest.mark.parametrize("change", ["metadata", "content", "delete"])
def test_stale_user_answer_does_not_overwrite_changes(service, change):
    add(service)
    old = snapshot(service)
    if change == "metadata":
        answer(service, old["revision"], "reference")
    elif change == "content":
        service.ingest({"source_id": "study", "title": "Study", "body": "Different content."})
    else:
        service.delete({"source_id": "study"})
    before = chunks(service)
    with pytest.raises(ValueError, match="classification_conflict|not found"):
        answer(service, old["revision"])
    assert chunks(service) == before
    if change == "metadata":
        assert snapshot(service)["metadata"]["content_kind"] == "reference"
    # A conflict rolls back its transaction, leaving the connection usable.
    assert service.list_documents({})["total"] == (0 if change == "delete" else 1)


def test_two_writers_cannot_apply_answers_to_the_same_revision(tmp_path):
    path = str(tmp_path / "knowledge.db")
    first = KnowledgeService(Store(path), embedder=FakeEmbedder(), model="fake")
    second = KnowledgeService(Store(path), embedder=FakeEmbedder(), model="fake")
    try:
        add(first)
        revision = snapshot(first)["revision"]
        def apply(service, kind):
            try:
                return answer(service, revision, kind)["metadata"]["content_kind"]
            except ValueError as err:
                assert "classification_conflict" in str(err)
                return "conflict"
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda args: apply(*args), [(first, "research"), (second, "reference")]))
        assert results.count("conflict") == 1
        assert snapshot(first)["metadata"]["content_kind"] in results
    finally:
        first.store.close()
        second.store.close()


def test_metadata_operations_validate_input_and_keep_existing_data(service):
    add(service)
    before = snapshot(service)
    for payload in [
        {"source_id": "study", "patch": {"content_kind": "news"}},
        {"source_id": "study", "revision": before["revision"], "patch": {}},
        {"source_id": "study", "revision": before["revision"], "patch": {"content_kind": "general"}},
        {"source_id": "study", "revision": before["revision"], "patch": {"content_kind": None, "kind_choice": {"origin": "user"}}},
    ]:
        with pytest.raises(ValueError):
            service.dispatch({"type": "knowledge.metadata.patch", "payload": payload})
    assert snapshot(service) == before
    with pytest.raises(ValueError, match="source_id"):
        service.metadata_get({})
