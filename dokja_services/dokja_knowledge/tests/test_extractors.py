import io
import zipfile

import pytest

from extractors import ExtractError, Registry, default_registry
from extractors.htmlfmt import html_to_text
from samples import ATOM, RSS, make_docx, make_epub, make_pdf


@pytest.fixture
def registry():
    return default_registry()


# ---- text and html ----------------------------------------------------------------------


def test_text_takes_its_title_from_a_leading_heading_or_the_file_name(registry):
    [doc] = registry.extract("notes.md", "﻿# Estoicismo\n\nA virtude basta.".encode())
    assert (doc.title, doc.text.startswith("# Estoicismo")) == ("Estoicismo", True)
    [doc] = registry.extract("ideas.txt", "sem cabeçalho".encode())
    assert doc.title == "ideas"
    [doc] = registry.extract("late.md", b"prose first\n\n# Later")
    assert doc.title == "late"


@pytest.mark.parametrize("data", [b"\xff\xfe\x00A", b"nul\x00byte"])
def test_binary_junk_is_not_accepted_as_text(registry, data):
    with pytest.raises(ExtractError):
        registry.extract("x.txt", data)


def test_html_keeps_headings_and_lists_and_drops_chrome():
    page = """<html><head><title>Page title</title><style>p{color:red}</style></head><body>
    <nav>Menu Home About</nav><script>var secret = 1;</script>
    <h1>Main heading</h1><p>First   paragraph
    with a break &amp; an entity.</p><h2>Sub</h2><ul><li>one</li><li>two</li></ul>
    <footer>Copyright line</footer></body></html>"""
    title, text = html_to_text(page)
    assert title == "Page title"
    assert "# Main heading" in text and "## Sub" in text
    assert "First paragraph with a break & an entity." in text
    assert "- one" in text and "- two" in text
    for hidden in ("Menu Home", "secret", "color:red", "Copyright"):
        assert hidden not in text


def test_html_declared_charset_is_honoured(registry):
    data = '<html><head><meta charset="latin-1"></head><body><p>ação</p></body></html>'.encode("latin-1")
    [doc] = registry.extract("page.html", data)
    assert "ação" in doc.text


# ---- docx -------------------------------------------------------------------------------


def test_docx_reads_headings_paragraphs_tables_and_the_title(registry):
    data = make_docx(
        [("h1", "Macro outlook"), ("p", "Growth slowed."), ("h2", "Details"),
         ("table", [["Item", "Value"], ["Rate", "4"]])],
        title="Quarterly memo",
    )
    [doc] = registry.extract("memo.docx", data)
    assert doc.title == "Quarterly memo"
    assert doc.text.splitlines()[0] == "# Macro outlook"
    assert "## Details" in doc.text and "Rate | 4" in doc.text


def test_docx_falls_back_to_the_first_heading_then_the_file_name(registry):
    [doc] = registry.extract("a.docx", make_docx([("h1", "Heading title"), ("p", "text")]))
    assert doc.title == "Heading title"
    [doc] = registry.extract("plain.docx", make_docx([("p", "only text")]))
    assert doc.title == "plain"


def test_docx_that_unpacks_to_too_much_is_refused(registry, monkeypatch):
    monkeypatch.setattr("extractors.MAX_UNPACKED_BYTES", 1_000)
    bomb = make_docx([("p", "x" * 5_000)])
    with pytest.raises(ExtractError, match="more than the allowed size"):
        registry.extract("bomb.docx", bomb)


def test_docx_with_entity_declarations_is_refused(registry):
    hostile = io.BytesIO()
    with zipfile.ZipFile(hostile, "w") as archive:
        archive.writestr(
            "word/document.xml",
            '<!DOCTYPE d [<!ENTITY a "aaaa">]><d xmlns:w="urn:t"><w:body><w:p><w:r><w:t>&a;</w:t></w:r></w:p></w:body></d>',
        )
    with pytest.raises(ExtractError, match="entity"):
        registry.extract("x.docx", hostile.getvalue())


@pytest.mark.parametrize("data", [b"not a zip", b"PK\x03\x04broken"])
def test_damaged_archives_are_reported_not_raised(registry, data):
    with pytest.raises(ExtractError):
        registry.extract("x.docx", data)
    with pytest.raises(ExtractError):
        registry.extract("x.epub", data)


# ---- epub -------------------------------------------------------------------------------


def test_epub_reads_chapters_in_spine_order_with_the_package_title(registry):
    data = make_epub(["<h1>Chapter one</h1><p>Alpha text.</p>", "<h1>Chapter two</h1><p>Beta text.</p>"])
    [doc] = registry.extract("book.epub", data)
    assert doc.title == "Sample Book"
    assert doc.text.index("Alpha text") < doc.text.index("Beta text")
    assert "# Chapter one" in doc.text and "PNG" not in doc.text


def test_epub_cannot_reference_files_outside_itself(registry):
    data = make_epub(["<p>x</p>"], hrefs=["../../outside.xhtml"])
    with pytest.raises(ExtractError, match="outside itself"):
        registry.extract("evil.epub", data)


# ---- pdf --------------------------------------------------------------------------------


def test_pdf_text_is_extracted(registry):
    [doc] = registry.extract("paper.pdf", make_pdf(["Inflation expectations rose.", "Rates were held."]))
    assert "Inflation expectations rose." in doc.text and doc.title == "paper"


def test_a_pdf_without_a_text_layer_says_so(registry):
    with pytest.raises(ExtractError, match="no text layer"):
        registry.extract("scan.pdf", make_pdf(None))


def test_a_damaged_pdf_is_reported_not_raised(registry):
    with pytest.raises(ExtractError):
        registry.extract("bad.pdf", b"%PDF-1.4 this is not a pdf")


# ---- feeds ------------------------------------------------------------------------------


def test_rss_yields_one_document_per_entry_with_text_and_a_link(registry):
    docs = registry.extract("feed.xml", RSS.encode())
    assert [d.title for d in docs] == ["First entry", "Second entry"]  # the empty entry is dropped
    assert docs[0].text == "Body of the first entry."
    assert docs[0].source_ref == "http://docs.example/one" and docs[0].key == "guid-1"
    assert docs[1].key == "http://docs.example/two"


def test_atom_entries_are_read_too(registry):
    docs = registry.extract("feed.atom", ATOM.encode())
    assert [(d.title, d.text, d.source_ref) for d in docs] == [
        ("Atom one", "Atom body one.", "http://docs.example/a1"),
        ("Atom two", "Atom summary two.", ""),
    ]


def test_xml_that_is_not_a_feed_is_refused(registry):
    with pytest.raises(ExtractError, match="not a syndication feed"):
        registry.extract("data.xml", b"<root><item>x</item></root>")


# ---- registry ---------------------------------------------------------------------------


def test_the_content_type_picks_the_extractor_when_the_name_has_no_extension(registry):
    [doc] = registry.extract("page", b"<html><body><p>hello there</p></body></html>", "text/html; charset=utf-8")
    assert doc.text == "hello there"


def test_unknown_types_list_what_is_supported(registry):
    with pytest.raises(ExtractError, match="supported: .*\\.docx"):
        registry.extract("archive.zip", b"x")


def test_extractors_and_fetchers_can_be_added_and_bad_schemes_are_refused():
    registry = Registry()
    registry.add_extractor("csv", lambda data, name: [])
    assert registry.extensions == [".csv"]
    registry.add_fetcher("inbox", lambda ref: (b"x", "x.txt"))
    assert registry.fetcher("INBOX") is not None
    for scheme in ("http", "https", "1bad", "has space", ""):
        with pytest.raises(ValueError):
            registry.add_fetcher(scheme, lambda ref: (b"", ""))


def test_an_extractor_that_crashes_is_reported_as_an_extract_error():
    registry = Registry()
    registry.add_extractor(".boom", lambda data, name: 1 / 0)
    with pytest.raises(ExtractError, match="ZeroDivisionError"):
        registry.extract("x.boom", b"data")
