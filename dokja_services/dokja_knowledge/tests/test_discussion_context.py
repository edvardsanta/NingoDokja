from unittest.mock import Mock

import pytest
from discussion_context import dispatch


def document():
    return {
        "url": "https://example.com/report.pdf",
        "title": "Report",
        "passages": [
            {
                "text": "Persist events before processing.",
                "label": "Page 35",
                "url": "https://example.com/report.pdf#page=35",
            }
        ],
    }


def test_document_bounds_and_original_page_references():
    store = Mock(backend="postgres")
    doc = document()
    dispatch(
        store, "save", {"scope": "channel", "session": "session", "documents": [doc]}
    )
    store.discussion_save.assert_called_once_with("channel", "session", [doc])
    doc["passages"][0]["url"] = "https://other.example/report.pdf#page=35"
    with pytest.raises(ValueError, match="unrelated"):
        dispatch(
            store,
            "save",
            {"scope": "channel", "session": "session", "documents": [doc]},
        )
    assert store.discussion_save.call_count == 1


def test_no_sqlite_fallback_and_scope_is_required():
    with pytest.raises(ValueError, match="requires PostgreSQL"):
        dispatch(Mock(backend="sqlite"), "recall", {"scope": "channel"})
    with pytest.raises(ValueError, match="scope"):
        dispatch(Mock(backend="postgres"), "recall", {})


def test_model_cannot_assign_verified_status():
    store = Mock(backend="postgres")
    idea = {
        "text": "An idea",
        "quote": "Persist events before processing",
        "status": "verified",
    }
    dispatch(
        store,
        "remember",
        {"scope": "channel", "session": "session", "event": "event", "ideas": [idea]},
    )
    assert (
        store.discussion_remember.call_args.args[-1][0]["status"]
        == "extracted_unverified"
    )


def test_context_operations_require_orchestrator_source():
    from service import KnowledgeService

    service = KnowledgeService(Mock())
    with pytest.raises(ValueError, match="orchestrator"):
        service.dispatch(
            {
                "source": "discord",
                "type": "knowledge.context.recall",
                "payload": {"scope": "private"},
            }
        )
