import base64
import textwrap

import pytest

from extractors import Registry, default_registry
from fakes import FakeEmbedder
from fetch import Fetched, FetchError
from plugins import load_plugins
from samples import ATOM, RSS, make_docx, make_epub, make_pdf
from service import KnowledgeService
from store import Store


def b64(data: bytes) -> str:
    return base64.b64encode(data).decode()


class StubFetcher:
    def __init__(self, page=None, error=None):
        self.page, self.error, self.calls = page, error, []

    def __call__(self, url):
        self.calls.append(url)
        if self.error:
            raise self.error
        return self.page


@pytest.fixture
def make_service():
    def build(**extra):
        return KnowledgeService(Store(":memory:"), embedder=FakeEmbedder(), model="fake", **extra)

    return build


@pytest.fixture
def service(make_service):
    return make_service()


# ---- uploaded files ---------------------------------------------------------------------


def test_an_uploaded_docx_is_extracted_stored_and_searchable(service):
    data = make_docx([("h1", "Macro outlook"), ("p", "Growth slowed while inflation expectations rose.")], title="Memo")

    result = service.ingest({"content_b64": b64(data), "filename": "Quarterly memo.docx"})

    assert result["source_id"] == "file:Quarterly-memo.docx" and result["created"] and result["chunks"] >= 1
    listed = service.list_documents({})["documents"][0]
    assert listed["title"] == "Memo"
    hit = service.search({"query": "inflation expectations growth"})["hits"][0]
    assert hit["source_id"] == "file:Quarterly-memo.docx" and hit["relevant"]


def test_uploading_the_same_file_again_changes_nothing(service):
    payload = {"content_b64": b64(make_docx([("p", "some stable text here")])), "filename": "a.docx"}
    assert service.ingest(payload)["changed"] is True
    assert service.ingest(payload)["changed"] is False


def test_epub_and_pdf_uploads_work_and_a_title_override_wins(service):
    epub = service.ingest({"content_b64": b64(make_epub(["<h1>One</h1><p>Chapter text about rates.</p>"])),
                           "filename": "book.epub", "title": "My title", "kind": "book", "tags": ["macro"]})
    pdf = service.ingest({"content_b64": b64(make_pdf(["Rates were held steady."])), "filename": "paper.pdf"})
    titles = {d["source_id"]: d["title"] for d in service.list_documents({})["documents"]}
    assert titles == {"file:book.epub": "My title", "file:paper.pdf": "paper"}
    assert epub["chunks"] >= 1 and pdf["chunks"] >= 1


@pytest.mark.parametrize(
    "payload, message",
    [
        ({"content_b64": "!!not base64!!", "filename": "a.docx"}, "not valid base64"),
        ({"content_b64": b64(b"x")}, "filename is required"),
        ({"content_b64": b64(b"x"), "filename": "a.docx"}, "not a valid archive"),
        ({"content_b64": b64(b"x"), "filename": "a.unknown"}, "unsupported file type"),
        ({"content_b64": b64(make_pdf(None)), "filename": "scan.pdf"}, "no text layer"),
        ({"content_b64": "A" * (20_000_000 * 4 // 3 + 100), "filename": "big.pdf"}, "larger than"),
        ({"body": "text", "title": "T", "source": "inbox:x"}, "exactly one"),
        ({"title": "T"}, "exactly one"),
        ({"source": "not a source"}, "no fetcher is registered"),
    ],
)
def test_bad_inputs_are_rejected_with_a_reason_and_store_nothing(service, payload, message):
    with pytest.raises(ValueError, match=message):
        service.ingest(payload)
    assert service.list_documents({})["total"] == 0


# ---- feeds (several documents from one input) ---------------------------------------------


def test_a_feed_file_stores_one_document_per_entry(service):
    result = service.ingest({"content_b64": b64(RSS.encode()), "filename": "feed.xml", "kind": "article"})

    assert (result["count"], result["created"], result["unchanged"]) == (2, 2, 0)
    ids = [d["source_id"] for d in result["documents"]]
    assert len(set(ids)) == 2 and all(i.startswith("file:feed.xml#") for i in ids)
    docs = service.list_documents({})["documents"]
    assert {d["title"] for d in docs} == {"First entry", "Second entry"}
    assert {d["source_ref"] for d in docs} == {"http://docs.example/one", "http://docs.example/two"}
    assert service.search({"query": "first entry body"})["hits"][0]["title"] == "First entry"


def test_reading_a_feed_again_keeps_entries_and_updates_only_the_edited_one(service):
    upload = lambda xml: service.ingest({"content_b64": b64(xml.encode()), "filename": "feed.xml"})  # noqa: E731
    upload(RSS)
    again = upload(RSS)
    assert (again["created"], again["unchanged"]) == (0, 2)

    edited = upload(RSS.replace("Plain text of the second entry.", "Revised text of the second entry."))
    assert edited["unchanged"] == 1 and service.list_documents({})["total"] == 2
    assert service.search({"query": "revised text second entry"})["hits"][0]["title"] == "Second entry"


def test_atom_works_and_embedding_runs_once_for_the_whole_feed(make_service):
    embedder = FakeEmbedder()
    service = KnowledgeService(Store(":memory:"), embedder=embedder, model="fake")
    result = service.ingest({"content_b64": b64(ATOM.encode()), "filename": "a.atom"})
    assert result["count"] == 2 and result["embedded"] == result["chunks"]
    assert len(embedder.calls) == 1, "all chunks of a feed are embedded in one pass"


# ---- addresses --------------------------------------------------------------------------


def test_an_http_source_is_fetched_extracted_and_keeps_its_address(make_service):
    page = Fetched(b"<html><head><title>Rates note</title></head><body><h1>Rates note</h1>"
                   b"<p>Policy rates were held.</p></body></html>", "text/html", "https://docs.example/notes/rates")
    stub = StubFetcher(page)
    service = make_service(fetcher=stub)

    result = service.ingest({"source": "https://docs.example/notes/rates", "kind": "article"})

    assert stub.calls == ["https://docs.example/notes/rates"]
    doc = service.list_documents({})["documents"][0]
    assert doc["title"] == "Rates note" and doc["source_ref"] == "https://docs.example/notes/rates"
    assert result["source_id"].startswith("url:docs.example-notes-rates-")
    assert service.ingest({"source": "https://docs.example/notes/rates", "kind": "article"})["changed"] is False


def test_a_fetched_document_type_is_chosen_from_the_content_type(make_service):
    page = Fetched(make_pdf(["Fetched pdf text about liquidity."]), "application/pdf", "https://docs.example/download?id=7")
    service = make_service(fetcher=StubFetcher(page))
    service.ingest({"source": "https://docs.example/download?id=7"})
    assert "liquidity" in service.search({"query": "liquidity"})["hits"][0]["text"]


def test_fetch_failures_and_the_off_switch_are_reported(make_service):
    refused = make_service(fetcher=StubFetcher(error=FetchError("the address points to a non-public network")))
    with pytest.raises(ValueError, match="non-public"):
        refused.ingest({"source": "http://intranet.example/"})
    off = make_service(fetch_enabled=False, fetcher=StubFetcher(Fetched(b"x", "text/html", "http://a.example/")))
    with pytest.raises(ValueError, match="switched off"):
        off.ingest({"source": "http://a.example/"})
    assert off._fetch.calls == []


def test_a_fetched_feed_stores_its_entries(make_service):
    service = make_service(fetcher=StubFetcher(Fetched(RSS.encode(), "application/rss+xml", "https://docs.example/feed")))
    result = service.ingest({"source": "https://docs.example/feed"})
    assert result["count"] == 2
    assert all(d["source_id"].startswith("url:docs.example-feed-") for d in result["documents"])


# ---- user plugins -----------------------------------------------------------------------


def test_a_registered_fetcher_handles_its_own_scheme(make_service):
    registry = default_registry()
    registry.add_fetcher("inbox", lambda ref: (b"# Inbox note\n\nText from the user's own source.", "note.md"))
    service = make_service(registry=registry)

    result = service.ingest({"source": "inbox:2024/note"})

    assert result["source_id"].startswith("src:inbox-2024-note-")
    assert service.list_documents({})["documents"][0]["source_ref"] == "inbox:2024/note"
    assert service.status()["fetchers"] == ["inbox"]


def test_a_failing_fetcher_plugin_is_reported_without_leaking_its_message(make_service):
    registry = default_registry()
    registry.add_fetcher("broken", lambda ref: 1 / 0)
    with pytest.raises(ValueError, match="broken: fetcher failed: ZeroDivisionError"):
        make_service(registry=registry).ingest({"source": "broken:x"})


def test_plugins_are_loaded_from_a_directory_and_bad_ones_are_skipped(tmp_path, caplog):
    (tmp_path / "good.py").write_text(textwrap.dedent('''
        def register(registry):
            registry.add_extractor(".csv", lambda data, name: [])
            registry.add_fetcher("mine", lambda ref: (b"x", "x.txt"))
    '''))
    (tmp_path / "broken.py").write_text("raise RuntimeError('boom')\n")
    (tmp_path / "no_register.py").write_text("VALUE = 1\n")
    (tmp_path / "_private.py").write_text("raise SystemExit\n")
    registry = Registry()

    loaded = load_plugins(registry, str(tmp_path))

    assert loaded == ["good"]
    assert registry.extensions == [".csv"] and registry.schemes == ["mine"]
    assert "broken.py failed to load" in caplog.text and "no register" in caplog.text
    assert load_plugins(Registry(), str(tmp_path / "missing")) == []


def test_status_lists_formats_and_whether_fetching_is_on(service):
    status = service.status()
    assert {".pdf", ".docx", ".epub", ".md", ".html", ".xml"} <= set(status["formats"])
    assert status["fetch_enabled"] is True and status["fetchers"] == []
