"""Passage identity survives extraction, retrieval, replacement and reopening."""
import base64
import io
import json

import pytest
from pypdf import PdfReader, PdfWriter
from pypdf.generic import DictionaryObject, NameObject, NumberObject, TextStringObject, ArrayObject

from extractors import default_registry
from passages import original_source, source_chunks, resolve
from samples import make_epub, make_pdf
from service import KnowledgeService
from store import Store


def encoded(data):
    return base64.b64encode(data).decode()


def test_pdf_retains_empty_pages_and_printed_labels():
    writer = PdfWriter()
    writer.add_blank_page(width=100, height=100)
    writer.add_page(PdfReader(io.BytesIO(make_pdf(["Population resources and marriage evidence."]))).pages[0])
    writer._root_object[NameObject("/PageLabels")] = DictionaryObject({NameObject("/Nums"): ArrayObject([
        NumberObject(0), DictionaryObject({NameObject("/P"): TextStringObject("cover")}),
        NumberObject(1), DictionaryObject({NameObject("/S"): NameObject("/D"), NameObject("/St"): NumberObject(7)})])})
    stream = io.BytesIO(); writer.write(stream)
    [doc] = default_registry().extract("study.pdf", stream.getvalue())
    assert doc.units[0]["text"] == ""
    assert doc.units[1]["page"] == 2 and doc.units[1]["label"] == "7"
    original = original_source(encoded(stream.getvalue()), "study.pdf", doc.units)
    [chunk] = source_chunks(original)
    loc = json.loads(chunk.locator)
    assert loc["page"] == 2 and loc["unit"] == 1
    assert resolve(original, loc)["unit"]["text"][loc["start"]:loc["end"]] == chunk.text


def test_epub_each_chapter_and_unicode_span_survive_restart_and_replacement(tmp_path):
    path = str(tmp_path / "knowledge.db")
    data = make_epub(["<h1>One</h1><p>Marriage evidence 🙂 repeated sentence.</p>",
                      "<h1>Two</h1><p>Marriage evidence 🙂 repeated sentence.</p>"])
    store = Store(path); service = KnowledgeService(store)
    service.ingest({"filename": "study.epub", "content_b64": encoded(data)})
    hits = service.search({"query": "Marriage evidence repeated sentence", "k": 8})["hits"]
    assert len(hits) == 2
    locators = [h["locator"] for h in hits]
    assert len({loc["passage_id"] for loc in locators}) == 2
    assert len({loc["href"] for loc in locators}) == 2
    old = hits[0]
    # A changed edition must not redirect an archived reference to the replacement text.
    service.ingest({"filename": "study.epub", "content_b64": encoded(make_epub(["<p>A different edition entirely.</p>"]))})
    store.close()
    store = Store(path); service = KnowledgeService(store)
    result = service.list_documents({"source_id": old["source_id"], "locator": old["locator"]})
    reading = result["documents"][0]["reading"]
    loc = old["locator"]
    assert reading["state"] == "resolved" and reading["content_b64"] == encoded(data)
    assert reading["unit"]["text"][loc["start"]:loc["end"]] == old["text"]
    assert service.list_documents({"source_id": old["source_id"], "locator": {**loc, "version": "f"*64}})["documents"][0]["reading"]["state"] == "unavailable"
    assert service.list_documents({"source_id": old["source_id"], "locator": {**loc, "start": -1}})["documents"][0]["reading"]["state"] == "unresolved"
    service.delete({"source_id": old["source_id"]})
    assert store.conn.execute("SELECT COUNT(*) FROM document_originals").fetchone()[0] == 0
    store.close()


def test_chunk_boundaries_preserve_exact_characters_and_unit_identity():
    text = "🙂 unicode evidence.\n\n" * 150
    source = original_source(encoded(b"fixture"), "study.epub", [{"href": "chapter.xhtml", "label": "Study", "text": text}])
    chunks = source_chunks(source)
    assert len(chunks) > 2
    for chunk in chunks:
        loc = json.loads(chunk.locator)
        assert text[loc["start"]:loc["end"]] == chunk.text
        assert len(chunk.text) <= 1200
    assert len({json.loads(c.locator)["passage_id"] for c in chunks}) == len(chunks)


def test_original_units_must_match_ingested_body():
    service = KnowledgeService(Store(":memory:"))
    with pytest.raises(ValueError, match="do not match"):
        service.ingest({"title": "Study", "body": "unrelated text", "original": {
            "filename": "study.epub", "content_b64": encoded(b"fixture"),
            "units": [{"text": "actual original", "href": "chapter.xhtml", "label": "Chapter"}]}})
