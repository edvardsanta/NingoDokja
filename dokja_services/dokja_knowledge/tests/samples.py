"""Builds small sample files in memory, so the repository carries no binary fixtures."""

import io
import zipfile

NS = "urn:test:format"  # the extractors match local names, so any namespace works


def make_docx(blocks, title=None, extra_members=None):
    """blocks: ("h1", text) | ("p", text) | ("table", [[cell, ...], ...])."""
    body = []
    for kind, value in blocks:
        if kind == "table":
            rows = "".join(
                "<w:tr>" + "".join(f"<w:tc><w:p><w:r><w:t>{c}</w:t></w:r></w:p></w:tc>" for c in row) + "</w:tr>"
                for row in value
            )
            body.append(f"<w:tbl>{rows}</w:tbl>")
        elif kind.startswith("h"):
            body.append(
                f'<w:p><w:pPr><w:pStyle w:val="Heading{kind[1:]}"/></w:pPr><w:r><w:t>{value}</w:t></w:r></w:p>'
            )
        else:
            body.append(f"<w:p><w:r><w:t>{value}</w:t></w:r></w:p>")
    document = f'<w:document xmlns:w="{NS}"><w:body>{"".join(body)}</w:body></w:document>'
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("word/document.xml", document)
        if title:
            archive.writestr(
                "docProps/core.xml", f'<cp:coreProperties xmlns:cp="{NS}" xmlns:dc="{NS}2"><dc:title>{title}</dc:title></cp:coreProperties>'
            )
        for name, content in (extra_members or {}).items():
            archive.writestr(name, content)
    return buffer.getvalue()


def make_epub(chapters, title="Sample Book", hrefs=None):
    """chapters: list of xhtml bodies. hrefs overrides the manifest paths (for hostile tests)."""
    hrefs = hrefs or [f"text/ch{i}.xhtml" for i in range(len(chapters))]
    manifest = "".join(
        f'<item id="c{i}" href="{href}" media-type="application/xhtml+xml"/>' for i, href in enumerate(hrefs)
    ) + '<item id="img" href="cover.png" media-type="image/png"/>'
    spine = "".join(f'<itemref idref="c{i}"/>' for i in range(len(chapters)))
    package = (
        f'<package xmlns="{NS}" xmlns:dc="{NS}2"><metadata><dc:title>{title}</dc:title></metadata>'
        f"<manifest>{manifest}</manifest><spine>{spine}</spine></package>"
    )
    container = f'<container xmlns="{NS}"><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>'
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("META-INF/container.xml", container)
        archive.writestr("OEBPS/content.opf", package)
        for href, body in zip(hrefs, chapters):
            if not href.startswith(".."):
                archive.writestr(f"OEBPS/{href}", f"<html><body>{body}</body></html>")
        archive.writestr("OEBPS/cover.png", b"\x89PNG")
    return buffer.getvalue()


def make_pdf(lines=None):
    """A one-page PDF. With no lines the page has no text layer, like a scan."""
    content = ""
    if lines:
        content = "BT /F1 12 Tf 14 TL 20 150 Td " + " T* ".join(f"({line}) Tj" for line in lines) + " ET"
    objects = [
        "<</Type/Catalog/Pages 2 0 R>>",
        "<</Type/Pages/Kids[3 0 R]/Count 1>>",
        "<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 200]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>",
        f"<</Length {len(content)}>>\nstream\n{content}\nendstream",
        "<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
    ]
    out = bytearray(b"%PDF-1.4\n")
    offsets = []
    for number, body in enumerate(objects, start=1):
        offsets.append(len(out))
        out += f"{number} 0 obj\n{body}\nendobj\n".encode()
    xref_at = len(out)
    out += f"xref\n0 {len(objects) + 1}\n0000000000 65535 f \n".encode()
    for offset in offsets:
        out += f"{offset:010d} 00000 n \n".encode()
    out += f"trailer\n<</Root 1 0 R/Size {len(objects) + 1}>>\nstartxref\n{xref_at}\n%%EOF\n".encode()
    return bytes(out)


RSS = """<?xml version="1.0"?>
<rss version="2.0"><channel><title>Sample feed</title>
<item><title>First entry</title><link>http://docs.example/one</link><guid>guid-1</guid>
<description>&lt;p&gt;Body of the &lt;b&gt;first&lt;/b&gt; entry.&lt;/p&gt;</description></item>
<item><title>Second entry</title><link>http://docs.example/two</link>
<description>Plain text of the second entry.</description></item>
<item><title>Empty entry</title><link>http://docs.example/three</link></item>
</channel></rss>"""

ATOM = f"""<?xml version="1.0"?>
<feed xmlns="{NS}"><title>Atom sample</title>
<entry><title>Atom one</title><id>urn:atom:1</id><link rel="alternate" href="http://docs.example/a1"/>
<content type="html">&lt;p&gt;Atom body one.&lt;/p&gt;</content></entry>
<entry><title>Atom two</title><id>urn:atom:2</id><summary>Atom summary two.</summary></entry>
</feed>"""
